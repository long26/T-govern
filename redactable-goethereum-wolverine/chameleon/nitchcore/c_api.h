/*
 * c_api.h — C boundary over the vendored nitch BLS12-381 core (libnitchcore.a).
 *
 * Specification: docs/nitchcore-c-api-contract.md is authoritative for names,
 * byte formats, semantics and error codes. The "§N.M" references below point
 * into that contract.
 *
 * Byte formats (§0):
 *   - points travel in kilic format (no MIRACL 0x04 prefix): G1 uncompressed
 *     96 bytes, G2 uncompressed 192 bytes, GT 576 bytes.
 *   - scalars travel as exactly 32 bytes, big-endian, value < curve order.
 *   - shares are plaintext 32-byte scalars, no [len][flag] framing.
 *
 * Errors (§0): every function returns NITCH_OK on success or an NITCH_ERR_*
 * code. No function throws or longjmps across the C boundary. On
 * NITCH_ERR_BUFFER the written-length out-param (if non-null) receives the
 * number of bytes needed; this is defense-in-depth, the Go caller is expected
 * to allocate exact sizes.
 *
 * Context: a per-thread lazily constructed nitch::MiraclBls12381 lives inside
 * the library (thread_local, destructor at thread exit). There is no global
 * mutable state and no locking; pairings always use the fixed generators g1/g2
 * (§0).
 */
#ifndef NITCHCORE_C_API_H
#define NITCHCORE_C_API_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Fixed wire sizes (§0): kilic points and 32-byte scalars. */
#define NITCH_SCALAR_BYTES 32
#define NITCH_G1_BYTES     96
#define NITCH_G2_BYTES     192
#define NITCH_GT_BYTES     576

/* Error codes (§0). */
enum {
    NITCH_OK          = 0, /* success */
    NITCH_ERR_PARAM   = 1, /* invalid parameter (range/domain/null violation) */
    NITCH_ERR_BUFFER  = 2, /* out-buffer too small; *<out>_len holds the needed size */
    NITCH_ERR_CRYPTO  = 3, /* crypto failure (point not on curve, interpolation failed) */
    NITCH_ERR_INTERNAL = 4 /* internal error (unexpected exception) */
};

/* =====================================================================
 * DistKG (§1)
 * ===================================================================== */

/* §1.1: create a DistKG deal: (t+1) random coeffs, A_k=g1^coeff_k (96B kilic each),
 * b0=g2^coeff_0 (192B), shares f(w^j) (n*32B, shares_out[j-1..j*32) for party j);
 * out_p_hex (optional) receives the minimal lowercase group-order hex + NUL. */
int nitch_distkg_create_deal(
    int dealer_index, int threshold, int num_parties,
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares,
    char* out_p_hex, uint32_t p_hex_cap, uint32_t* out_p_hex_len);

/* §1.2: two-step share check — (a) e(g1,b0)==e(A0,g2), then (b) g1^s==sum_k
 * (w^j)^k*A_k over the n-party domain; any failed step => *valid=0, NITCH_OK. */
int nitch_distkg_verify_share(
    int threshold, int num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0,   /* 192B kilic */
    const uint8_t* share,           /* 32B */
    int* valid);

/* §1.3: plain-sum extraction (nitch Go semantics, NOT Lagrange): pk=sum B0
 * (192B), pk_g1=sum A0 (96B), sk=sum s_i (32B), vk=g1^sk (96B), QUAL-ordered. */
int nitch_distkg_extract(
    int my_index, int threshold, int num_parties,
    const int32_t* qual, uint32_t qual_count,
    const uint8_t* b0s,    /* qual_count*192B */
    const uint8_t* a0s,    /* qual_count*96B  */
    const uint8_t* shares, /* qual_count*32B  */
    uint8_t* out_pk,     uint32_t pk_cap,     uint32_t* out_pk_len,     /* 192B */
    uint8_t* out_pk_g1,  uint32_t pk_g1_cap,  uint32_t* out_pk_g1_len,  /* 96B  */
    uint8_t* out_vk,     uint32_t vk_cap,     uint32_t* out_vk_len,     /* 96B  */
    uint8_t* out_sk,     uint32_t sk_cap,     uint32_t* out_sk_len);    /* 32B  */

/* =====================================================================
 * DKRS / reshare (§2)
 * ===================================================================== */

/* §2.1: reshare deal: coeff_0=old_secret_share (const term), coeff_1..t' random,
 * A_k=g1^coeff_k, b0=g2^coeff_0, shares evaluated in the NEW (n') domain. */
int nitch_dkrs_create_deal(
    int dealer_index, int new_threshold, int new_num_parties,
    const uint8_t* old_secret_share, /* 32B */
    uint8_t* commitments_a, uint32_t commitments_a_cap, uint32_t* out_len_a,
    uint8_t* commitment_b0, uint32_t commitment_b0_cap, uint32_t* out_len_b0,
    uint8_t* shares_out, uint32_t shares_out_cap, uint32_t* out_len_shares);

