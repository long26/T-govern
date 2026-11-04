// REE baseline for Table 6.2.
//
// This is a deliberately host-runnable copy of the algorithmic portions of
// enclave/enclave.cpp.  It uses the same NITCH/MIRACL and libsecp256k1 source,
// but replaces SGX randomness with Linux getrandom().  It is therefore a
// software baseline, not an SGX simulation and not an ECALL benchmark.
#include <array>
#include <chrono>
#include <cerrno>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

#include <sys/random.h>

#include "bls_BLS12381.h"
#include "core.h"
#include "nitch/distkg.hpp"
#include "nitch/miracl_bls12381.hpp"
#include "secp256k1.h"
#include "secp256k1_recovery.h"

namespace {

using Clock = std::chrono::steady_clock;
constexpr int kParticipants = 8;  // enclave DKG currently requires 2^k.
constexpr int kThreshold = 4;
constexpr int kWarmupIterations = 10;

volatile uint8_t benchmark_sink = 0;

struct SecpKeyPair {
    std::array<uint8_t, 32> sk{};
    std::array<uint8_t, 65> pk65{};
    std::array<uint8_t, 32> addr32{};
};

struct BlsFixture {
    std::array<char, BGS_BLS12381> secret{};
    std::array<char, 4 * BFS_BLS12381 + 1> public_key{};
    std::array<char, BFS_BLS12381 + 1> signature{};
    std::array<uint8_t, 32> authorization_digest{};
    int public_key_length = 0;
    int signature_length = 0;
};

void require(bool condition, const char* message) {
    if (!condition) throw std::runtime_error(message);
}

template <typename T>
void do_not_optimize(const T& value) {
#if defined(__GNUC__) || defined(__clang__)
    asm volatile("" : : "m"(value) : "memory");
#else
    benchmark_sink ^= reinterpret_cast<const uint8_t*>(&value)[0];
#endif
}

void random_bytes(uint8_t* output, size_t length) {
    size_t offset = 0;
    while (offset < length) {
        const ssize_t count = getrandom(output + offset, length - offset, 0);
        if (count < 0 && errno == EINTR) continue;
        require(count > 0, "getrandom failed");
        offset += static_cast<size_t>(count);
    }
}

// Keccak-256 copied from enclave.cpp.  MIRACL's SHA3 helper otherwise uses
// NIST padding, whereas the protocol binds Ethereum-compatible Keccak values.
void keccak256_hash(const uint8_t* data, size_t length, uint8_t* output) {
    core::sha3 state;
    core::SHA3_init(&state, SHA3_HASH256);
    for (size_t i = 0; i < length; ++i) core::SHA3_process(&state, data[i]);
    const int padding_bytes = state.rate - state.length;
    if (padding_bytes == 1) {
        core::SHA3_process(&state, 0x81);
    } else {
        core::SHA3_process(&state, 0x01);
        while (state.length != state.rate - 1) core::SHA3_process(&state, 0x00);
        core::SHA3_process(&state, 0x80);
    }
    core::SHA3_squeeze(&state, reinterpret_cast<char*>(output), state.len);
}

std::array<uint8_t, 32> domain(const char* text) {
    std::array<uint8_t, 32> output{};
    keccak256_hash(reinterpret_cast<const uint8_t*>(text), std::strlen(text), output.data());
    return output;
}

// Shared signing context (created once, reused) so per-call context creation
// stays out of the timed loops — matches the enclave's reused context. The
// precomp table is deterministic, so reuse is byte-identical to per-call create.
secp256k1_context* ree_sign_context() {
    static secp256k1_context* ctx = nullptr;
    if (ctx == nullptr) {
        ctx = secp256k1_context_create(SECP256K1_CONTEXT_SIGN);
    }
    return ctx;
}

int sign_recoverable_65(const uint8_t digest[32], const uint8_t secret[32], uint8_t output[65]) {
    secp256k1_context* context = ree_sign_context();
    if (!context) return 0;
    secp256k1_ecdsa_recoverable_signature signature;
    const int signed_ok = secp256k1_ecdsa_sign_recoverable(
        context, &signature, digest, secret, nullptr, nullptr);
    int recovery_id = 0;
    const int serialized_ok = signed_ok && secp256k1_ecdsa_recoverable_signature_serialize_compact(
        context, output, &recovery_id, &signature);
    if (!serialized_ok) return 0;
    output[64] = static_cast<uint8_t>(recovery_id + 27);
    return 65;
}

SecpKeyPair create_secp256k1_pair(secp256k1_context* context) {
    SecpKeyPair result;
    do {
        random_bytes(result.sk.data(), result.sk.size());
    } while (!secp256k1_ec_seckey_verify(context, result.sk.data()));

    secp256k1_pubkey public_key;
    require(secp256k1_ec_pubkey_create(context, &public_key, result.sk.data()) == 1,
            "secp256k1 public-key creation failed");
    size_t output_length = result.pk65.size();
    require(secp256k1_ec_pubkey_serialize(context, result.pk65.data(), &output_length,
            &public_key, SECP256K1_EC_UNCOMPRESSED) == 1 && output_length == result.pk65.size(),
            "secp256k1 public-key serialization failed");

    std::array<uint8_t, 32> address_hash{};
    keccak256_hash(result.pk65.data() + 1, 64, address_hash.data());
    std::memcpy(result.addr32.data() + 12, address_hash.data() + 12, 20);
    return result;
}

SecpKeyPair generate_keypair() {
    secp256k1_context* context = ree_sign_context();
    require(context != nullptr, "secp256k1 context allocation failed");
    return create_secp256k1_pair(context);
}

nitch::Scalar evaluation_point_for_index(
    nitch::MiraclBls12381& context, int domain_size, int participant_index) {
    const nitch::EvaluationDomain& domain = context.evaluation_domain(domain_size);
    return domain.powers[static_cast<size_t>(participant_index % domain.size)];
}

// Direct REE copy of the local part of ecall_distkg_create_deal.  Wire-format
// buffers are not needed for the baseline; the same coefficients, point
// multiplications, scalar evaluations and serializations are still performed.
uint8_t local_dkg(nitch::MiraclBls12381& context) {
    std::vector<nitch::Scalar> coefficients;
    coefficients.reserve(kThreshold + 1);
    for (int degree = 0; degree <= kThreshold; ++degree) {
        coefficients.push_back(context.random_scalar());
    }

    uint8_t sink = 0;
    for (const nitch::Scalar& coefficient : coefficients) {
        const std::vector<uint8_t> bytes = context.g1_to_bytes(
            context.g1_mul(context.g1_generator(), coefficient), false);
        sink ^= bytes.back();
    }
    const std::vector<uint8_t> b0 = context.g2_to_bytes(
        context.g2_mul(context.g2_generator(), coefficients.front()), false);
    sink ^= b0.back();

    for (int participant = 1; participant <= kParticipants; ++participant) {
        const nitch::Scalar x = evaluation_point_for_index(context, kParticipants, participant);
        nitch::Scalar value = context.scalar_from_u64(0);
        for (int degree = kThreshold; degree >= 0; --degree) {
            value = context.scalar_mul(value, x);
            value = context.scalar_add(value, coefficients[degree]);
        }
        const std::vector<uint8_t> serialized = context.scalar_to_bytes(value);
        sink ^= serialized.back();
    }
    return sink;
}

std::vector<uint8_t> strip_miracl_prefix(const std::vector<uint8_t>& bytes) {
    if (bytes.size() > 1 && (bytes[0] == 0x04 || bytes[0] == 0x03 || bytes[0] == 0x02)) {
        return std::vector<uint8_t>(bytes.begin() + 1, bytes.end());
    }
    return bytes;
}

struct PCollideOutput {
    nitch::G1Point contribution;
    nitch::Scalar challenge;
    nitch::Scalar response;
    nitch::G1Point y;
};

// Direct REE copy of ecall_partial_adapt's TCH.PCollide region. In
// particular, keep the enclave's G1 transcript and the otherwise redundant
// hash_to_g1(challenge_data) call: the baseline must measure the code that is
// actually deployed, not a cleaned-up NITCH variant.
PCollideOutput pcollide_enclave_copy(
    nitch::MiraclBls12381& context,
    const nitch::Scalar& secret_share,
    const nitch::G1Point& verification_key_g1,
    const std::string& source_message,
    const std::string& target_message) {
    const nitch::G1Point h1m = context.hash_to_g1(source_message);
    const nitch::G1Point h1m_prime = context.hash_to_g1(target_message);
    PCollideOutput output;
    output.y = context.g1_sub(h1m, h1m_prime);
    output.contribution = context.g1_mul(output.y, secret_share);

    const nitch::Scalar tau = context.random_scalar();
    const nitch::G1Point g1_generator = context.g1_generator();
    const nitch::G1Point t1 = context.g1_mul(g1_generator, tau);
    const nitch::G1Point t2 = context.g1_mul(output.y, tau);
    std::vector<uint8_t> challenge_data;
    const auto append_point = [&](const nitch::G1Point& point) {
        const std::vector<uint8_t> bytes = strip_miracl_prefix(context.g1_to_bytes(point, false));
        challenge_data.insert(challenge_data.end(), bytes.begin(), bytes.end());
    };
    append_point(g1_generator);
    append_point(output.y);
    append_point(verification_key_g1);
    append_point(output.contribution);
    append_point(t1);
    append_point(t2);
    const std::string transcript(reinterpret_cast<const char*>(challenge_data.data()), challenge_data.size());
    const nitch::G1Point challenge_point = context.hash_to_g1(transcript);
    do_not_optimize(challenge_point);
    output.challenge = context.hash_to_scalar(challenge_data);
    output.response = context.scalar_add(tau, context.scalar_mul(output.challenge, secret_share));

    // These are the four serializations written by ecall_partial_adapt before
    // its separately instrumented secp256k1 signature step.
    const std::vector<uint8_t> contribution_bytes = strip_miracl_prefix(context.g1_to_bytes(output.contribution, false));
    const std::vector<uint8_t> challenge_bytes = context.scalar_to_bytes(output.challenge);
    const std::vector<uint8_t> response_bytes = context.scalar_to_bytes(output.response);
    const std::vector<uint8_t> y_bytes = strip_miracl_prefix(context.g1_to_bytes(output.y, false));
    benchmark_sink ^= contribution_bytes.back() ^ challenge_bytes.back() ^ response_bytes.back() ^ y_bytes.back();
    return output;
}

BlsFixture make_bls_fixture() {
    require(BLS12381::BLS_INIT() == BLS_OK, "BLS initialization failed");
    BlsFixture fixture;
    std::array<char, 64> ikm{};
    for (size_t i = 0; i < 32; ++i) ikm[i] = static_cast<char>(i + 1);
    core::octet ikm_octet = {32, static_cast<int>(ikm.size()), ikm.data()};
    core::octet secret = {0, static_cast<int>(fixture.secret.size()), fixture.secret.data()};
    core::octet public_key = {0, static_cast<int>(fixture.public_key.size()), fixture.public_key.data()};
    require(BLS12381::BLS_KEY_PAIR_GENERATE(&ikm_octet, &secret, &public_key) == BLS_OK,
            "BLS key generation failed");

    const auto auth_domain = domain("T_GOVERN_REDACT_AUTH_V1");
    std::array<uint8_t, 160> encoded{};
    std::memcpy(encoded.data(), auth_domain.data(), 32);
    for (size_t i = 32; i < encoded.size(); ++i) encoded[i] = static_cast<uint8_t>(i);
    keccak256_hash(encoded.data(), encoded.size(), fixture.authorization_digest.data());
    core::octet message = {32, 32, reinterpret_cast<char*>(fixture.authorization_digest.data())};
    core::octet signature = {0, static_cast<int>(fixture.signature.size()), fixture.signature.data()};
    require(BLS12381::BLS_CORE_SIGN(&signature, &message, &secret) == BLS_OK,
            "BLS signing failed");
    fixture.public_key_length = public_key.len;
    fixture.signature_length = signature.len;
    return fixture;
}

bool verify_authorization_certificate(const BlsFixture& fixture) {
    // This is the same BLS verification and context-digest binding performed
    // by verify_cert_req_auth in enclave.cpp.
    require(BLS12381::BLS_INIT() == BLS_OK, "BLS initialization failed");
    core::octet message = {32, 32, const_cast<char*>(reinterpret_cast<const char*>(fixture.authorization_digest.data()))};
    core::octet signature = {fixture.signature_length, fixture.signature_length,
                             const_cast<char*>(fixture.signature.data())};
    core::octet public_key = {fixture.public_key_length, fixture.public_key_length,
                              const_cast<char*>(fixture.public_key.data())};
    if (BLS12381::BLS_CORE_VERIFY(&signature, &message, &public_key) != BLS_OK) return false;

    const auto auth_domain = domain("T_GOVERN_REDACT_AUTH_V1");
    std::array<uint8_t, 160> encoded{};
    std::memcpy(encoded.data(), auth_domain.data(), 32);
    for (size_t i = 32; i < encoded.size(); ++i) encoded[i] = static_cast<uint8_t>(i);
    std::array<uint8_t, 32> recomputed{};
    keccak256_hash(encoded.data(), encoded.size(), recomputed.data());
    return recomputed == fixture.authorization_digest;
}

std::array<uint8_t, 32> vrf_compute(
    const SecpKeyPair& signature_key, const SecpKeyPair& vrf_key, const std::array<uint8_t, 32>& seed) {
    const auto vrf_domain = domain("T_GOVERN_TEE_VRF_V1");
    std::array<uint8_t, 224> encoded{};
    std::memcpy(encoded.data(), vrf_domain.data(), 32);
    encoded[127] = 1;  // epoch = 1, left-padded uint256
    std::memcpy(encoded.data() + 128, signature_key.addr32.data(), 32);
    std::memcpy(encoded.data() + 160, vrf_key.addr32.data(), 32);
    std::memcpy(encoded.data() + 192, seed.data(), 32);
    std::array<uint8_t, 32> digest{};
    keccak256_hash(encoded.data(), encoded.size(), digest.data());

    std::array<uint8_t, 65> proof{};
    require(sign_recoverable_65(digest.data(), vrf_key.sk.data(), proof.data()) == 65,
            "VRF proof signing failed");
    std::array<uint8_t, 161> packed{};
    std::memcpy(packed.data(), vrf_domain.data(), 32);
    std::memcpy(packed.data() + 32, seed.data(), 32);
    std::memcpy(packed.data() + 64, vrf_key.addr32.data(), 32);
    std::memcpy(packed.data() + 96, proof.data(), proof.size());
    std::array<uint8_t, 32> result{};
    keccak256_hash(packed.data(), packed.size(), result.data());
    return result;
}

std::array<uint8_t, 65> erase_share(
    nitch::Scalar& secret_share, const SecpKeyPair& signature_key, uint64_t epoch) {
    // The zeroisation, proof hash and proof signature mirror ecall_erase_share.
    std::memset(&secret_share, 0, sizeof(secret_share));
    std::array<uint8_t, 32> epoch_bytes{};
    for (int i = 0; i < 8; ++i) epoch_bytes[31 - i] = static_cast<uint8_t>(epoch >> (8 * i));
    std::array<uint8_t, 70> erase_data{};
    std::memcpy(erase_data.data(), epoch_bytes.data(), 32);
    std::memcpy(erase_data.data() + 32, signature_key.addr32.data(), 32);
    std::memcpy(erase_data.data() + 64, "ERASED", 6);
    std::array<uint8_t, 32> proof_hash{};
    keccak256_hash(erase_data.data(), erase_data.size(), proof_hash.data());

    const auto erase_domain = domain("T_GOVERN_TEE_ERASE_V1");
    std::array<uint8_t, 224> encoded{};
    std::memcpy(encoded.data(), erase_domain.data(), 32);
    std::memcpy(encoded.data() + 96, signature_key.addr32.data(), 32);
    std::memcpy(encoded.data() + 160, epoch_bytes.data(), 32);
    std::memcpy(encoded.data() + 192, proof_hash.data(), 32);
    std::array<uint8_t, 32> digest{};
    keccak256_hash(encoded.data(), encoded.size(), digest.data());
    std::array<uint8_t, 65> proof{};
    require(sign_recoverable_65(digest.data(), signature_key.sk.data(), proof.data()) == 65,
            "erase proof signing failed");
    return proof;
}

struct TimingSummary {
    double mean_microseconds = 0;
    double sample_standard_deviation_microseconds = 0;
};

// Each formal invocation is one sample. The reported standard deviation uses
// Bessel's correction (n - 1), so it remains an unbiased sample estimate.
template <typename Operation>
TimingSummary measure_microseconds(int iterations, Operation operation) {
    std::vector<double> samples;
    samples.reserve(static_cast<size_t>(iterations));
    double sum = 0;
    for (int i = 0; i < iterations; ++i) {
        const auto start = Clock::now();
        operation();
        const auto end = Clock::now();
        const double elapsed = std::chrono::duration<double, std::micro>(end - start).count();
        samples.push_back(elapsed);
        sum += elapsed;
    }

    TimingSummary summary;
    summary.mean_microseconds = sum / iterations;
    if (iterations > 1) {
        double squared_deviation_sum = 0;
        for (const double sample : samples) {
            const double deviation = sample - summary.mean_microseconds;
            squared_deviation_sum += deviation * deviation;
        }
        summary.sample_standard_deviation_microseconds =
            std::sqrt(squared_deviation_sum / (iterations - 1));
    }
    return summary;
}

template <typename Operation>
void warm_up(Operation operation) {
    for (int i = 0; i < kWarmupIterations; ++i) operation();
}

int parse_iterations(int argc, char** argv) {
    if (argc < 2) return 100;
    const int iterations = std::atoi(argv[1]);
    require(iterations > 0, "iteration count must be positive");
    return iterations;
}

void self_test(nitch::MiraclBls12381& context) {
    require(local_dkg(context) != 0, "DKG output unexpectedly empty");
    const nitch::DistributedKeyMaterial material = nitch::simulate_distkg(context, kParticipants, kThreshold);
    const nitch::G1Point verification_key_g1 = context.g1_mul(
        context.g1_generator(), material.secret_shares[0]);
    const PCollideOutput partial = pcollide_enclave_copy(
        context, material.secret_shares[0], verification_key_g1, "old", "new");
    const nitch::G1Point expected_y = context.g1_sub(context.hash_to_g1("old"), context.hash_to_g1("new"));
    require(context.g1_equal(partial.y, expected_y) &&
                context.g1_equal(partial.contribution, context.g1_mul(expected_y, material.secret_shares[0])),
            "PCollide contribution equation failed");
    const BlsFixture certificate = make_bls_fixture();
    require(verify_authorization_certificate(certificate), "authorization certificate verification failed");
    const SecpKeyPair signature_key = generate_keypair();
    const SecpKeyPair vrf_key = generate_keypair();
    std::array<uint8_t, 32> seed{};
    seed[31] = 1;
    require(vrf_compute(signature_key, vrf_key, seed) != std::array<uint8_t, 32>{}, "VRF result is empty");
    nitch::Scalar share = material.secret_shares[0];
    const auto proof = erase_share(share, signature_key, 1);
    require(proof[0] != 0 || proof[1] != 0, "erase proof is empty");
}

}  // namespace

