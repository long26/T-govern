// c_api.cpp — C boundary over the vendored nitch BLS12-381 core.
//
// Authoritative contract: docs/nitchcore-c-api-contract.md. Algorithm bodies
// are transcribed from the tee-connect enclave sources:
//   - sgx/enclave/enclave.cpp  : the per-party ecalls (SGX boilerplate and
//     peer-encryption framing stripped; shares cross this boundary as plain
//     32-byte scalars per contract §0).
//   - sgx/enclave/bench/table_6_2_ree_bench.cpp : pcollide_enclave_copy, the
//     host-native reference for nitch_partial_adapt.
// Where the enclave and the nitch Go semantics disagree, the CONTRACT wins
// (it designates the Go implementation as the semantic baseline):
//   - distkg_extract   : PLAIN SUM  (enclave uses Lagrange weighting),
//   - dkrs_extract     : old-domain Lagrange, sk/vk only, no pk output
//                        (enclave recomputes and outputs the pk),
//   - dkrs_verify_share: commitment evaluation only, no pairing check
//                        (the enclave adds one; the Go version does not),
//   - distkg_verify_share: pairing check FIRST, then commitment evaluation.
//
// State model (contract §0): one lazily constructed context per thread, no
// global mutable state, no locks. No exception ever crosses the C boundary:
// every exported function is a try/catch wrapper around an impl_ function.

#include "c_api.h"

#include <cstring>
#include <exception>
#include <stdexcept>
#include <string>
#include <unordered_set>
#include <vector>

#include "core.h"
#include "big_B384_58.h"
#include "ecp_BLS12381.h"
#include "ecp2_BLS12381.h"
#include "nitch/miracl_bls12381.hpp"
#include "nitch/serialization.hpp"

