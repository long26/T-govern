// c_api_selftest — pure C driver over c_api.h (contract §6-style regression).
//
// Exercises every exported function end-to-end in a single thread:
//   1. primitives: H1/H2/g1_add/g1_mul_scalar/pairing round-trip
//   2. DistKG: create(4,2) -> verify each share -> extract -> vk == g1^sk
//   3. DKRS:   create -> verify -> extract (old-domain Lagrange)
//   4. adapt:  partial_adapt -> partial_verify valid
//   5. tamper: a flipped share byte -> verify returns valid=0 (both protocols)
//   6. buffers: too-small out-buffer -> NITCH_ERR_BUFFER + *out_needed
//
// Compiled as C (see the `selftest` Makefile target: gcc -x c) to prove the
// header is valid C and the boundary is pure C. Linking against the C++
// archive is fine: the C++ runtime is pulled in by the static libs' needs and
// the driver itself never constructs a C++ object.
#include "c_api.h"

#include <stdio.h>
#include <string.h>

static int failures = 0;
static int checks = 0;

static void check(const char* name, int cond) {
    checks++;
    if (cond) {
        printf("PASS %s\n", name);
    } else {
        printf("FAIL %s\n", name);
        failures++;
    }
}

static void check_rc(const char* name, int rc, int expected) {
    checks++;
    if (rc == expected) {
        printf("PASS %s (rc=%d)\n", name, rc);
    } else {
        printf("FAIL %s: rc=%d expected=%d\n", name, rc, expected);
        failures++;
    }
}

static void hex(const char* label, const uint8_t* p, uint32_t n) {
    printf("   %s=", label);
    for (uint32_t i = 0; i < n && i < 12; i++) printf("%02x", p[i]);
    printf("...(%u bytes)\n", n);
}

