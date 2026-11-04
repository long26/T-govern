// Standalone committee authorization-certificate generator for Table 6.2 op 1
// (enclave verify_cert_req_auth). Decoupled from table_6_2_ree_bench, which is
// a pure timing baseline. This binary generates the deterministic committee BLS
// keypair and signs the authorization_digest with the exact MIRACL BLS_CORE_SIGN
// the enclave verifies with BLS_CORE_VERIFY, so the emitted bytes are accepted
// as-is. Output (one per line, lowercase hex, no 0x):
//   PUBKEY=<> DIGEST=<> SIG=<> DREQ=<> DPOLICY=<> CPKV=<> EXPIRES=<>
#include <cstdint>
#include <cstring>
#include <iostream>
#include <string>

#include "bls_BLS12381.h"
#include "core.h"

namespace {

void keccak256_hash(const uint8_t* data, size_t len, uint8_t* out) {
    core::sha3 sh;
    core::SHA3_init(&sh, SHA3_HASH256);
    for (size_t i = 0; i < len; ++i) core::SHA3_process(&sh, data[i]);
    const int q = sh.rate - sh.length;
    if (q == 1) {
        core::SHA3_process(&sh, 0x81);
    } else {
        core::SHA3_process(&sh, 0x01);
        while (sh.length != sh.rate - 1) core::SHA3_process(&sh, 0x00);
        core::SHA3_process(&sh, 0x80);
    }
    core::SHA3_squeeze(&sh, reinterpret_cast<char*>(out), sh.len);
}

void print_hex(const char* label, const uint8_t* data, size_t len) {
    std::cout << label << "=";
    static const char d[] = "0123456789abcdef";
    for (size_t i = 0; i < len; ++i) std::cout << d[data[i] >> 4] << d[data[i] & 0x0f];
    std::cout << "\n";
}

}  // namespace

// Deterministic committee keypair (fixed IKM = bytes 1..32) so the output is
// reproducible across runs. The request-context fields use the same fixed byte
// pattern the enclave recomputes the digest over.
int main() {
    using namespace core;
    if (BLS12381::BLS_INIT() != BLS_OK) {
        std::cerr << "BLS_INIT failed\n";
        return 1;
    }

    char ikm[32];
    for (int i = 0; i < 32; ++i) ikm[i] = static_cast<char>(i + 1);
    octet IKM = {32, 32, ikm};

    char secret[64] = {0};
    char pubkey_buf[256] = {0};
    octet S = {0, sizeof(secret), secret};
    octet W = {0, sizeof(pubkey_buf), pubkey_buf};
    if (BLS12381::BLS_KEY_PAIR_GENERATE(&IKM, &S, &W) != BLS_OK) {
        std::cerr << "BLS key generation failed\n";
        return 1;
    }

    // AUTH_DOMAIN = keccak256("T_GOVERN_REDACT_AUTH_V1"); encoded = AUTH_DOMAIN
    // || d_req || d_policy || committee_pub_key_version || expires_at, where the
    // last four fields are the fixed bytes 0x20..0x9f (matching the enclave's
    // recompute).
    uint8_t auth_domain[32];
    {
        const char d[] = "T_GOVERN_REDACT_AUTH_V1";
        keccak256_hash(reinterpret_cast<const uint8_t*>(d), std::strlen(d), auth_domain);
    }
    uint8_t encoded[160];
    std::memset(encoded, 0, sizeof(encoded));
    std::memcpy(encoded, auth_domain, 32);
    for (size_t i = 32; i < sizeof(encoded); ++i) encoded[i] = static_cast<uint8_t>(i);

    uint8_t digest[32];
    keccak256_hash(encoded, sizeof(encoded), digest);

    char sig_buf[64] = {0};
    octet M = {32, 32, reinterpret_cast<char*>(digest)};
    octet SIG = {0, sizeof(sig_buf), sig_buf};
    if (BLS12381::BLS_CORE_SIGN(&SIG, &M, &S) != BLS_OK) {
        std::cerr << "BLS sign failed\n";
        return 1;
    }

    // Self-verify with the enclave's exact W semantics (W.max = 128, pubkey
    // left-justified, zero-padded), to catch any mismatch before the test runs.
    char gvk[128];
    std::memset(gvk, 0, sizeof(gvk));
    std::memcpy(gvk, pubkey_buf, W.len);
    octet Wd = {static_cast<int>(sizeof(gvk)), static_cast<int>(sizeof(gvk)), gvk};
    if (BLS12381::BLS_CORE_VERIFY(&SIG, &M, &Wd) != BLS_OK) {
        std::cerr << "BLS self-verify failed\n";
        return 1;
    }

    print_hex("PUBKEY", reinterpret_cast<const uint8_t*>(pubkey_buf), W.len);
    print_hex("DIGEST", digest, 32);
    print_hex("SIG", reinterpret_cast<const uint8_t*>(sig_buf), SIG.len);
    print_hex("DREQ", encoded + 32, 32);
    print_hex("DPOLICY", encoded + 64, 32);
    print_hex("CPKV", encoded + 96, 32);
    print_hex("EXPIRES", encoded + 128, 32);
    return 0;
}