namespace {

// MIRACL serializes scalars as MODBYTES (=48) bytes; the contract wire format
// is 32 bytes. Every scalar this file writes is < r < 2^255, so the top 16
// bytes of the 48-byte form are always zero and the trim below is exact.
// (Verified against nitch::MiraclBls12381::scalar_to_bytes, which emits 48.)
constexpr size_t kMiraclScalarBytes = 48;

using core::octet;

octet make_octet(char* buffer, int capacity) {
    octet out;
    out.len = 0;
    out.max = capacity;
    out.val = buffer;
    return out;
}

// ---------------------------------------------------------------------------
// Per-thread context (contract §0): lazy construction, destruction at thread
// exit, no locks. Each thread gets an independent CSPRNG and FFT-domain cache.
// ---------------------------------------------------------------------------
nitch::MiraclBls12381* tls_ctx() {
    thread_local nitch::MiraclBls12381 ctx;
    return &ctx;
}

// ---------------------------------------------------------------------------
// Byte conversion helpers — copied verbatim (as `static`) from
// sgx/enclave/enclave.cpp ~901-960. Point wire format is kilic (no 0x04).
// ---------------------------------------------------------------------------
std::vector<uint8_t> miracl_g1_to_kilic(const std::vector<uint8_t>& miracl_bytes) {
    if (miracl_bytes.size() > 1 && (miracl_bytes[0] == 0x04 || miracl_bytes[0] == 0x03 || miracl_bytes[0] == 0x02)) {
        return std::vector<uint8_t>(miracl_bytes.begin() + 1, miracl_bytes.end());
    }
    return miracl_bytes;
}

std::vector<uint8_t> strip_miracl_prefix_local(const std::vector<uint8_t>& miracl_bytes) {
    return miracl_g1_to_kilic(miracl_bytes);
}

std::vector<uint8_t> miracl_g2_to_kilic(const std::vector<uint8_t>& miracl_bytes) {
    if (miracl_bytes.size() > 1 && (miracl_bytes[0] == 0x04 || miracl_bytes[0] == 0x03 || miracl_bytes[0] == 0x02)) {
        return std::vector<uint8_t>(miracl_bytes.begin() + 1, miracl_bytes.end());
    }
    return miracl_bytes;
}

std::vector<uint8_t> kilic_to_miracl_g1(const std::vector<uint8_t>& kilic_bytes) {
    std::vector<uint8_t> result;
    result.reserve(kilic_bytes.size() + 1);
    result.push_back(0x04);
    result.insert(result.end(), kilic_bytes.begin(), kilic_bytes.end());
    return result;
}

std::vector<uint8_t> kilic_to_miracl_g2(const std::vector<uint8_t>& kilic_bytes) {
    std::vector<uint8_t> result;
    result.reserve(kilic_bytes.size() + 1);
    result.push_back(0x04);
    result.insert(result.end(), kilic_bytes.begin(), kilic_bytes.end());
    return result;
}

// ---------------------------------------------------------------------------
// Scalar wire conversion: exactly 32 bytes big-endian in, exactly 32 out.
// nitch::MiraclBls12381::scalar_to_bytes emits 48 bytes (MODBYTES_B384_58);
// the low 32 are the value, so trim to the LAST 32.
// ---------------------------------------------------------------------------
bool scalar_from_wire(nitch::MiraclBls12381* ctx, const uint8_t* in32, nitch::Scalar* out) {
    if (in32 == nullptr) return false;
    *out = ctx->scalar_from_bytes_mod_order(std::vector<uint8_t>(in32, in32 + NITCH_SCALAR_BYTES));
    return true;
}

std::vector<uint8_t> scalar_to_wire(nitch::MiraclBls12381* ctx, const nitch::Scalar& s) {
    const std::vector<uint8_t> wide = ctx->scalar_to_bytes(s);
    if (wide.size() < NITCH_SCALAR_BYTES) {
        // Cannot happen for B384_58 (MODBYTES=48 > 32); keep as a guard.
        std::vector<uint8_t> padded(NITCH_SCALAR_BYTES, 0);
        padded.insert(padded.end() - wide.size(), wide.begin(), wide.end());
        return padded;
    }
    return std::vector<uint8_t>(wide.end() - NITCH_SCALAR_BYTES, wide.end());
}

// ---------------------------------------------------------------------------
// Point wire conversion: kilic 96B/192B -> prepend 0x04 -> ECP[_2]fromOctet.
// Parse failure is reported to the caller; it maps to NITCH_ERR_CRYPTO (§0
// "point not on curve") or *valid=0 depending on the Go error/bool split of
// the function being mirrored.
// ---------------------------------------------------------------------------
bool parse_g1(const uint8_t* in96, nitch::G1Point* out) {
    if (in96 == nullptr) return false;
    const std::vector<uint8_t> miracl_bytes = kilic_to_miracl_g1(
        std::vector<uint8_t>(in96, in96 + NITCH_G1_BYTES));
    char buf[256];
    std::memcpy(buf, miracl_bytes.data(), miracl_bytes.size());
    octet oct = make_octet(buf, static_cast<int>(sizeof(buf)));
    oct.len = static_cast<int>(miracl_bytes.size());
    return ECP_fromOctet(&out->value, &oct) != 0;
}

bool parse_g2(const uint8_t* in192, nitch::G2Point* out) {
    if (in192 == nullptr) return false;
    const std::vector<uint8_t> miracl_bytes = kilic_to_miracl_g2(
        std::vector<uint8_t>(in192, in192 + NITCH_G2_BYTES));
    char buf[384];
    std::memcpy(buf, miracl_bytes.data(), miracl_bytes.size());
    octet oct = make_octet(buf, static_cast<int>(sizeof(buf)));
    oct.len = static_cast<int>(miracl_bytes.size());
    return ECP2_fromOctet(&out->value, &oct) != 0;
}

std::vector<uint8_t> serialize_g1(nitch::MiraclBls12381* ctx, const nitch::G1Point& p) {
    return miracl_g1_to_kilic(ctx->g1_to_bytes(p, false));
}

std::vector<uint8_t> serialize_g2(nitch::MiraclBls12381* ctx, const nitch::G2Point& p) {
    return miracl_g2_to_kilic(ctx->g2_to_bytes(p, false));
}

// ---------------------------------------------------------------------------
// Buffer helper: copy `bytes` into `out` (capacity `cap`), set *out_len (if
// non-null) always, return true when it fit. Contract §0: NITCH_ERR_BUFFER +
// *out_needed on too-small writes.
// ---------------------------------------------------------------------------
bool write_out(const std::vector<uint8_t>& bytes, uint8_t* out, uint32_t cap, uint32_t* out_len) {
    if (out_len != nullptr) *out_len = static_cast<uint32_t>(bytes.size());
    if (out == nullptr || bytes.size() > cap) return false;
    std::memcpy(out, bytes.data(), bytes.size());
    return true;
}

bool is_power_of_two_int(int value) {
    return value > 0 && (value & (value - 1)) == 0;
}

// ---------------------------------------------------------------------------
// Domain helpers — copied from sgx/enclave/enclave.cpp (~988-1033, ~1330-1387).
// ---------------------------------------------------------------------------
nitch::Scalar evaluation_point_for_index(
    nitch::MiraclBls12381* ctx,
    int domain_size,
    int participant_index)
{
    const nitch::EvaluationDomain& domain = ctx->evaluation_domain(domain_size);
    return domain.powers[static_cast<size_t>(participant_index % domain.size)];
}

std::vector<nitch::Scalar> batch_inverse_scalars(
    nitch::MiraclBls12381* ctx,
    const std::vector<nitch::Scalar>& values)
{
    std::vector<nitch::Scalar> inverses;
    if (values.empty()) {
        return inverses;
    }

    const nitch::Scalar zero = ctx->scalar_from_u64(0);
    std::vector<nitch::Scalar> prefix(values.size(), zero);
    prefix[0] = values[0];
    if (ctx->scalar_equal(prefix[0], zero)) {
        return {};
    }

    for (size_t i = 1; i < values.size(); ++i) {
        if (ctx->scalar_equal(values[i], zero)) {
            return {};
        }
        prefix[i] = ctx->scalar_mul(prefix[i - 1], values[i]);
    }

    nitch::Scalar inverse_total = ctx->scalar_inv(prefix.back());
    inverses.assign(values.size(), zero);
    for (size_t i = values.size(); i-- > 0;) {
        const nitch::Scalar before = (i == 0) ? ctx->scalar_from_u64(1) : prefix[i - 1];
        inverses[i] = ctx->scalar_mul(inverse_total, before);
        inverse_total = ctx->scalar_mul(inverse_total, values[i]);
    }
    return inverses;
}

std::vector<nitch::Scalar> compute_lagrange_at_zero(
    nitch::MiraclBls12381* ctx,
    int domain_size,
    const int32_t* indices,
    uint32_t count)
{
    if (!is_power_of_two_int(domain_size) || count == 0 || count > static_cast<uint32_t>(domain_size)) {
        return {};
    }

    std::unordered_set<int32_t> seen;
    seen.reserve(count);
    std::vector<nitch::Scalar> points;
    points.reserve(count);
    for (uint32_t i = 0; i < count; i++) {
        if (indices[i] <= 0 || indices[i] > domain_size || !seen.insert(indices[i]).second) {
            return {};
        }
        points.push_back(evaluation_point_for_index(ctx, domain_size, indices[i]));
    }

    const nitch::EvaluationDomain& domain = ctx->evaluation_domain(domain_size);
    if (static_cast<uint32_t>(domain.size) == count) {
        return std::vector<nitch::Scalar>(count, domain.inverse_size);
    }

    std::vector<nitch::Scalar> coefficients;
    coefficients.reserve(count);
    std::vector<nitch::Scalar> denominators;
    denominators.reserve(count);

    for (uint32_t i = 0; i < count; i++) {
        nitch::Scalar numerator = ctx->scalar_from_u64(1);
        nitch::Scalar denominator = ctx->scalar_from_u64(1);
        const nitch::Scalar& i_scalar = points[i];

        for (uint32_t j = 0; j < count; j++) {
            if (i == j) continue;
            nitch::Scalar minus_j = ctx->scalar_neg(points[j]);
            numerator = ctx->scalar_mul(numerator, minus_j);
            const nitch::Scalar& j_scalar = points[j];
            denominator = ctx->scalar_mul(denominator, ctx->scalar_sub(i_scalar, j_scalar));
        }

        coefficients.push_back(numerator);
        denominators.push_back(denominator);
    }

    const std::vector<nitch::Scalar> denominator_inverses = batch_inverse_scalars(ctx, denominators);
    if (denominator_inverses.size() != count) {
        return {};
    }
    for (uint32_t i = 0; i < count; i++) {
        coefficients[i] = ctx->scalar_mul(coefficients[i], denominator_inverses[i]);
    }

    return coefficients;
}

// ---------------------------------------------------------------------------
// Shared polynomial/commitment body for distkg_create_deal and dkrs_create_deal
// (enclave.cpp ~1088-1163 and ~2160-2242; identical once coeffs are chosen and
// the peer-encryption framing is stripped).
// ---------------------------------------------------------------------------
struct DealOutput {
    std::vector<uint8_t> commitments_a;  // (t+1)*96 kilic
    std::vector<uint8_t> commitment_b0;  // 192 kilic
    std::vector<uint8_t> shares;         // n*32
};

DealOutput build_deal(
    nitch::MiraclBls12381* ctx,
    const std::vector<nitch::Scalar>& coeffs,
    int threshold,
    int num_parties)
{
    (void)ctx->evaluation_domain(num_parties);

    DealOutput out;
    out.commitments_a.reserve(static_cast<size_t>(threshold + 1) * NITCH_G1_BYTES);
    for (int k = 0; k <= threshold; k++) {
        nitch::G1Point pt = ctx->g1_mul(ctx->g1_generator(), coeffs[k]);
        std::vector<uint8_t> pt_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(pt, false));
        if (static_cast<int>(pt_bytes.size()) != NITCH_G1_BYTES) {
            throw std::runtime_error("unexpected G1 serialization size");
        }
        out.commitments_a.insert(out.commitments_a.end(), pt_bytes.begin(), pt_bytes.end());
    }