int main(void) {
    /* ---------------- primitives (§4) ---------------- */
    {
        const char* msg = "old";
        uint8_t h1a[NITCH_G1_BYTES], h1b[NITCH_G1_BYTES];
        uint32_t n1 = 0, n2 = 0;
        check_rc("hash_to_g1", nitch_hash_to_g1((const uint8_t*)msg, 3, h1a, sizeof(h1a), &n1), NITCH_OK);
        check("hash_to_g1 size 96", n1 == NITCH_G1_BYTES);
        check_rc("hash_to_g1 deterministic",
                 nitch_hash_to_g1((const uint8_t*)msg, 3, h1b, sizeof(h1b), &n2), NITCH_OK);
        check("hash_to_g1 size 96 (2nd)", n2 == NITCH_G1_BYTES);
        check("hash_to_g1 reproducible", memcmp(h1a, h1b, NITCH_G1_BYTES) == 0);

        uint8_t h2[NITCH_SCALAR_BYTES];
        uint32_t nh2 = 0;
        check_rc("hash_to_scalar", nitch_hash_to_scalar((const uint8_t*)msg, 3, h2, sizeof(h2), &nh2), NITCH_OK);
        check("hash_to_scalar size 32", nh2 == NITCH_SCALAR_BYTES);

        /* commutativity: a+b == b+a  (no neg/sub in the API, so identity is
         * not directly reachable; commutativity + the *2/+ relation below
         * still cross-check add and mul against each other) */
        uint8_t h1c[NITCH_G1_BYTES];
        uint32_t nc1 = 0;
        check_rc("hash_to_g1(other msg)",
                 nitch_hash_to_g1((const uint8_t*)"new", 3, h1c, sizeof(h1c), &nc1), NITCH_OK);
        uint8_t ab[NITCH_G1_BYTES], ba[NITCH_G1_BYTES];
        uint32_t dummy = 0;
        check_rc("g1_add(a,b)", nitch_g1_add(h1a, h1c, ab, sizeof(ab), &dummy), NITCH_OK);
        check_rc("g1_add(b,a)", nitch_g1_add(h1c, h1a, ba, sizeof(ba), &dummy), NITCH_OK);
        check("g1_add commutative", memcmp(ab, ba, NITCH_G1_BYTES) == 0);

        /* g1 * 1 == g1, g1 * 2 == g1 + g1 */
        uint8_t one[NITCH_SCALAR_BYTES] = {0};
        one[31] = 0x01;
        uint8_t two[NITCH_SCALAR_BYTES] = {0};
        two[31] = 0x02;
        uint8_t mul1[NITCH_G1_BYTES], mul2[NITCH_G1_BYTES], add2[NITCH_G1_BYTES];
        check_rc("g1_mul_scalar(a,1)", nitch_g1_mul_scalar(h1a, one, mul1, sizeof(mul1), &dummy), NITCH_OK);
        check("g1*1 == g1", memcmp(mul1, h1a, NITCH_G1_BYTES) == 0);
        check_rc("g1_mul_scalar(a,2)", nitch_g1_mul_scalar(h1a, two, mul2, sizeof(mul2), &dummy), NITCH_OK);
        check_rc("g1_add(a,a) sum", nitch_g1_add(h1a, h1a, add2, sizeof(add2), &dummy), NITCH_OK);
        check("g1*2 == g1+g1", memcmp(mul2, add2, NITCH_G1_BYTES) == 0);
        hex("h1(old)", h1a, n1);

        /* degenerate e(g1,g2) bilinearity smoke: e(g1^2, g2) == e(g1, g2)^2 is
         * not directly comparable without GT mul in the C API, so just check
         * the two pairings of the same inputs agree (deterministic). */
        uint8_t g1gen[NITCH_G1_BYTES];
        uint8_t g2_from_g1[NITCH_G2_BYTES];
        uint32_t ng1 = 0;
        /* derive a generator-kilic G1: H1 of empty string is a valid point;
         * use hash_to_g1 output as one pairing input, and build a G2 from the
         * distkg deal below instead. Pairing itself is exercised in §1 below
         * implicitly via distkg_verify_share. Here just exercise the call. */
        (void)g2_from_g1;
        check_rc("hash_to_g1 empty", nitch_hash_to_g1((const uint8_t*)"", 0, g1gen, sizeof(g1gen), &ng1), NITCH_OK);
    }

    /* ---------------- DistKG (§1): create(4,2) -> verify -> extract -------- */
    uint8_t a[3][3 * NITCH_G1_BYTES];   /* 3 dealers x (t+1)=3 commitments */
    uint8_t b0[3][NITCH_G2_BYTES];
    uint8_t sh[3][4 * NITCH_SCALAR_BYTES];
    uint32_t la = 0, lb = 0, ls = 0;
    char p_hex[128];
    uint32_t p_hex_len = 0;

    for (int dealer = 1; dealer <= 3; dealer++) {
        int rc = nitch_distkg_create_deal(dealer, 2, 4,
                                          a[dealer - 1], sizeof(a[0]), &la,
                                          b0[dealer - 1], sizeof(b0[0]), &lb,
                                          sh[dealer - 1], sizeof(sh[0]), &ls,
                                          p_hex, sizeof(p_hex), &p_hex_len);
        char name[64];
        snprintf(name, sizeof(name), "distkg_create_deal(dealer=%d)", dealer);
        check_rc(name, rc, NITCH_OK);
    }
    check("distkg A size = (t+1)*96", la == 3 * NITCH_G1_BYTES);
    check("distkg b0 size = 192", lb == NITCH_G2_BYTES);
    check("distkg shares size = n*32", ls == 4 * NITCH_SCALAR_BYTES);
    check("distkg p_hex non-empty", p_hex_len > 0);

    /* verify every share of every dealer */
    for (int dealer = 0; dealer < 3; dealer++) {
        for (int j = 1; j <= 4; j++) {
            int valid = -1;
            int rc = nitch_distkg_verify_share(2, 4, j,
                                               a[dealer], 3 * NITCH_G1_BYTES,
                                               b0[dealer],
                                               sh[dealer] + (j - 1) * NITCH_SCALAR_BYTES,
                                               &valid);
            char name[64];
            snprintf(name, sizeof(name), "distkg_verify_share(d%d,j%d)", dealer + 1, j);
            check_rc(name, rc, NITCH_OK);
            snprintf(name, sizeof(name), "distkg_verify_share(d%d,j%d) valid", dealer + 1, j);
            check(name, valid == 1);
        }
    }

    /* tamper: flip one byte of the share for party 1 of dealer 1 */
    {
        uint8_t bad[4 * NITCH_SCALAR_BYTES];
        memcpy(bad, sh[0], sizeof(bad));
        bad[5] ^= 0x40;
        int valid = -1;
        int rc = nitch_distkg_verify_share(2, 4, 1, a[0], 3 * NITCH_G1_BYTES, b0[0], bad, &valid);
        check_rc("distkg_verify_share(tampered) rc", rc, NITCH_OK);
        check("distkg_verify_share(tampered) valid=0", valid == 0);
    }

    /* extract: QUAL = {1,2,3}, party 2 receives share j=2 from each dealer */
    {
        int32_t qual[3] = {1, 2, 3};
        uint8_t q_b0[3 * NITCH_G2_BYTES];
        uint8_t q_a0[3 * NITCH_G1_BYTES];
        uint8_t q_sh[3 * NITCH_SCALAR_BYTES];
        for (int q = 0; q < 3; q++) {
            memcpy(q_b0 + q * NITCH_G2_BYTES, b0[q], NITCH_G2_BYTES);
            memcpy(q_a0 + q * NITCH_G1_BYTES, a[q], NITCH_G1_BYTES); /* A_0 chunk */
            memcpy(q_sh + q * NITCH_SCALAR_BYTES, sh[q] + 1 * NITCH_SCALAR_BYTES, NITCH_SCALAR_BYTES);
        }
        uint8_t pk[NITCH_G2_BYTES], pkg1[NITCH_G1_BYTES], vk[NITCH_G1_BYTES], sk[NITCH_SCALAR_BYTES];
        uint32_t npk = 0, npkg1 = 0, nvk = 0, nsk = 0;
        int rc = nitch_distkg_extract(2, 2, 4, qual, 3, q_b0, q_a0, q_sh,
                                      pk, sizeof(pk), &npk,
                                      pkg1, sizeof(pkg1), &npkg1,
                                      vk, sizeof(vk), &nvk,
                                      sk, sizeof(sk), &nsk);
        check_rc("distkg_extract", rc, NITCH_OK);
        check("distkg_extract pk 192", npk == NITCH_G2_BYTES);
        check("distkg_extract pkg1 96", npkg1 == NITCH_G1_BYTES);
        check("distkg_extract vk 96", nvk == NITCH_G1_BYTES);
        check("distkg_extract sk 32", nsk == NITCH_SCALAR_BYTES);

        /* vk == g1^sk: multiply H1("") (any fixed G1) is NOT g1; use pairing
         * bilinearity instead: e(vk, g2_deal_b0sum) vs ... simplest available
         * check: nitch_g1_mul_scalar needs a g1 point. Derive g1 generator by
         * hashing is wrong. Instead verify via pairing with the aggregated pk:
         * e(vk, g2_any) is not comparable without a known g2 generator input.
         *
         * Use the relation vk = g1^sk and pk = g2^(sum coeffs): not directly
         * checkable either without generators exported. The pragmatic check is
         * the DKRS round-trip below (which recomputes sk from shares) plus
         * partial_adapt/verify, which cryptographically binds sk and vk. */

        /* buffer check: too-small out */
        uint32_t need = 0;
        uint8_t tiny[8];
        int rc2 = nitch_distkg_extract(2, 2, 4, qual, 3, q_b0, q_a0, q_sh,
                                       tiny, sizeof(tiny), &need,
                                       pkg1, sizeof(pkg1), &npkg1,
                                       vk, sizeof(vk), &nvk,
                                       sk, sizeof(sk), &nsk);
        check_rc("distkg_extract tiny buffer rc", rc2, NITCH_ERR_BUFFER);
        check("distkg_extract tiny buffer need", need == NITCH_G2_BYTES);

        /* param checks */
        int32_t badqual[1] = {9};
        check_rc("distkg_extract bad qual",
                 nitch_distkg_extract(2, 2, 4, badqual, 1, q_b0, q_a0, q_sh,
                                      pk, sizeof(pk), &npk, pkg1, sizeof(pkg1), &npkg1,
                                      vk, sizeof(vk), &nvk, sk, sizeof(sk), &nsk),
                 NITCH_ERR_PARAM);
        check_rc("distkg_create_deal t>=n",
                 nitch_distkg_create_deal(1, 4, 4, a[0], sizeof(a[0]), &la,
                                          b0[0], sizeof(b0[0]), &lb,
                                          sh[0], sizeof(sh[0]), &ls,
                                          p_hex, sizeof(p_hex), &p_hex_len),
                 NITCH_ERR_PARAM);
        check_rc("distkg_create_deal n not pow2",
                 nitch_distkg_create_deal(1, 2, 5, a[0], sizeof(a[0]), &la,
                                          b0[0], sizeof(b0[0]), &lb,
                                          sh[0], sizeof(sh[0]), &ls,
                                          p_hex, sizeof(p_hex), &p_hex_len),
                 NITCH_ERR_PARAM);
    }

    /* ---------------- DKRS (§2): reshare 4 -> 4 ---------------- */
    uint8_t ra[3][3 * NITCH_G1_BYTES];
    uint8_t rb0[3][NITCH_G2_BYTES];
    uint8_t rsh[3][4 * NITCH_SCALAR_BYTES];
    uint32_t rla = 0, rlb = 0, rls = 0;

    /* each old dealer resharing its party-2 share (extracted sk above is the
     * aggregate; for the test each dealer uses its own share value) */
    for (int d = 0; d < 3; d++) {
        char name[64];
        snprintf(name, sizeof(name), "dkrs_create_deal(dealer=%d)", d + 1);
        check_rc(name,
                 nitch_dkrs_create_deal(d + 1, 2, 4,
                                        sh[d] + 1 * NITCH_SCALAR_BYTES,
                                        ra[d], sizeof(ra[0]), &rla,
                                        rb0[d], sizeof(rb0[0]), &rlb,
                                        rsh[d], sizeof(rsh[0]), &rls),
                 NITCH_OK);
    }
    check("dkrs A size", rla == 3 * NITCH_G1_BYTES);
    check("dkrs b0 size", rlb == NITCH_G2_BYTES);
    check("dkrs shares size", rls == 4 * NITCH_SCALAR_BYTES);

    for (int d = 0; d < 3; d++) {
        for (int j = 1; j <= 4; j++) {
            int valid = -1;
            int rc = nitch_dkrs_verify_share(2, 4, j, ra[d], 3 * NITCH_G1_BYTES,
                                             rb0[d], rsh[d] + (j - 1) * NITCH_SCALAR_BYTES, &valid);
            char name[64];
            snprintf(name, sizeof(name), "dkrs_verify_share(d%d,j%d)", d + 1, j);
            check_rc(name, rc, NITCH_OK);
            snprintf(name, sizeof(name), "dkrs_verify_share(d%d,j%d) valid", d + 1, j);
            check(name, valid == 1);
        }
    }
    {
        int valid = -1;
        check_rc("dkrs_verify_share(wrong b0) rc",
                 nitch_dkrs_verify_share(2, 4, 2, ra[0], 3 * NITCH_G1_BYTES,
                                         rb0[1], rsh[0] + NITCH_SCALAR_BYTES, &valid),
                 NITCH_OK);
        check("dkrs_verify_share(wrong b0) valid=0", valid == 0);
    }
    {
        /* flip a byte inside party 2's share (offset 32..63), then verify j=2 */
        uint8_t bad[4 * NITCH_SCALAR_BYTES];
        memcpy(bad, rsh[0], sizeof(bad));
        bad[1 * NITCH_SCALAR_BYTES + 9] ^= 0x01;
        int valid = -1;
        check_rc("dkrs_verify_share(tampered) rc",
                 nitch_dkrs_verify_share(2, 4, 2, ra[0], 3 * NITCH_G1_BYTES,
                                         rb0[0], bad + 1 * NITCH_SCALAR_BYTES, &valid),
                 NITCH_OK);
        check("dkrs_verify_share(tampered) valid=0", valid == 0);
    }
    {
        int32_t qual_old[3] = {1, 2, 3};
        uint8_t q_sh[3 * NITCH_SCALAR_BYTES];
        uint8_t q_b0[3 * NITCH_G2_BYTES];
        uint8_t q_a0[3 * NITCH_G1_BYTES];
        for (int q = 0; q < 3; q++) {
            memcpy(q_sh + q * NITCH_SCALAR_BYTES, rsh[q] + 1 * NITCH_SCALAR_BYTES, NITCH_SCALAR_BYTES);
            memcpy(q_b0 + q * NITCH_G2_BYTES, rb0[q], NITCH_G2_BYTES);
            memcpy(q_a0 + q * NITCH_G1_BYTES, ra[q], NITCH_G1_BYTES);
        }
        uint8_t nsk[NITCH_SCALAR_BYTES], nvk[NITCH_G1_BYTES];
        uint32_t nnsk = 0, nnvk = 0;
        check_rc("dkrs_extract",
                 nitch_dkrs_extract(2, 2, 4, 4, qual_old, 3, q_b0, q_a0, q_sh,
                                    nsk, sizeof(nsk), &nnsk, nvk, sizeof(nvk), &nnvk),
                 NITCH_OK);
        check("dkrs_extract sk 32", nnsk == NITCH_SCALAR_BYTES);
        check("dkrs_extract vk 96", nnvk == NITCH_G1_BYTES);

        /* buffer check */
        uint32_t need = 0;
        uint8_t tiny[4];
        check_rc("dkrs_extract tiny sk buffer",
                 nitch_dkrs_extract(2, 2, 4, 4, qual_old, 3, q_b0, q_a0, q_sh,
                                    tiny, sizeof(tiny), &need, nvk, sizeof(nvk), &nnvk),
                 NITCH_ERR_BUFFER);
        check("dkrs_extract need", need == NITCH_SCALAR_BYTES);

        /* duplicated qual -> crypto failure (lagrange denominators) */
        int32_t dup[3] = {1, 1, 2};
        check_rc("dkrs_extract duplicate qual",
                 nitch_dkrs_extract(2, 2, 4, 4, dup, 3, q_b0, q_a0, q_sh,
                                    nsk, sizeof(nsk), &nnsk, nvk, sizeof(nvk), &nnvk),
                 NITCH_ERR_CRYPTO);
    }

    /* ---------------- vk == g1^sk identity check ----------------
     * With threshold t=0 the polynomial is the constant c0, so every share
     * equals c0 and extract gives sk=c0, vk=g1^c0. A[0] is g1^c0 too, so the
     * check vk == A[0] IS the vk==g1^sk relation, using only wire bytes. */
    {
        uint8_t a0d[NITCH_G1_BYTES];          /* (t+1)=1 commitment */
        uint8_t b0d[NITCH_G2_BYTES];
        uint8_t shd[4 * NITCH_SCALAR_BYTES];
        uint32_t u = 0, v = 0, w = 0;
        check_rc("vk-check distkg_create_deal(1,0,4)",
                 nitch_distkg_create_deal(1, 0, 4, a0d, sizeof(a0d), &u,
                                          b0d, sizeof(b0d), &v, shd, sizeof(shd), &w,
                                          NULL, 0, NULL),
                 NITCH_OK);
        check("vk-check A size = 96", u == NITCH_G1_BYTES);

        int32_t qual1[1] = {1};
        uint8_t pk0[NITCH_G2_BYTES], pkg10[NITCH_G1_BYTES], vk0[NITCH_G1_BYTES], sk0[NITCH_SCALAR_BYTES];
        uint32_t q = 0, r = 0, s = 0, t = 0;
        check_rc("vk-check distkg_extract",
                 nitch_distkg_extract(3, 0, 4, qual1, 1, b0d, a0d,
                                      shd + 2 * NITCH_SCALAR_BYTES, /* any party's share */
                                      pk0, sizeof(pk0), &q, pkg10, sizeof(pkg10), &r,
                                      vk0, sizeof(vk0), &s, sk0, sizeof(sk0), &t),
                 NITCH_OK);
        check("vk == g1^sk (t=0: vk == A[0])", memcmp(vk0, a0d, NITCH_G1_BYTES) == 0);
        check("sk == share (t=0: share == c0)",
              memcmp(sk0, shd + 2 * NITCH_SCALAR_BYTES, NITCH_SCALAR_BYTES) == 0);
        check("pk == b0 (single dealer, QUAL={1})", memcmp(pk0, b0d, NITCH_G2_BYTES) == 0);
        check("pkG1 == A0 (single dealer, QUAL={1})", memcmp(pkg10, a0d, NITCH_G1_BYTES) == 0);

        /* g1_mul_scalar cross-check: vk == A[0]^1 via the exported primitive */
        uint8_t via_mul[NITCH_G1_BYTES];
        uint8_t one[32] = {0};
        one[31] = 0x01;
        uint32_t dummy2 = 0;
        check_rc("vk-check g1_mul_scalar(A0,1)",
                 nitch_g1_mul_scalar(a0d, one, via_mul, sizeof(via_mul), &dummy2), NITCH_OK);
        check("g1_mul_scalar(A0,1) == vk", memcmp(via_mul, vk0, NITCH_G1_BYTES) == 0);
    }

    /* ---------------- Adaptation (§3) ---------------- */
    {
        /* Reconstruct a real (sk, vk) pair via a fresh single-dealer DKG so the
         * NIZK binds a matching key: deal from dealer 1, party 2's share. */
        uint8_t a2[2 * NITCH_G1_BYTES];  /* (4,1): 2 commitments */
        uint8_t b02[NITCH_G2_BYTES];
        uint8_t sh2[4 * NITCH_SCALAR_BYTES];
        uint32_t x = 0, y = 0, z = 0;
        check_rc("adapt setup distkg_create_deal(1,1,4)",
                 nitch_distkg_create_deal(1, 1, 4, a2, sizeof(a2), &x,
                                          b02, sizeof(b02), &y, sh2, sizeof(sh2), &z,
                                          NULL, 0, NULL),
                 NITCH_OK);
        int valid = -1;
        check_rc("adapt setup verify",
                 nitch_distkg_verify_share(1, 4, 2, a2, 2 * NITCH_G1_BYTES, b02,
                                           sh2 + 1 * NITCH_SCALAR_BYTES, &valid),
                 NITCH_OK);
        check("adapt setup verify valid", valid == 1);

        const uint8_t* sk = sh2 + 1 * NITCH_SCALAR_BYTES;
        /* vk = g1^sk is produced by extract; with QUAL={1} plain sum = the
         * share itself, so extract(QUAL={1}) yields exactly vk=g1^sk. */
        int32_t qual1[1] = {1};
        uint8_t vk[NITCH_G1_BYTES];
        uint32_t nvk = 0;
        uint8_t tmp_pk[NITCH_G2_BYTES], tmp_pkg1[NITCH_G1_BYTES], tmp_sk[NITCH_SCALAR_BYTES];
        uint32_t t1 = 0, t2 = 0, t3 = 0;
        check_rc("adapt vk via distkg_extract(QUAL={1})",
                 nitch_distkg_extract(2, 1, 4, qual1, 1, b02, a2, sh2 + 1 * NITCH_SCALAR_BYTES,
                                      tmp_pk, sizeof(tmp_pk), &t1,
                                      tmp_pkg1, sizeof(tmp_pkg1), &t2,
                                      vk, sizeof(vk), &nvk,
                                      tmp_sk, sizeof(tmp_sk), &t3),
                 NITCH_OK);
        /* sk from extract (QUAL={1}) must equal the share */
        check("adapt sk round-trip", memcmp(tmp_sk, sk, NITCH_SCALAR_BYTES) == 0);

        const uint8_t* src = (const uint8_t*)"old";
        const uint8_t* tgt = (const uint8_t*)"new";
        uint8_t contrib[NITCH_G1_BYTES], chal[NITCH_SCALAR_BYTES], resp[NITCH_SCALAR_BYTES];
        uint32_t nc = 0, nch = 0, nr = 0;
        check_rc("partial_adapt",
                 nitch_partial_adapt(sk, vk, 2, src, 3, tgt, 3,
                                     contrib, sizeof(contrib), &nc,
                                     chal, sizeof(chal), &nch,
                                     resp, sizeof(resp), &nr),
                 NITCH_OK);
        check("partial_adapt contribution 96", nc == NITCH_G1_BYTES);
        check("partial_adapt challenge 32", nch == NITCH_SCALAR_BYTES);
        check("partial_adapt response 32", nr == NITCH_SCALAR_BYTES);

        int pvalid = -1;
        check_rc("partial_verify",
                 nitch_partial_verify(vk, src, 3, tgt, 3, contrib, chal, resp, &pvalid),
                 NITCH_OK);
        check("partial_verify valid", pvalid == 1);

        /* tamper the challenge */
        uint8_t bad_chal[NITCH_SCALAR_BYTES];
        memcpy(bad_chal, chal, sizeof(bad_chal));
        bad_chal[0] ^= 0x01;
        pvalid = -1;
        check_rc("partial_verify(tampered challenge) rc",
                 nitch_partial_verify(vk, src, 3, tgt, 3, contrib, bad_chal, resp, &pvalid),
                 NITCH_OK);
        check("partial_verify(tampered challenge) valid=0", pvalid == 0);

        /* tamper the response */
        uint8_t bad_resp[NITCH_SCALAR_BYTES];
        memcpy(bad_resp, resp, sizeof(bad_resp));
        bad_resp[31] ^= 0x80;
        pvalid = -1;
        check_rc("partial_verify(tampered response) rc",
                 nitch_partial_verify(vk, src, 3, tgt, 3, contrib, chal, bad_resp, &pvalid),
                 NITCH_OK);
        check("partial_verify(tampered response) valid=0", pvalid == 0);

        /* tamper the contribution (point) */
        uint8_t bad_contrib[NITCH_G1_BYTES];
        memcpy(bad_contrib, contrib, sizeof(bad_contrib));
        bad_contrib[10] ^= 0x02;
        pvalid = -1;
        check_rc("partial_verify(tampered contribution) rc",
                 nitch_partial_verify(vk, src, 3, tgt, 3, bad_contrib, chal, resp, &pvalid),
                 NITCH_OK);
        check("partial_verify(tampered contribution) valid=0", pvalid == 0);

        /* cross-key: a proof made with a different sk must fail */
        uint8_t contrib2[NITCH_G1_BYTES], chal2[NITCH_SCALAR_BYTES], resp2[NITCH_SCALAR_BYTES];
        uint32_t c2 = 0, h2_ = 0, r2_ = 0;
        uint8_t sk2[NITCH_SCALAR_BYTES];
        memcpy(sk2, sk, sizeof(sk2));
        sk2[0] ^= 0x01; /* different scalar */
        check_rc("partial_adapt(other sk)",
                 nitch_partial_adapt(sk2, vk, 2, src, 3, tgt, 3,
                                     contrib2, sizeof(contrib2), &c2,
                                     chal2, sizeof(chal2), &h2_,
                                     resp2, sizeof(resp2), &r2_),
                 NITCH_OK);
        pvalid = -1;
        check_rc("partial_verify(other sk proof) rc",
                 nitch_partial_verify(vk, src, 3, tgt, 3, contrib2, chal2, resp2, &pvalid),
                 NITCH_OK);
        check("partial_verify(other sk proof) valid=0", pvalid == 0);

        /* cross-message: verify against different messages must fail */
        pvalid = -1;
        check_rc("partial_verify(wrong target msg) rc",
                 nitch_partial_verify(vk, src, 3, (const uint8_t*)"neW", 3, contrib, chal, resp, &pvalid),
                 NITCH_OK);
        check("partial_verify(wrong target msg) valid=0", pvalid == 0);

        /* too-small contribution buffer */
        uint32_t need = 0;
        uint8_t tiny[16];
        check_rc("partial_adapt tiny contribution",
                 nitch_partial_adapt(sk, vk, 2, src, 3, tgt, 3,
                                     tiny, sizeof(tiny), &need,
                                     chal, sizeof(chal), &nch,
                                     resp, sizeof(resp), &nr),
                 NITCH_ERR_BUFFER);
        check("partial_adapt need", need == NITCH_G1_BYTES);
    }

    /* ---------------- pairing primitive (§4) ---------------- */
    {
        /* bilinearity: e(g1^a, g2) == e(g1, g2)^a cannot be expressed without
         * GT ops in the API; check determinism + size + bilinear trick via
         * two scalar multiples: e(a*P, Q) vs e(P, a*Q) requires a G2 mul, not
         * exported. So: same inputs -> same 576B output. */
        uint8_t p1[NITCH_G1_BYTES], p2[NITCH_G2_BYTES];
        uint32_t n1 = 0;
        nitch_hash_to_g1((const uint8_t*)"pairing-g1", 10, p1, sizeof(p1), &n1);
        /* build a G2 from a distkg b0 produced above */
        memcpy(p2, b0[0], NITCH_G2_BYTES);
        uint8_t gt1[NITCH_GT_BYTES], gt2[NITCH_GT_BYTES];
        uint32_t g1n = 0, g2n = 0;
        check_rc("pairing #1", nitch_pairing(p1, p2, gt1, sizeof(gt1), &g1n), NITCH_OK);
        check("pairing size 576", g1n == NITCH_GT_BYTES);
        check_rc("pairing #2", nitch_pairing(p1, p2, gt2, sizeof(gt2), &g2n), NITCH_OK);
        check("pairing deterministic", memcmp(gt1, gt2, NITCH_GT_BYTES) == 0);

        /* non-degenerate: not all zero bytes */
        int nonzero = 0;
        for (size_t i = 0; i < NITCH_GT_BYTES; i++) if (gt1[i]) { nonzero = 1; break; }
        check("pairing non-degenerate", nonzero);
    }

    printf("\n%d checks, %d failures\n", checks, failures);
    return failures == 0 ? 0 : 1;
}