int main(int argc, char** argv) {
    try {
        nitch::MiraclBls12381 context;
        self_test(context);
        if (argc == 2 && std::string(argv[1]) == "--self-test") {
            std::cout << "Table 6.2 REE baseline self-test: PASS\n";
            return 0;
        }

        const int iterations = parse_iterations(argc, argv);
        const BlsFixture certificate = make_bls_fixture();
        const nitch::DistributedKeyMaterial material = nitch::simulate_distkg(context, kParticipants, kThreshold);
        const nitch::G1Point verification_key_g1 = context.g1_mul(
            context.g1_generator(), material.secret_shares[0]);
        const SecpKeyPair signature_key = generate_keypair();
        const SecpKeyPair vrf_key = generate_keypair();
        std::array<uint8_t, 32> digest{};
        digest[31] = 7;
        std::array<uint8_t, 32> seed{};
        seed[31] = 1;

        const auto dkg_operation = [&] {
            benchmark_sink ^= local_dkg(context);
        };
        const auto certificate_operation = [&] {
            require(verify_authorization_certificate(certificate), "authorization verification failed");
        };
        const auto pcollide_operation = [&] {
            const PCollideOutput output = pcollide_enclave_copy(
                context, material.secret_shares[0], verification_key_g1, "old", "new");
            do_not_optimize(output);
        };
        const auto sign_operation = [&] {
            std::array<uint8_t, 65> signature{};
            require(sign_recoverable_65(digest.data(), signature_key.sk.data(), signature.data()) == 65,
                    "TEE signature generation failed");
            benchmark_sink ^= signature.back();
        };
        const auto vrf_keygen_operation = [&] {
            const SecpKeyPair output = generate_keypair();
            do_not_optimize(output);
        };
        const auto vrf_comp_operation = [&] {
            const auto output = vrf_compute(signature_key, vrf_key, seed);
            benchmark_sink ^= output.back();
        };
        const auto erase_operation = [&] {
            nitch::Scalar share = material.secret_shares[0];  // setup outside timed erase body
            const auto output = erase_share(share, signature_key, 1);
            benchmark_sink ^= output.back();
        };

        warm_up(dkg_operation);
        warm_up(certificate_operation);
        warm_up(pcollide_operation);
        warm_up(sign_operation);
        warm_up(vrf_keygen_operation);
        warm_up(vrf_comp_operation);
        warm_up(erase_operation);

        const TimingSummary dkg_us = measure_microseconds(iterations, dkg_operation);
        const TimingSummary certificate_us = measure_microseconds(iterations, certificate_operation);
        const TimingSummary pcollide_us = measure_microseconds(iterations, pcollide_operation);
        const TimingSummary sign_us = measure_microseconds(iterations, sign_operation);
        const TimingSummary vrf_keygen_us = measure_microseconds(iterations, vrf_keygen_operation);
        const TimingSummary vrf_comp_us = measure_microseconds(iterations, vrf_comp_operation);
        const TimingSummary erase_us = measure_microseconds(iterations, erase_operation);

        const auto print_summary = [](const char* name, const TimingSummary& summary) {
            std::cout << name << summary.mean_microseconds << " ± "
                      << summary.sample_standard_deviation_microseconds << " us/op\n";
        };

        std::cout << "Table 6.2 REE algorithm baseline (iterations=" << iterations
                  << ", warmup=" << kWarmupIterations << ", n=" << kParticipants
                  << ", t=" << kThreshold << ")\n";
        std::cout << "Operation: mean ± sample stddev\n";
        print_summary("TCH.DKG local:          ", dkg_us);
        print_summary("Authorization cert:     ", certificate_us);
        print_summary("TCH.PCollide:           ", pcollide_us);
        print_summary("TEE signature:          ", sign_us);
        print_summary("VRF.KeyGen:             ", vrf_keygen_us);
        print_summary("VRF.Comp:               ", vrf_comp_us);
        print_summary("Share erase:            ", erase_us);
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "Table 6.2 REE benchmark failed: " << error.what() << '\n';
        return 1;
    }
}