    nitch::G2Point b0 = ctx->g2_mul(ctx->g2_generator(), coeffs[0]);
    out.commitment_b0 = miracl_g2_to_kilic(ctx->g2_to_bytes(b0, false));
    if (out.commitment_b0.size() != NITCH_G2_BYTES) {
        throw std::runtime_error("unexpected G2 serialization size");
    }

    out.shares.reserve(static_cast<size_t>(num_parties) * NITCH_SCALAR_BYTES);
    for (int j = 1; j <= num_parties; j++) {
        nitch::Scalar x_scalar = evaluation_point_for_index(ctx, num_parties, j);
        nitch::Scalar eval_result = ctx->scalar_from_u64(0);
        for (int k = threshold; k >= 0; k--) {
            eval_result = ctx->scalar_mul(eval_result, x_scalar);
            eval_result = ctx->scalar_add(eval_result, coeffs[k]);
        }
        std::vector<uint8_t> share_bytes = scalar_to_wire(ctx, eval_result);
        out.shares.insert(out.shares.end(), share_bytes.begin(), share_bytes.end());
    }
    return out;
}

// ---------------------------------------------------------------------------
// Shared commitment-evaluation body for distkg_verify_share and
// dkrs_verify_share (enclave.cpp ~1260-1297 and ~2349-2386). Returns 0 on
// success with *ok set, non-zero on parameter/crypto error.
// ---------------------------------------------------------------------------
int evaluate_commitments(
    nitch::MiraclBls12381* ctx,
    int threshold,
    int num_parties,
    int receiver_index,
    const uint8_t* commitments_a,
    uint32_t commitments_a_len,
    const uint8_t* share,        // 32B, may be any bytes
    bool* ok)
{
    *ok = false;
    if (commitments_a == nullptr || share == nullptr) return NITCH_ERR_PARAM;
    if (static_cast<uint32_t>((threshold + 1) * NITCH_G1_BYTES) != commitments_a_len) {
        return NITCH_ERR_PARAM;
    }

    std::vector<nitch::G1Point> a_points;
    a_points.reserve(threshold + 1);
    for (int k = 0; k <= threshold; k++) {
        nitch::G1Point pt;
        if (!parse_g1(commitments_a + static_cast<size_t>(k) * NITCH_G1_BYTES, &pt)) {
            return NITCH_ERR_CRYPTO;
        }
        a_points.push_back(pt);
    }

    nitch::G1Point rhs = ctx->g1_identity();
    nitch::Scalar j_power = ctx->scalar_from_u64(1);
    nitch::Scalar j_scalar = evaluation_point_for_index(ctx, num_parties, receiver_index);

    for (int k = 0; k <= threshold; k++) {
        nitch::G1Point term = ctx->g1_mul(a_points[k], j_power);
        rhs = ctx->g1_add(rhs, term);
        j_power = ctx->scalar_mul(j_power, j_scalar);
    }

    nitch::Scalar share_scalar;
    if (!scalar_from_wire(ctx, share, &share_scalar)) return NITCH_ERR_PARAM;
    nitch::G1Point lhs = ctx->g1_mul(ctx->g1_generator(), share_scalar);

    *ok = ctx->g1_equal(lhs, rhs);
    return NITCH_OK;
}

} // namespace