/* §2.2: share commitment check over the n' domain, followed by
 * pairing consistency e(A0,g2)==e(g1,b0). */
int nitch_dkrs_verify_share(
    int new_threshold, int new_num_parties, int receiver_index,
    const uint8_t* commitments_a, uint32_t commitments_a_len,
    const uint8_t* commitment_b0, /* 192B */
    const uint8_t* share,           /* 32B */
    int* valid);

/* §2.3: Lagrange-weighted extraction over the OLD domain: lambda =
 * lagrange_at_zero(old_domain, qual_old), sk=sum lambda_i*s_i (32B), vk=g1^sk
 * (96B); global pk is NOT recomputed or output (Go inherits it from currentKey),
 * b0s/a0s are accepted for signature parity but unused. */
int nitch_dkrs_extract(
    int my_new_index, int new_threshold, int new_num_parties,
    int old_num_parties,
    const int32_t* qual_old, uint32_t qual_old_count,
    const uint8_t* b0s,    /* qual_old_count*192B (accepted, unused — §2.3) */
    const uint8_t* a0s,    /* qual_old_count*96B  (accepted, unused — §2.3) */
    const uint8_t* shares, /* qual_old_count*32B  */
    uint8_t* out_sk, uint32_t sk_cap, uint32_t* out_sk_len, /* 32B */
    uint8_t* out_vk, uint32_t vk_cap, uint32_t* out_vk_len);/* 96B */

/* =====================================================================
 * Adaptation (§3)
 * ===================================================================== */

/* §3.1: partial adapt = REE pcollide body: y=H1(m)-H1(m'), c=y^sk, tau random,
 * T1=g1^tau, T2=y^tau, transcript=[g1,y,vk,c,T1,T2] (each G1, 96B stripped),
 * challenge=H2(transcript), response=tau+challenge*sk. index is the member
 * index carried in the share tuple; it takes no part in the computation. */
int nitch_partial_adapt(
    const uint8_t* secret_share,         /* 32B */
    const uint8_t* verification_key_g1,  /* 96B kilic */
    int index,
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    uint8_t* out_contribution, uint32_t contribution_cap, uint32_t* out_contribution_len, /* 96B */
    uint8_t* out_challenge,    uint32_t challenge_cap,    uint32_t* out_challenge_len,    /* 32B */
    uint8_t* out_response,     uint32_t response_cap,     uint32_t* out_response_len);    /* 32B */

/* §3.2: verify a NIZK share: T1'=g1^z*(vk^w)^-1, T2'=y^z*(c^w)^-1, transcript
 * [g1,y,vk,c,T1',T2'], expected_w=H2(transcript), valid=(w==expected_w). */
int nitch_partial_verify(
    const uint8_t* verification_key_g1,  /* 96B kilic */
    const uint8_t* source_msg, uint32_t source_len,
    const uint8_t* target_msg, uint32_t target_len,
    const uint8_t* contribution, /* 96B kilic */
    const uint8_t* challenge,    /* 32B */
    const uint8_t* response,     /* 32B */
    int* valid);

/* =====================================================================
 * Primitives (§4)
 * ===================================================================== */

/* §4: H1 — hash a message to G1 (DST NITCH_BLS12381_H1), out = 96B kilic. */
int nitch_hash_to_g1(const uint8_t* msg, uint32_t len,
                     uint8_t* out, uint32_t out_cap, uint32_t* out_len);

/* §4: H2 — hash a transcript to a scalar (DST NITCH_BLS12381_H2), out = 32B. */
int nitch_hash_to_scalar(const uint8_t* data, uint32_t len,
                         uint8_t* out, uint32_t out_cap, uint32_t* out_len);

/* §4: G1 point addition, a and b are 96B kilic, out = 96B kilic. */
int nitch_g1_add(const uint8_t* a, const uint8_t* b,
                 uint8_t* out, uint32_t out_cap, uint32_t* out_len);

/* §4: G1 scalar multiplication, a is 96B kilic, s is 32B, out = 96B kilic. */
int nitch_g1_mul_scalar(const uint8_t* a, const uint8_t* s,
                        uint8_t* out, uint32_t out_cap, uint32_t* out_len);

/* §4: pairing e(g1,g2), inputs 96B/192B kilic, out_gt = 576B. */
int nitch_pairing(const uint8_t* g1, const uint8_t* g2,
                  uint8_t* out_gt, uint32_t out_gt_cap, uint32_t* out_gt_len);

#ifdef __cplusplus
} /* extern "C" */
#endif

#endif /* NITCHCORE_C_API_H */