// ===========================================================================
// Exception barrier. Every exported function below is exactly this wrapper
// around an impl_ function (design requirement 2): no exception crosses the
// C boundary. std::invalid_argument maps to NITCH_ERR_PARAM, every other
// std::exception to NITCH_ERR_CRYPTO, anything else to NITCH_ERR_INTERNAL.
// ===========================================================================
extern "C" {

#define NITCH_TRY(expr)                                          \
    do {                                                         \
        try {                                                    \
            return (expr);                                       \
        } catch (const std::invalid_argument&) {                 \
            return NITCH_ERR_PARAM;                              \
        } catch (const std::exception&) {                        \
            return NITCH_ERR_CRYPTO;                             \
        } catch (...) {                                          \
            return NITCH_ERR_INTERNAL;                           \
        }                                                        \
    } while (0)

// ---------------------------------------------------------------------------
// DistKG (§1)
// ---------------------------------------------------------------------------

// §1.1 nitch_distkg_create_deal — enclave.cpp ecall_distkg_create_deal
// (~1044) with the cert checks, peer-key framing and g1/g2/p_hex out-params of
// the enclave signature reduced to the optional p_hex the contract keeps.
// Parameter constraints mirror the Go CreateDistKGDeal / enclave check:
// t < n, n a power of two, 1 <= dealer_index <= n.
int impl_nitch_distkg_create_deal(
    int dealer_index, int threshold, int num_parties,
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares,
    char* out_p_hex, uint32_t p_hex_cap, uint32_t* out_p_hex_len)
{
    if (!is_power_of_two_int(num_parties) || threshold < 0 || threshold >= num_parties ||
        dealer_index < 1 || dealer_index > num_parties) {
        return NITCH_ERR_PARAM;
    }
    if (commitments_a == nullptr || commitment_b0 == nullptr || shares_out == nullptr) {
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    std::vector<nitch::Scalar> coeffs;
    coeffs.reserve(threshold + 1);
    for (int k = 0; k <= threshold; k++) {
        coeffs.push_back(ctx->random_scalar());
    }

    const DealOutput deal = build_deal(ctx, coeffs, threshold, num_parties);

    if (!write_out(deal.commitments_a, commitments_a, commitments_a_cap, out_len_a)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(deal.commitment_b0, commitment_b0, commitment_b0_cap, out_len_b0)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(deal.shares, shares_out, shares_out_cap, out_len_shares)) {
        return NITCH_ERR_BUFFER;
    }

    if (out_p_hex != nullptr) {
        // Go emits pp.p.Text(16): minimal lowercase hex, no leading zeros.
        nitch::Scalar order_scalar;
        B384_58::BIG_rcopy(order_scalar.value, BLS12381::CURVE_Order);
        const std::string order_hex = nitch::bytes_to_hex(ctx->scalar_to_bytes(order_scalar));
        size_t first_non_zero = order_hex.find_first_not_of('0');
        const std::string minimal =
            (first_non_zero == std::string::npos) ? "0" : order_hex.substr(first_non_zero);
        if (out_p_hex_len != nullptr) *out_p_hex_len = static_cast<uint32_t>(minimal.size() + 1);
        if (p_hex_cap < minimal.size() + 1) return NITCH_ERR_BUFFER;
        std::memcpy(out_p_hex, minimal.c_str(), minimal.size() + 1);
    } else if (out_p_hex_len != nullptr) {
        *out_p_hex_len = 0;
    }

    return NITCH_OK;
}

int nitch_distkg_create_deal(
    int dealer_index, int threshold, int num_parties,
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares,
    char* out_p_hex, uint32_t p_hex_cap, uint32_t* out_p_hex_len)
{
    NITCH_TRY(impl_nitch_distkg_create_deal(
        dealer_index, threshold, num_parties,
        commitments_a, commitments_a_cap, out_len_a,
        commitment_b0, commitment_b0_cap, out_len_b0,
        shares_out, shares_out_cap, out_len_shares,
        out_p_hex, p_hex_cap, out_p_hex_len));
}

// §1.2 nitch_distkg_verify_share — enclave.cpp ecall_distkg_verify_share
// (~1248). Contract §1.2 fixes the step ORDER: pairing check
// e(g1,b0)==e(A0,g2) FIRST, then the commitment evaluation. (The enclave
// evaluates first and pairs second; the contract is authoritative.)
//
// Go error/bool split (pureVerifyDistKGShare): a malformed A commitment or B0
// is an ERROR (FromBytes failure -> (false, err)); the pairing mismatch and the
// share mismatch are (false, nil). Mirrored here: parse failure of A or b0 =>
// NITCH_ERR_CRYPTO; both check failures => *valid=0, NITCH_OK.
int impl_nitch_distkg_verify_share(
    int threshold, int num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0,
    const uint8_t* share,
    int* valid)
{
    *valid = 0;
    if (valid == nullptr) return NITCH_ERR_PARAM;
    if (threshold < 0 || threshold >= num_parties ||
        receiver_index < 1 || receiver_index > num_parties) {
        return NITCH_ERR_PARAM;
    }
    if (commitments_a == nullptr || commitment_b0 == nullptr || share == nullptr) {
        return NITCH_ERR_PARAM;
    }
    if (static_cast<uint32_t>((threshold + 1) * NITCH_G1_BYTES) != commitments_a_len) {
        // Go: "invalid deal A length" -> (false, err). PARAM, not a valid=0.
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    // Parse all commitments up front (Go parses every A_k and B0 before
    // checking anything; a parse failure is an error, never valid=0).
    std::vector<nitch::G1Point> a_points;
    a_points.reserve(threshold + 1);
    for (int k = 0; k <= threshold; k++) {
        nitch::G1Point pt;
        if (!parse_g1(commitments_a + static_cast<size_t>(k) * NITCH_G1_BYTES, &pt)) {
            return NITCH_ERR_CRYPTO;
        }
        a_points.push_back(pt);
    }
    nitch::G2Point b0_pt;
    if (!parse_g2(commitment_b0, &b0_pt)) {
        return NITCH_ERR_CRYPTO;
    }

    // (a) pairing check: e(g1, b0) == e(A0, g2), fixed generators (§0).
    nitch::G1Point g1_gen = ctx->g1_generator();
    nitch::G2Point g2_gen = ctx->g2_generator();
    nitch::GTPoint lhs_pair = ctx->pairing(g1_gen, b0_pt);
    nitch::GTPoint rhs_pair = ctx->pairing(a_points[0], g2_gen);
    if (!ctx->gt_equal(lhs_pair, rhs_pair)) {
        *valid = 0;
        return NITCH_OK;
    }

    // (b) commitment evaluation over the n-party domain.
    bool ok = false;
    const int rc = evaluate_commitments(
        ctx, threshold, num_parties, receiver_index,
        commitments_a, commitments_a_len, share, &ok);
    if (rc != NITCH_OK) return rc;

    *valid = ok ? 1 : 0;
    return NITCH_OK;
}

int nitch_distkg_verify_share(
    int threshold, int num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0,
    const uint8_t* share,
    int* valid)
{
    NITCH_TRY(impl_nitch_distkg_verify_share(
        threshold, num_parties, receiver_index,
        commitments_a, commitments_a_len, commitment_b0, share, valid));
}

// §1.3 nitch_distkg_extract — point/parse/serialize plumbing transcribed from
// enclave.cpp ecall_distkg_extract (~1389), with the aggregation replaced by
// the contract's PLAIN SUM (nitch Go DistKGExtract): pk=sum B0, pkG1=sum A0,
// sk=sum s_i, vk=g1^sk. NO Lagrange weighting, no g_secret_share side effects.
// Parameter constraints mirror Go DistKGExtract + the enclave: non-empty QUAL,
// 1-based indices, t < n, my_index in [1,n].
int impl_nitch_distkg_extract(
    int my_index, int threshold, int num_parties,
    const int32_t* qual, uint32_t qual_count,
    const uint8_t* b0s,
    const uint8_t* a0s,
    const uint8_t* shares,
    uint8_t* out_pk, uint32_t pk_cap, uint32_t* out_pk_len,
    uint8_t* out_pk_g1, uint32_t pk_g1_cap, uint32_t* out_pk_g1_len,
    uint8_t* out_vk, uint32_t vk_cap, uint32_t* out_vk_len,
    uint8_t* out_sk, uint32_t sk_cap, uint32_t* out_sk_len)
{
    if (qual == nullptr || b0s == nullptr || a0s == nullptr || shares == nullptr ||
        out_pk == nullptr || out_pk_g1 == nullptr || out_vk == nullptr || out_sk == nullptr) {
        return NITCH_ERR_PARAM;
    }
    if (qual_count == 0) return NITCH_ERR_PARAM;
    if (!is_power_of_two_int(num_parties) || threshold < 0 || threshold >= num_parties ||
        my_index < 1 || my_index > num_parties) {
        return NITCH_ERR_PARAM;
    }
    for (uint32_t q = 0; q < qual_count; q++) {
        if (qual[q] < 1 || qual[q] > num_parties) return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    // pk = sum B0  (G2)
    nitch::G2Point pk = ctx->g2_identity();
    for (uint32_t q = 0; q < qual_count; q++) {
        nitch::G2Point b0_pt;
        if (!parse_g2(b0s + static_cast<size_t>(q) * NITCH_G2_BYTES, &b0_pt)) {
            return NITCH_ERR_CRYPTO;
        }
        pk = ctx->g2_add(pk, b0_pt);
    }

    // pkG1 = sum A0  (G1)
    nitch::G1Point pk_g1 = ctx->g1_identity();
    for (uint32_t q = 0; q < qual_count; q++) {
        nitch::G1Point a0_pt;
        if (!parse_g1(a0s + static_cast<size_t>(q) * NITCH_G1_BYTES, &a0_pt)) {
            return NITCH_ERR_CRYPTO;
        }
        pk_g1 = ctx->g1_add(pk_g1, a0_pt);
    }

    // sk = sum s_i  (plain sum, mod r via scalar_add)
    nitch::Scalar sk = ctx->scalar_from_u64(0);
    for (uint32_t q = 0; q < qual_count; q++) {
        nitch::Scalar s;
        if (!scalar_from_wire(ctx, shares + static_cast<size_t>(q) * NITCH_SCALAR_BYTES, &s)) {
            return NITCH_ERR_PARAM;
        }
        sk = ctx->scalar_add(sk, s);
    }

    // vk = g1^sk
    nitch::G1Point vk = ctx->g1_mul(ctx->g1_generator(), sk);

    if (!write_out(serialize_g2(ctx, pk), out_pk, pk_cap, out_pk_len)) return NITCH_ERR_BUFFER;
    if (!write_out(serialize_g1(ctx, pk_g1), out_pk_g1, pk_g1_cap, out_pk_g1_len)) return NITCH_ERR_BUFFER;
    if (!write_out(serialize_g1(ctx, vk), out_vk, vk_cap, out_vk_len)) return NITCH_ERR_BUFFER;
    if (!write_out(scalar_to_wire(ctx, sk), out_sk, sk_cap, out_sk_len)) return NITCH_ERR_BUFFER;
    return NITCH_OK;
}

int nitch_distkg_extract(
    int my_index, int threshold, int num_parties,
    const int32_t* qual, uint32_t qual_count,
    const uint8_t* b0s,
    const uint8_t* a0s,
    const uint8_t* shares,
    uint8_t* out_pk, uint32_t pk_cap, uint32_t* out_pk_len,
    uint8_t* out_pk_g1, uint32_t pk_g1_cap, uint32_t* out_pk_g1_len,
    uint8_t* out_vk, uint32_t vk_cap, uint32_t* out_vk_len,
    uint8_t* out_sk, uint32_t sk_cap, uint32_t* out_sk_len)
{
    NITCH_TRY(impl_nitch_distkg_extract(
        my_index, threshold, num_parties, qual, qual_count,
        b0s, a0s, shares,
        out_pk, pk_cap, out_pk_len,
        out_pk_g1, pk_g1_cap, out_pk_g1_len,
        out_vk, vk_cap, out_vk_len,
        out_sk, sk_cap, out_sk_len));
}

// ---------------------------------------------------------------------------
// DKRS (§2)
// ---------------------------------------------------------------------------

// §2.1 nitch_dkrs_create_deal — enclave.cpp ecall_dkrs_create_deal (~2108):
// coeff_0 = old_secret_share (the constant term), coeff_1..t' random,
// commitments, shares evaluated in the NEW domain (num_parties = n').
int impl_nitch_dkrs_create_deal(
    int dealer_index, int new_threshold, int new_num_parties,
    const uint8_t* old_secret_share,
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares)
{
    if (old_secret_share == nullptr ||
        commitments_a == nullptr || commitment_b0 == nullptr || shares_out == nullptr) {
        return NITCH_ERR_PARAM;
    }
    if (!is_power_of_two_int(new_num_parties) || new_threshold < 0 ||
        new_threshold >= new_num_parties || dealer_index < 1) {
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    nitch::Scalar old_sk;
    if (!scalar_from_wire(ctx, old_secret_share, &old_sk)) return NITCH_ERR_PARAM;

    std::vector<nitch::Scalar> coeffs;
    coeffs.reserve(new_threshold + 1);
    coeffs.push_back(old_sk);
    for (int k = 1; k <= new_threshold; k++) {
        coeffs.push_back(ctx->random_scalar());
    }

    const DealOutput deal = build_deal(ctx, coeffs, new_threshold, new_num_parties);

    if (!write_out(deal.commitments_a, commitments_a, commitments_a_cap, out_len_a)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(deal.commitment_b0, commitment_b0, commitment_b0_cap, out_len_b0)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(deal.shares, shares_out, shares_out_cap, out_len_shares)) {
        return NITCH_ERR_BUFFER;
    }
    return NITCH_OK;
}

int nitch_dkrs_create_deal(
    int dealer_index, int new_threshold, int new_num_parties,
    const uint8_t* old_secret_share,
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares)
{
    NITCH_TRY(impl_nitch_dkrs_create_deal(
        dealer_index, new_threshold, new_num_parties, old_secret_share,
        commitments_a, commitments_a_cap, out_len_a,
        commitment_b0, commitment_b0_cap, out_len_b0,
        shares_out, shares_out_cap, out_len_shares));
}

// §2.2 nitch_dkrs_verify_share — commitment evaluation followed by the
// same A0/B0 pairing consistency check as enclave.cpp ecall_dkrs_verify_share.
//
// Go error/bool split (pureVerifyDKRSShare): a malformed A commitment is an
// ERROR (FromBytes -> (false, err)); a share mismatch is (false, nil).
int impl_nitch_dkrs_verify_share(
    int new_threshold, int new_num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0,
    const uint8_t* share,
    int* valid)
{
    if (valid == nullptr) return NITCH_ERR_PARAM;
    *valid = 0;
    if (new_threshold < 0 || new_threshold >= new_num_parties ||
        receiver_index < 1 || receiver_index > new_num_parties) {
        return NITCH_ERR_PARAM;
    }
    if (commitments_a == nullptr || commitment_b0 == nullptr || share == nullptr) return NITCH_ERR_PARAM;
    if (static_cast<uint32_t>((new_threshold + 1) * NITCH_G1_BYTES) != commitments_a_len) {
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();
    bool ok = false;
    const int rc = evaluate_commitments(
        ctx, new_threshold, new_num_parties, receiver_index,
        commitments_a, commitments_a_len, share, &ok);
    if (rc != NITCH_OK) return rc;
    if (!ok) return NITCH_OK;

    nitch::G1Point a0;
    nitch::G2Point b0;
    if (!parse_g1(commitments_a, &a0) || !parse_g2(commitment_b0, &b0)) {
        return NITCH_ERR_CRYPTO;
    }
    const nitch::GTPoint lhs_pair = ctx->pairing(a0, ctx->g2_generator());
    const nitch::GTPoint rhs_pair = ctx->pairing(ctx->g1_generator(), b0);
    *valid = ctx->gt_equal(lhs_pair, rhs_pair) ? 1 : 0;
    return NITCH_OK;
}

int nitch_dkrs_verify_share(
    int new_threshold, int new_num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0,
    const uint8_t* share,
    int* valid)
{
    NITCH_TRY(impl_nitch_dkrs_verify_share(
        new_threshold, new_num_parties, receiver_index,
        commitments_a, commitments_a_len, commitment_b0, share, valid));
}

// §2.3 nitch_dkrs_extract — enclave.cpp ecall_dkrs_extract (~2418) with the
// contract's changes: Lagrange weights over the OLD domain (compute_lagrange_
// at_zero(old_num_parties, qual_old)), sk = sum lambda_i*s_i, vk = g1^sk, and
// NO global_pk output (Go inherits it from currentKey; C produces sk/vk only).
// b0s/a0s stay in the signature for parity but are deliberately unused.
int impl_nitch_dkrs_extract(
    int my_new_index, int new_threshold, int new_num_parties,
    int old_num_parties,
    const int32_t* qual_old, uint32_t qual_old_count,
    const uint8_t* b0s,
    const uint8_t* a0s,
    const uint8_t* shares,
    uint8_t* out_sk, uint32_t sk_cap, uint32_t* out_sk_len,
    uint8_t* out_vk, uint32_t vk_cap, uint32_t* out_vk_len)
{
    if (qual_old == nullptr || shares == nullptr || out_sk == nullptr || out_vk == nullptr) {
        return NITCH_ERR_PARAM;
    }
    if (qual_old_count == 0) return NITCH_ERR_PARAM;
    if (!is_power_of_two_int(new_num_parties) || !is_power_of_two_int(old_num_parties) ||
        new_threshold < 0 || new_threshold >= new_num_parties ||
        my_new_index < 1 || my_new_index > new_num_parties) {
        return NITCH_ERR_PARAM;
    }
    (void)b0s;
    (void)a0s;

    nitch::MiraclBls12381* ctx = tls_ctx();

    const std::vector<nitch::Scalar> lambdas =
        compute_lagrange_at_zero(ctx, old_num_parties, qual_old, qual_old_count);
    if (lambdas.size() != qual_old_count) {
        // compute_lagrange_at_zero rejects bad domains, out-of-range or
        // duplicated indices, and non-invertible denominators.
        return NITCH_ERR_CRYPTO;
    }

    nitch::Scalar sk = ctx->scalar_from_u64(0);
    for (uint32_t i = 0; i < qual_old_count; i++) {
        nitch::Scalar s;
        if (!scalar_from_wire(ctx, shares + static_cast<size_t>(i) * NITCH_SCALAR_BYTES, &s)) {
            return NITCH_ERR_PARAM;
        }
        sk = ctx->scalar_add(sk, ctx->scalar_mul(lambdas[i], s));
    }

    nitch::G1Point vk = ctx->g1_mul(ctx->g1_generator(), sk);

    if (!write_out(scalar_to_wire(ctx, sk), out_sk, sk_cap, out_sk_len)) return NITCH_ERR_BUFFER;
    if (!write_out(serialize_g1(ctx, vk), out_vk, vk_cap, out_vk_len)) return NITCH_ERR_BUFFER;
    return NITCH_OK;
}

int nitch_dkrs_extract(
    int my_new_index, int new_threshold, int new_num_parties,
    int old_num_parties,
    const int32_t* qual_old, uint32_t qual_old_count,
    const uint8_t* b0s,
    const uint8_t* a0s,
    const uint8_t* shares,
    uint8_t* out_sk, uint32_t sk_cap, uint32_t* out_sk_len,
    uint8_t* out_vk, uint32_t vk_cap, uint32_t* out_vk_len)
{
    NITCH_TRY(impl_nitch_dkrs_extract(
        my_new_index, new_threshold, new_num_parties, old_num_parties,
        qual_old, qual_old_count, b0s, a0s, shares,
        out_sk, sk_cap, out_sk_len, out_vk, vk_cap, out_vk_len));
}

// ---------------------------------------------------------------------------
// Adaptation (§3)
// ---------------------------------------------------------------------------

namespace {

// §3.1 transcript builder, shared by adapt and verify so the two sides can
// never drift: [g1_gen, y, vk, c, T1, T2], every element a G1 serialized
// uncompressed and prefix-stripped to 96 bytes (enclave.cpp ~1785-1795).
std::vector<uint8_t> build_transcript(
    nitch::MiraclBls12381* ctx,
    const nitch::G1Point& g1_gen,
    const nitch::G1Point& y,
    const nitch::G1Point& vk,
    const nitch::G1Point& c,
    const nitch::G1Point& t1,
    const nitch::G1Point& t2)
{
    std::vector<uint8_t> data;
    data.reserve(6 * NITCH_G1_BYTES);
    const nitch::G1Point* points[6] = {&g1_gen, &y, &vk, &c, &t1, &t2};
    for (const nitch::G1Point* p : points) {
        const std::vector<uint8_t> stripped =
            strip_miracl_prefix_local(ctx->g1_to_bytes(*p, false));
        data.insert(data.end(), stripped.begin(), stripped.end());
    }
    return data;
}

} // namespace

// §3.1 nitch_partial_adapt — pcollide_enclave_copy (bench/table_6_2_ree_bench.cpp
// 206-247) / the inline proof at enclave.cpp 1759-1799, with the secret passed
// explicitly instead of living in enclave globals.
int impl_nitch_partial_adapt(
    const uint8_t* secret_share,
    const uint8_t* verification_key_g1,
    int index,
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    uint8_t* out_contribution, uint32_t contribution_cap, uint32_t* out_contribution_len,
    uint8_t* out_challenge, uint32_t challenge_cap, uint32_t* out_challenge_len,
    uint8_t* out_response, uint32_t response_cap, uint32_t* out_response_len)
{
    (void)index;  // carried in the share tuple, not used by the computation
    if (secret_share == nullptr || verification_key_g1 == nullptr ||
        source_msg == nullptr || target_msg == nullptr ||
        out_contribution == nullptr || out_challenge == nullptr || out_response == nullptr) {
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    nitch::Scalar sk;
    if (!scalar_from_wire(ctx, secret_share, &sk)) return NITCH_ERR_PARAM;
    nitch::G1Point vk;
    if (!parse_g1(verification_key_g1, &vk)) return NITCH_ERR_CRYPTO;

    const std::string src_str(reinterpret_cast<const char*>(source_msg), source_len);
    const std::string tgt_str(reinterpret_cast<const char*>(target_msg), target_len);

    const nitch::G1Point h1m = ctx->hash_to_g1(src_str);
    const nitch::G1Point h1m_prime = ctx->hash_to_g1(tgt_str);
    const nitch::G1Point y = ctx->g1_sub(h1m, h1m_prime);
    const nitch::G1Point contribution = ctx->g1_mul(y, sk);

    const nitch::Scalar tau = ctx->random_scalar();
    const nitch::G1Point g1_gen = ctx->g1_generator();
    const nitch::G1Point t1 = ctx->g1_mul(g1_gen, tau);
    const nitch::G1Point t2 = ctx->g1_mul(y, tau);

    const std::vector<uint8_t> transcript =
        build_transcript(ctx, g1_gen, y, vk, contribution, t1, t2);
    const nitch::Scalar challenge = ctx->hash_to_scalar(transcript);
    const nitch::Scalar response = ctx->scalar_add(tau, ctx->scalar_mul(challenge, sk));

    if (!write_out(serialize_g1(ctx, contribution), out_contribution, contribution_cap, out_contribution_len)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(scalar_to_wire(ctx, challenge), out_challenge, challenge_cap, out_challenge_len)) {
        return NITCH_ERR_BUFFER;
    }
    if (!write_out(scalar_to_wire(ctx, response), out_response, response_cap, out_response_len)) {
        return NITCH_ERR_BUFFER;
    }
    return NITCH_OK;
}

int nitch_partial_adapt(
    const uint8_t* secret_share,
    const uint8_t* verification_key_g1,
    int index,
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    uint8_t* out_contribution, uint32_t contribution_cap, uint32_t* out_contribution_len,
    uint8_t* out_challenge, uint32_t challenge_cap, uint32_t* out_challenge_len,
    uint8_t* out_response, uint32_t response_cap, uint32_t* out_response_len)
{
    NITCH_TRY(impl_nitch_partial_adapt(
        secret_share, verification_key_g1, index,
        source_msg, source_len, target_msg, target_len,
        out_contribution, contribution_cap, out_contribution_len,
        out_challenge, challenge_cap, out_challenge_len,
        out_response, response_cap, out_response_len));
}

// §3.2 nitch_partial_verify — mirror of Go verifyNIZKProof:
//   T1' = g1^z * (vk^w)^-1 ; T2' = y^z * (c^w)^-1
//   transcript = [g1, y, vk, c, T1', T2'] ; expected_w = H2(transcript)
//   valid = (w == expected_w)   (Go: share.Proof.Omega.Cmp(expectedOmega) == 0)
// Parse failures of vk/contribution and wrong-length scalars make the whole
// verification fail (valid=0, NITCH_OK), matching Go's bool return.
int impl_nitch_partial_verify(
    const uint8_t* verification_key_g1,
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    const uint8_t* contribution,
    const uint8_t* challenge,
    const uint8_t* response,
    int* valid)
{
    *valid = 0;
    if (valid == nullptr) return NITCH_ERR_PARAM;
    if (verification_key_g1 == nullptr || source_msg == nullptr || target_msg == nullptr ||
        contribution == nullptr || challenge == nullptr || response == nullptr) {
        return NITCH_ERR_PARAM;
    }

    nitch::MiraclBls12381* ctx = tls_ctx();

    nitch::G1Point vk;
    nitch::G1Point c;
    nitch::Scalar omega;
    nitch::Scalar z;
    if (!parse_g1(verification_key_g1, &vk)) return NITCH_OK;
    if (!parse_g1(contribution, &c)) return NITCH_OK;
    scalar_from_wire(ctx, challenge, &omega);
    scalar_from_wire(ctx, response, &z);

    const std::string src_str(reinterpret_cast<const char*>(source_msg), source_len);
    const std::string tgt_str(reinterpret_cast<const char*>(target_msg), target_len);
    const nitch::G1Point h1m = ctx->hash_to_g1(src_str);
    const nitch::G1Point h1m_prime = ctx->hash_to_g1(tgt_str);
    const nitch::G1Point y = ctx->g1_sub(h1m, h1m_prime);

    const nitch::G1Point g1_gen = ctx->g1_generator();
    const nitch::G1Point g1z = ctx->g1_mul(g1_gen, z);
    const nitch::G1Point vk_omega = ctx->g1_mul(vk, omega);
    const nitch::G1Point t1_prime = ctx->g1_sub(g1z, vk_omega);
    const nitch::G1Point yz = ctx->g1_mul(y, z);
    const nitch::G1Point c_omega = ctx->g1_mul(c, omega);
    const nitch::G1Point t2_prime = ctx->g1_sub(yz, c_omega);

    const std::vector<uint8_t> transcript =
        build_transcript(ctx, g1_gen, y, vk, c, t1_prime, t2_prime);
    const nitch::Scalar expected_omega = ctx->hash_to_scalar(transcript);

    *valid = ctx->scalar_equal(omega, expected_omega) ? 1 : 0;
    return NITCH_OK;
}

int nitch_partial_verify(
    const uint8_t* verification_key_g1,
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    const uint8_t* contribution,
    const uint8_t* challenge,
    const uint8_t* response,
    int* valid)
{
    NITCH_TRY(impl_nitch_partial_verify(
        verification_key_g1, source_msg, source_len, target_msg, target_len,
        contribution, challenge, response, valid));
}

// ---------------------------------------------------------------------------
// Primitives (§4)
// ---------------------------------------------------------------------------

// §4: H1 — hash a message to G1, out = 96B kilic.
int impl_nitch_hash_to_g1(const uint8_t* msg, uint32_t len,
                          uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    if (msg == nullptr || out == nullptr) return NITCH_ERR_PARAM;
    nitch::MiraclBls12381* ctx = tls_ctx();
    const nitch::G1Point p =
        ctx->hash_to_g1(std::string(reinterpret_cast<const char*>(msg), len));
    if (!write_out(serialize_g1(ctx, p), out, out_cap, out_len)) return NITCH_ERR_BUFFER;
    return NITCH_OK;
}

int nitch_hash_to_g1(const uint8_t* msg, uint32_t len,
                     uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    NITCH_TRY(impl_nitch_hash_to_g1(msg, len, out, out_cap, out_len));
}

// §4: H2 — hash a transcript to a scalar, out = 32B.
int impl_nitch_hash_to_scalar(const uint8_t* data, uint32_t len,
                              uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    if (data == nullptr || out == nullptr) return NITCH_ERR_PARAM;
    nitch::MiraclBls12381* ctx = tls_ctx();
    const nitch::Scalar s = ctx->hash_to_scalar(std::vector<uint8_t>(data, data + len));
    if (!write_out(scalar_to_wire(ctx, s), out, out_cap, out_len)) return NITCH_ERR_BUFFER;
    return NITCH_OK;
}

int nitch_hash_to_scalar(const uint8_t* data, uint32_t len,
                         uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    NITCH_TRY(impl_nitch_hash_to_scalar(data, len, out, out_cap, out_len));
}

// §4: G1 point addition.
int impl_nitch_g1_add(const uint8_t* a, const uint8_t* b,
                      uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    if (a == nullptr || b == nullptr || out == nullptr) return NITCH_ERR_PARAM;
    nitch::MiraclBls12381* ctx = tls_ctx();
    nitch::G1Point pa;
    nitch::G1Point pb;
    if (!parse_g1(a, &pa)) return NITCH_ERR_CRYPTO;
    if (!parse_g1(b, &pb)) return NITCH_ERR_CRYPTO;
    if (!write_out(serialize_g1(ctx, ctx->g1_add(pa, pb)), out, out_cap, out_len)) {
        return NITCH_ERR_BUFFER;
    }
    return NITCH_OK;
}

int nitch_g1_add(const uint8_t* a, const uint8_t* b,
                 uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    NITCH_TRY(impl_nitch_g1_add(a, b, out, out_cap, out_len));
}

// §4: G1 scalar multiplication.
int impl_nitch_g1_mul_scalar(const uint8_t* a, const uint8_t* s,
                             uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    if (a == nullptr || s == nullptr || out == nullptr) return NITCH_ERR_PARAM;
    nitch::MiraclBls12381* ctx = tls_ctx();
    nitch::G1Point pa;
    nitch::Scalar sc;
    if (!parse_g1(a, &pa)) return NITCH_ERR_CRYPTO;
    if (!scalar_from_wire(ctx, s, &sc)) return NITCH_ERR_PARAM;
    if (!write_out(serialize_g1(ctx, ctx->g1_mul(pa, sc)), out, out_cap, out_len)) {
        return NITCH_ERR_BUFFER;
    }
    return NITCH_OK;
}

int nitch_g1_mul_scalar(const uint8_t* a, const uint8_t* s,
                        uint8_t* out, uint32_t out_cap, uint32_t* out_len) {
    NITCH_TRY(impl_nitch_g1_mul_scalar(a, s, out, out_cap, out_len));
}

// §4: pairing e(g1,g2), out_gt = 576B (12 * 48, MIRACL FP12 octet form).
int impl_nitch_pairing(const uint8_t* g1, const uint8_t* g2,
                       uint8_t* out_gt, uint32_t out_gt_cap, uint32_t* out_gt_len) {
    if (g1 == nullptr || g2 == nullptr || out_gt == nullptr) return NITCH_ERR_PARAM;
    nitch::MiraclBls12381* ctx = tls_ctx();
    nitch::G1Point pa;
    nitch::G2Point pb;
    if (!parse_g1(g1, &pa)) return NITCH_ERR_CRYPTO;
    if (!parse_g2(g2, &pb)) return NITCH_ERR_CRYPTO;
    const nitch::GTPoint gt = ctx->pairing(pa, pb);
    if (!write_out(ctx->gt_to_bytes(gt), out_gt, out_gt_cap, out_gt_len)) {
        return NITCH_ERR_BUFFER;
    }
    return NITCH_OK;
}

int nitch_pairing(const uint8_t* g1, const uint8_t* g2,
                  uint8_t* out_gt, uint32_t out_gt_cap, uint32_t* out_gt_len) {
    NITCH_TRY(impl_nitch_pairing(g1, g2, out_gt, out_gt_cap, out_gt_len));
}

} // extern "C"
