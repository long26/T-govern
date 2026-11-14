#include "Enclave_t.h"
#include "sgx_tcrypto.h"
#include <string.h>
#include <vector>
#include <stdio.h>
#include <map>
#include <unordered_set>
#include <stdint.h>

#include "sgx_trts.h"
#include "sgx_tseal.h"
#include "stdio.h"
#include "string.h"
#include "stdlib.h"
#include "stdarg.h"

#include "sgx_error.h"
#include "sgx_report.h"
#include "sgx_utils.h"

#include "sgx_thread.h"

#include "sgx_dcap_ql_wrapper.h"
#include "sgx_quote_3.h"
#include "sgx_pce.h"
#include "sgx_ql_quote.h"
#include "sgx_dcap_quoteverify.h"

#include "nitch/adapt.hpp"
#include "nitch/distkg.hpp"
#include "nitch/setup.hpp"
#include "nitch/types.hpp"
#include "nitch/serialization.hpp"

#include "bls_BLS12381.h"
#include "randapi.h"

#include "secp256k1.h"
#include "secp256k1_recovery.h"

using namespace BLS12381;
using namespace BLS12381_BIG;
using namespace core;

typedef struct {
    uint8_t sk[32];          // secp256k1 private key, big-endian
    uint8_t pk65[65];        // 0x04 || X || Y
    uint8_t pk_x[32];
    uint8_t pk_y[32];
    uint8_t eth_addr[20];    // keccak256(X || Y)[12:]
    uint8_t addr32[32];      // leftPad32(eth_addr), 可作为 vrfPubKey 风格字段
} tee_secp256k1_pair_t;

// 委员会聚合公钥 g_vk_gc，通过 ecall_set_committee_pubkey 设置
static uint8_t g_vk_gc[128] = {0};
static bool g_vk_gc_set = false;

static sgx_ecc_state_handle_t g_ecc_handle = nullptr;
static bool g_enclave_key_generated = false;
static sgx_ec256_private_t g_enclave_sk = {0};
static sgx_ec256_public_t g_enclave_pk = {0};
static std::map<int, sgx_aes_gcm_128bit_key_t> g_peer_shared_keys;

static bool g_sig_key_generated = false;
tee_secp256k1_pair_t g_sig_key;
tee_secp256k1_pair_t g_vrf_key;

// Table 6.2 instrumentation.  These counters are deliberately kept inside
// the enclave: the host measures ECALL wall-clock time, while this data
// exposes only aggregate CPU-cycle counts for the algorithmic region.
// The counters are intended for a single benchmark client; the spin lock
// keeps diagnostic reads well-defined when the production enclave is shared.
enum tgovern_bench_operation_t : uint32_t {
    TGO_BENCH_DKG_LOCAL = 0,
    TGO_BENCH_AUTH_CERT_VERIFY,
    TGO_BENCH_PCOLLIDE,
    TGO_BENCH_TEE_SIGN,
    TGO_BENCH_VRF_KEYGEN,
    TGO_BENCH_VRF_COMP,
    TGO_BENCH_SHARE_ERASE,
    TGO_BENCH_OPERATION_COUNT,
};

struct tgovern_bench_stat_t {
    uint64_t calls;
    uint64_t total_cycles;
    uint64_t min_cycles;
    uint64_t max_cycles;
};

static tgovern_bench_stat_t g_tgovern_bench_stats[TGO_BENCH_OPERATION_COUNT] = {};
static sgx_spinlock_t g_tgovern_bench_lock = SGX_SPINLOCK_INITIALIZER;

// RDTSC is used only for repeatable relative measurement.  It is not a
// trusted clock and must not be used by protocol logic or security checks.
static uint64_t tgovern_bench_rdtsc() {
    uint32_t low = 0;
    uint32_t high = 0;
    asm volatile("lfence\n\trdtsc" : "=a"(low), "=d"(high) :: "memory");
    return (static_cast<uint64_t>(high) << 32) | low;
}

static void tgovern_bench_record(tgovern_bench_operation_t operation, uint64_t start_cycles) {
    const uint64_t elapsed = tgovern_bench_rdtsc() - start_cycles;
    sgx_spin_lock(&g_tgovern_bench_lock);
    tgovern_bench_stat_t& stat = g_tgovern_bench_stats[operation];
    stat.calls++;
    stat.total_cycles += elapsed;
    if (stat.min_cycles == 0 || elapsed < stat.min_cycles) stat.min_cycles = elapsed;
    if (elapsed > stat.max_cycles) stat.max_cycles = elapsed;
    sgx_spin_unlock(&g_tgovern_bench_lock);
}

class tgovern_bench_scope_t {
public:
    explicit tgovern_bench_scope_t(tgovern_bench_operation_t operation, bool enabled = true)
        : operation_(operation), start_cycles_(enabled ? tgovern_bench_rdtsc() : 0), active_(enabled) {}
    ~tgovern_bench_scope_t() { stop(); }
    void stop() {
        if (active_) {
            tgovern_bench_record(operation_, start_cycles_);
            active_ = false;
        }
    }
private:
    tgovern_bench_operation_t operation_;
    uint64_t start_cycles_;
    bool active_;
};

uint8_t hardware_id[32];
static bool g_hardware_id_seeded = false;

// 反转32字节数组字节序（大端↔小端）
static void reverse32(const uint8_t* src, uint8_t* dst) {
    for (int i = 0; i < 32; i++) {
        dst[i] = src[31 - i];
    }
}

// 1.1.1 初始化Enclave密钥对
void ensure_enclave_key() {
    if (g_enclave_key_generated) {
        return;
    }

    sgx_status_t ret = sgx_ecc256_open_context(&g_ecc_handle);
    if (ret != SGX_SUCCESS) {
        return;
    }
    ret = sgx_ecc256_create_key_pair(&g_enclave_sk, &g_enclave_pk, g_ecc_handle);
    if (ret == SGX_SUCCESS) {
        g_enclave_key_generated = true;
        // [DKRS-DBG] dump the freshly generated keypair (FULL bytes) once, to check whether
        // g_enclave_sk/g_enclave_pk are per-node distinct and a matching pair.
        {
            uint8_t pk_be[64];
            for (int i = 0; i < 32; i++) pk_be[i]        = g_enclave_pk.gx[31 - i];
            for (int i = 0; i < 32; i++) pk_be[32 + i]   = g_enclave_pk.gy[31 - i];
            char buf[300];
            int n = snprintf(buf, sizeof(buf), "[DKRS-DBG] KEYGEN sk=");
            for (int i = 0; i < 8 && n < (int)sizeof(buf) - 4; i++)   n += snprintf(buf + n, sizeof(buf) - n, "%02x", g_enclave_sk.r[i]);
            n += snprintf(buf + n, sizeof(buf) - n, " pk=");
            for (int i = 0; i < 64 && n < (int)sizeof(buf) - 4; i++)  n += snprintf(buf + n, sizeof(buf) - n, "%02x", pk_be[i]);
            ocall_print_string(buf);
        }
    }
}

void ensure_hardware_id() {
    if (g_hardware_id_seeded || hardware_id[0] != 0) {
        return;
    }
    sgx_key_request_t key_request;
    memset(&key_request, 0, sizeof(key_request));
    key_request.key_name = SGX_KEYSELECT_SEAL;
    key_request.key_policy = SGX_KEYPOLICY_MRENCLAVE;

    sgx_key_128bit_t seal_key;
    memset(&seal_key, 0, sizeof(seal_key));
    sgx_status_t ret = sgx_get_key(&key_request, &seal_key);
    if (ret != SGX_SUCCESS) {
        return;
    }
    const uint8_t label[] = "hardware_id";
    ret = sgx_hmac_sha256_msg(label, sizeof(label) - 1, seal_key, sizeof(seal_key), hardware_id, 32);
}

sgx_status_t ecall_set_identity_seed(const uint8_t* seed, uint32_t seed_len) {
    if (!seed || seed_len == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    const uint8_t label[] = "sgx-sim-hardware-id:";
    std::vector<uint8_t> material(sizeof(label) - 1 + seed_len);
    memcpy(material.data(), label, sizeof(label) - 1);
    memcpy(material.data() + sizeof(label) - 1, seed, seed_len);
    sgx_status_t ret = sgx_sha256_msg(material.data(), static_cast<uint32_t>(material.size()), reinterpret_cast<sgx_sha256_hash_t*>(hardware_id));
    if (ret != SGX_SUCCESS) {
        memset(hardware_id, 0, sizeof(hardware_id));
        g_hardware_id_seeded = false;
        return ret;
    }
    g_hardware_id_seeded = true;
    return SGX_SUCCESS;
}

// 1.1.2 拿enclave公钥用于广播
sgx_status_t ecall_get_enclave_pk(uint8_t* pk, uint32_t pk_buf_size, uint32_t* pk_out_size) {
    if (!pk || !pk_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (pk_buf_size < 64) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }
    // SGX内部存储为小端，外部接口返回大端（标准格式）
    reverse32(g_enclave_pk.gx, pk);
    reverse32(g_enclave_pk.gy, pk + 32);
    *pk_out_size = 64;
    return SGX_SUCCESS;
}

// ---- DKRS key-material diagnostic (temporary instrumentation; remove after debug) ----
// Prints idx + own published EPK prefix + (optional) peer EPK prefix + (optional) derived/looked-up AES key prefix.
// own/peer EPKs are in published (big-endian) form so they match the Go-side EPK logs.
static void dkrs_dbg_key(const char* tag, int idx,
                         const uint8_t* peer_pk_be,
                         const uint8_t* key16)
{
    ensure_enclave_key();
    uint8_t own_be[32];
    for (int i = 0; i < 16; i++) own_be[i]      = g_enclave_pk.gx[15 - i];
    for (int i = 0; i < 16; i++) own_be[16 + i] = g_enclave_pk.gy[15 - i];
    char buf[320];
    int n = snprintf(buf, sizeof(buf),
        "[DKRS-DBG] %s idx=%d own=%02x%02x%02x%02x%02x%02x%02x%02x",
        tag, idx,
        own_be[0],own_be[1],own_be[2],own_be[3],own_be[4],own_be[5],own_be[6],own_be[7]);
    if (n > 0 && n < (int)sizeof(buf)) {
        if (peer_pk_be) {
            n += snprintf(buf + n, sizeof(buf) - n,
                " peer=%02x%02x%02x%02x%02x%02x%02x%02x",
                peer_pk_be[0],peer_pk_be[1],peer_pk_be[2],peer_pk_be[3],
                peer_pk_be[4],peer_pk_be[5],peer_pk_be[6],peer_pk_be[7]);
        }
        if (key16) {
            n += snprintf(buf + n, sizeof(buf) - n,
                " key=%02x%02x%02x%02x%02x%02x%02x%02x",
                key16[0],key16[1],key16[2],key16[3],key16[4],key16[5],key16[6],key16[7]);
        }
    }
    ocall_print_string(buf);
}

static void sgx_diag_status(const char* op, const char* stage, sgx_status_t status,
                            uint32_t a = 0, uint32_t b = 0, uint32_t c = 0, int idx = 0)
{
    char buf[320];
    snprintf(buf, sizeof(buf),
        "[SGX-DIAG] op=%s stage=%s status=0x%x idx=%d a=%u b=%u c=%u",
        op ? op : "", stage ? stage : "", static_cast<unsigned int>(status), idx, a, b, c);
    ocall_print_string(buf);
}

static sgx_status_t sgx_diag_return(const char* op, const char* stage, sgx_status_t status,
                                    uint32_t a = 0, uint32_t b = 0, uint32_t c = 0, int idx = 0)
{
    sgx_diag_status(op, stage, status, a, b, c, idx);
    return status;
}

// 1.1.3 注册加密信道密钥
sgx_status_t ecall_register_peer_epk(int peer_index, const uint8_t* peer_pk, uint32_t peer_pk_size) {
    if (!peer_pk || peer_pk_size != 64) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (peer_index <= 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    
    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }
    
    // 1. 反序列化公钥（外部传入大端，SGX内部使用小端）
    sgx_ec256_public_t peer_public;
    reverse32(peer_pk, peer_public.gx);
    reverse32(peer_pk + 32, peer_public.gy);
    
    int valid = 0;
    sgx_status_t ret = sgx_ecc256_check_point(&peer_public, g_ecc_handle, &valid);
    if (ret != SGX_SUCCESS || !valid) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    
    // 2. 计算共享密钥
    sgx_ec256_dh_shared_t shared_point;
    ret = sgx_ecc256_compute_shared_dhkey(&g_enclave_sk, &peer_public, &shared_point, g_ecc_handle);
    if (ret != SGX_SUCCESS) {
        return ret;
    }

    // 3. 从共享密钥派生一个 AES-128-GCM 密钥
    sgx_aes_gcm_128bit_key_t aes_key;
    memset(aes_key, 0, sizeof(aes_key));

    const uint8_t salt[] = "sgx-ecdh-salt-v1";
    const uint8_t info[] = "sgx-ecdh-aes128-gcm-key-v1";

    uint8_t prk[32];
    uint8_t okm[32];

    ret = sgx_hmac_sha256_msg(
        shared_point.s,sizeof(shared_point.s),
        salt,sizeof(salt) - 1,
        prk,sizeof(prk)
    );
    if (ret != SGX_SUCCESS) {
        memset_s(&shared_point, sizeof(shared_point), 0, sizeof(shared_point));
        return ret;
    }

    uint8_t expand_input[sizeof(info)];
    memcpy(expand_input, info, sizeof(info) - 1);
    expand_input[sizeof(info) - 1] = 0x01;

    ret = sgx_hmac_sha256_msg(
        expand_input,sizeof(expand_input),
        prk,sizeof(prk),
        okm,sizeof(okm)
    );
    if (ret != SGX_SUCCESS) {
        memset_s(prk, sizeof(prk), 0, sizeof(prk));
        memset_s(&shared_point, sizeof(shared_point), 0, sizeof(shared_point));
        return ret;
    }

    // 只取前 16 字节，因为 sgx_aes_gcm_128bit_key_t 是 128-bit
    memcpy(aes_key, okm, SGX_AESGCM_KEY_SIZE);
    memcpy(g_peer_shared_keys[peer_index],aes_key,sizeof(sgx_aes_gcm_128bit_key_t));
    dkrs_dbg_key("REG", peer_index, peer_pk, aes_key);

    memset_s(prk, sizeof(prk), 0, sizeof(prk));
    memset_s(okm, sizeof(okm), 0, sizeof(okm));
    memset_s(aes_key, sizeof(aes_key), 0, sizeof(aes_key));
    memset_s(&shared_point, sizeof(shared_point), 0, sizeof(shared_point));    
    return SGX_SUCCESS;
}

static sgx_status_t encrypt_for_peer(
    int peer_index,
    const uint8_t* plaintext,
    uint32_t pt_len,
    uint8_t* ciphertext,
    uint32_t ct_buf_size,
    uint32_t* ct_out_size)
{
    if (g_peer_shared_keys.find(peer_index) == g_peer_shared_keys.end()) {
        return SGX_ERROR_UNEXPECTED;
    }
    
    const sgx_aes_gcm_128bit_key_t& key = g_peer_shared_keys[peer_index];
    
    uint32_t required_size = 12 + 16 + pt_len;
    if (ct_buf_size < required_size) {
        return sgx_diag_return("peer_encrypt", "ct_buffer", SGX_ERROR_OUT_OF_MEMORY,
            ct_buf_size, required_size, pt_len, peer_index);
    }
    
    uint8_t iv[12];
    sgx_status_t ret = sgx_read_rand(iv, 12);
    if (ret != SGX_SUCCESS) {
        sgx_diag_status("peer_encrypt", "sgx_read_rand", ret, 12, 0, 0, peer_index);
        return ret;
    }
    
    sgx_aes_gcm_128bit_tag_t tag;
    ret = sgx_rijndael128GCM_encrypt(&key, plaintext, pt_len, ciphertext + 28, iv, 12, nullptr, 0, &tag);
    if (ret != SGX_SUCCESS) {
        sgx_diag_status("peer_encrypt", "aes_gcm_encrypt", ret, ct_buf_size, required_size, pt_len, peer_index);
        memset_s(tag, sizeof(tag), 0, sizeof(tag));
        memset_s(iv, sizeof(iv), 0, sizeof(iv));
        return ret;
    }
    
    memcpy(ciphertext, iv, 12);
    memcpy(ciphertext + 12, tag, 16);
    
    *ct_out_size = required_size;
    return SGX_SUCCESS;
}

static sgx_status_t decrypt_from_peer(
    int peer_index,
    const uint8_t* ciphertext,
    uint32_t ct_len,
    uint8_t* plaintext,
    uint32_t pt_buf_size,
    uint32_t* pt_out_size)
{
    if (ct_len < 28) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (g_peer_shared_keys.find(peer_index) == g_peer_shared_keys.end()) {
        return SGX_ERROR_UNEXPECTED;
    }
    
    const sgx_aes_gcm_128bit_key_t& key = g_peer_shared_keys[peer_index];
    
    uint32_t pt_len = ct_len - 28;
    if (pt_buf_size < pt_len) {
        return sgx_diag_return("peer_decrypt", "pt_buffer", SGX_ERROR_OUT_OF_MEMORY,
            pt_buf_size, pt_len, ct_len, peer_index);
    }
    
    const uint8_t* iv = ciphertext;
    const uint8_t* mac = ciphertext + 12;
    const uint8_t* ct = ciphertext + 28;
    
    sgx_status_t ret = sgx_rijndael128GCM_decrypt(&key, ct, pt_len, plaintext, iv, 12, nullptr, 0, (const sgx_aes_gcm_128bit_tag_t*)mac);
    if (ret != SGX_SUCCESS) {
        sgx_diag_status("peer_decrypt", "aes_gcm_decrypt", ret, pt_buf_size, pt_len, ct_len, peer_index);
        dkrs_dbg_key("DEC-FAIL", peer_index, nullptr, (const uint8_t*)&key);
        return ret;
    }
    
    *pt_out_size = pt_len;
    return SGX_SUCCESS;
}

static void secure_bzero(void *p, size_t n) {
    volatile uint8_t *v = (volatile uint8_t *)p;
    while (n--) *v++ = 0;
}

// 前向声明: keccak256_hash 定义在后面，但 create_secp256k1_pair 需要使用
static void keccak256_hash(const uint8_t* data, size_t len, uint8_t* out);

// 使用 secp256k1 私钥对消息哈希进行可恢复签名
// 输出以太坊风格 65 字节签名: r(32) || s(32) || v(1), v = recovery_id + 27
// 返回签名长度(65) 或 0 表示失败
// Shared signing context, created once and reused so the per-call
// secp256k1_context_create/destroy stays out of the TEE_SIGN / VRF_COMP /
// SHARE_ERASE cycle scopes (pure-crypto timing). libsecp256k1 contexts are
// designed for create-once-reuse; the precomp table is deterministic (no RNG),
// so a reused context is byte-identical to a freshly created one.
static secp256k1_context* g_secp_sign_ctx = nullptr;
static secp256k1_context* get_secp_sign_context() {
    if (g_secp_sign_ctx == nullptr) {
        g_secp_sign_ctx = secp256k1_context_create(SECP256K1_CONTEXT_SIGN);
    }
    return g_secp_sign_ctx;
}

static int secp256k1_sign_recoverable_65(
    const uint8_t* msg_hash32,
    const uint8_t* seckey32,
    uint8_t* sig65_out
) {
    if (!msg_hash32 || !seckey32 || !sig65_out) {
        return 0;
    }

    secp256k1_context *ctx = get_secp_sign_context();
    if (!ctx) {
        return 0;
    }

    secp256k1_ecdsa_recoverable_signature sig;
    int ok = secp256k1_ecdsa_sign_recoverable(
        ctx, &sig, msg_hash32, seckey32, NULL, NULL);
    if (!ok) {
        return 0;
    }

    int recid = 0;
    ok = secp256k1_ecdsa_recoverable_signature_serialize_compact(
        ctx, sig65_out, &recid, &sig);
    if (!ok) {
        return 0;
    }

    // 以太坊风格: v = recovery_id + 27
    sig65_out[64] = (uint8_t)(recid + 27);
    return 65;
}

int create_secp256k1_pair(secp256k1_context *ctx, tee_secp256k1_pair_t *out) {
    if (ctx == NULL || out == NULL) {
        ocall_print_string("create_secp256k1_pair ctx or out is NULL");
        return 1;
    }

    memset(out, 0, sizeof(*out));

    /*
     * 1. 生成合法 secp256k1 私钥
     *
     * libsecp256k1 要求：
     * 1 <= sk < secp256k1n
     */
    do {
        if (sgx_read_rand(out->sk, sizeof(out->sk)) != SGX_SUCCESS) {
            ocall_print_string("sgx_read_rand failed");
            return 1;
        }
    } while (!secp256k1_ec_seckey_verify(ctx, out->sk));

    /*
     * 2. 由 sk 推导 pk
     */
    secp256k1_pubkey pubkey;

    if (!secp256k1_ec_pubkey_create(ctx, &pubkey, out->sk)) {
        secure_bzero(out->sk, sizeof(out->sk));
        ocall_print_string("secp256k1_ec_pubkey_create failed");
        return 1;
    }

    /*
     * 3. 序列化为非压缩公钥：0x04 || X || Y
     */
    size_t pk_len = sizeof(out->pk65);

    if (!secp256k1_ec_pubkey_serialize(
            ctx,
            out->pk65,
            &pk_len,
            &pubkey,
            SECP256K1_EC_UNCOMPRESSED
        )) {
        secure_bzero(out->sk, sizeof(out->sk));
        ocall_print_string("secp256k1_ec_pubkey_serialize pk_len failed");
        return 1;
    }

    if (pk_len != 65 || out->pk65[0] != 0x04) {
        secure_bzero(out->sk, sizeof(out->sk));
        ocall_print_string("secp256k1_ec_pubkey_serialize pk_len failed");
        return 1;
    }

    memcpy(out->pk_x, out->pk65 + 1, 32);
    memcpy(out->pk_y, out->pk65 + 33, 32);

    /*
     * 4. Ethereum 地址派生：
     *
     * eth_addr = last20Bytes(keccak256(pk_x || pk_y))
     *
     * 注意：不要把 0x04 前缀放进去 hash。
     */
    uint8_t hash32[32];

    keccak256_hash(out->pk65 + 1, 64, hash32);

    memcpy(out->eth_addr, hash32 + 12, 20);

    /*
     * 5. leftPad32(address)
     *
     * 对 VRF 来说，这就是你的 vrfPubKey 格式：
     * bytes32(uint256(uint160(vrfAddress)))
     */
    memset(out->addr32, 0, 12);
    memcpy(out->addr32 + 12, out->eth_addr, 20);

    secure_bzero(hash32, sizeof(hash32));
    return 0;
}

// 1.2 初始化签名和vrf密钥
int create_sig_and_vrf_key() {
    if (g_sig_key_generated) return 0;

    secp256k1_context *ctx = secp256k1_context_create(SECP256K1_CONTEXT_SIGN);
    if (ctx == NULL) return 1;

    if (create_secp256k1_pair(ctx, &g_sig_key) != 0) {
        secp256k1_context_destroy(ctx);
        ocall_print_string("create_secp256k1_pair sig_key failed");
        return 1;
    }

    {
        // VRF.KeyGen: scope excludes secp256k1_context creation (done outside,
        // via the shared ctx) to match the REE baseline's context-reuse timing.
        tgovern_bench_scope_t benchmark_scope(TGO_BENCH_VRF_KEYGEN);
        if (create_secp256k1_pair(ctx, &g_vrf_key) != 0) {
            ocall_print_string("create_secp256k1_pair vrf_key failed");
            secp256k1_context_destroy(ctx);
            return 1;
        }
    }

    secp256k1_context_destroy(ctx);
    g_sig_key_generated = true;
    return 0;
}

sgx_status_t ecall_get_target_info(sgx_target_info_t* target_info) {
    return sgx_self_target(target_info);
}

// 设置委员会聚合公钥 g_vk_gc
// 由主链节点在初始化或委员会公钥轮换时调用
sgx_status_t ecall_set_committee_pubkey(
    const uint8_t* pubkey,
    uint32_t pubkey_len
) {
    if (!pubkey || pubkey_len == 0 || pubkey_len > sizeof(g_vk_gc)) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    memset(g_vk_gc, 0, sizeof(g_vk_gc));
    memcpy(g_vk_gc, pubkey, pubkey_len);
    g_vk_gc_set = true;
    return SGX_SUCCESS;
}

// 2.1 生成远程证明
uint32_t ecall_create_report(
    const sgx_target_info_t* p_qe3_target, 
    sgx_report_t* p_report,
    uint8_t* e_hardware_id,uint32_t e_hardware_id_len,
    uint8_t* e_committee_pubkey,uint32_t e_committee_pubkey_len,
    uint8_t* e_sig_pubkey,uint32_t e_sig_pubkey_len,
    uint8_t* e_vrf_pubkey,uint32_t e_vrf_pubkey_len
) {
    int res = create_sig_and_vrf_key();
    if (res != 0) {
        ocall_print_string("create_sig_and_vrf_key failed");
        return SGX_ERROR_UNEXPECTED;
    }

    ensure_hardware_id();
    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        ocall_print_string("ensure_enclave_key failed");
        return SGX_ERROR_UNEXPECTED;
    }

    uint8_t enclave_pk_be[64];
    reverse32(g_enclave_pk.gx, enclave_pk_be);
    reverse32(g_enclave_pk.gy, enclave_pk_be + 32);

    uint8_t buf[288];
    memcpy(buf, hardware_id, 32);
    memcpy(buf + 32, g_vk_gc, sizeof(g_vk_gc));
    memcpy(buf + 32 + sizeof(g_vk_gc), g_sig_key.addr32, sizeof(g_sig_key.addr32));
    memcpy(buf + 32 + sizeof(g_vk_gc) + sizeof(g_sig_key.addr32), g_vrf_key.addr32, sizeof(g_vrf_key.addr32));
    memcpy(buf + 32 + sizeof(g_vk_gc) + sizeof(g_sig_key.addr32) + sizeof(g_vrf_key.addr32), enclave_pk_be, sizeof(enclave_pk_be));

    sgx_sha256_hash_t hash = {0};
    sgx_status_t ret = sgx_sha256_msg(buf, sizeof(buf), &hash);
    if (ret != SGX_SUCCESS) {
        ocall_print_string("sgx_sha256_msg failed");
        return ret;
    }

    sgx_report_data_t report_data = { 0 };
    memcpy(report_data.d, hash, sizeof(hash));

    ret = sgx_create_report(p_qe3_target, &report_data, p_report);
    if (ret != SGX_SUCCESS) {
        ocall_print_string("sgx_create_report failed");
        return ret;
    }

    memcpy(e_hardware_id, hardware_id, e_hardware_id_len);
    memcpy(e_committee_pubkey, g_vk_gc, e_committee_pubkey_len);
    memcpy(e_sig_pubkey, g_sig_key.addr32, e_sig_pubkey_len);
    memcpy(e_vrf_pubkey, g_vrf_key.addr32, e_vrf_pubkey_len);
    return SGX_SUCCESS;
}

sgx_status_t ecall_get_identity(
    uint8_t* e_hardware_id,uint32_t e_hardware_id_len,
    uint8_t* e_committee_pubkey,uint32_t e_committee_pubkey_len,
    uint8_t* e_sig_pubkey,uint32_t e_sig_pubkey_len,
    uint8_t* e_vrf_pubkey,uint32_t e_vrf_pubkey_len
) {
    if (!e_hardware_id || e_hardware_id_len != 32 ||
        !e_committee_pubkey || e_committee_pubkey_len > sizeof(g_vk_gc) ||
        !e_sig_pubkey || e_sig_pubkey_len != 32 ||
        !e_vrf_pubkey || e_vrf_pubkey_len != 32) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    int res = create_sig_and_vrf_key();
    if (res != 0) {
        ocall_print_string("create_sig_and_vrf_key failed");
        return SGX_ERROR_UNEXPECTED;
    }
    ensure_hardware_id();
    memcpy(e_hardware_id, hardware_id, e_hardware_id_len);
    memcpy(e_committee_pubkey, g_vk_gc, e_committee_pubkey_len);
    memcpy(e_sig_pubkey, g_sig_key.addr32, e_sig_pubkey_len);
    memcpy(e_vrf_pubkey, g_vrf_key.addr32, e_vrf_pubkey_len);
    return SGX_SUCCESS;
}

// keccak256哈希函数，基于MIRACL SHA3原语，使用keccak填充(0x01)而非NIST SHA3填充(0x06)
// 与以太坊keccak256兼容
static void keccak256_hash(const uint8_t* data, size_t len, uint8_t* out) {
    sha3 sh;
    SHA3_init(&sh, SHA3_HASH256);
    for (size_t i = 0; i < len; i++) {
        SHA3_process(&sh, data[i]);
    }
    // keccak填充: 0x01 ... 0x80 (NIST SHA3使用0x06)
    int q = sh.rate - sh.length;
    if (q == 1) {
        SHA3_process(&sh, 0x81);  // 0x80 | 0x01
    } else {
        SHA3_process(&sh, 0x01);
        while (sh.length != sh.rate - 1) {
            SHA3_process(&sh, 0x00);
        }
        SHA3_process(&sh, 0x80);
    }
    SHA3_squeeze(&sh, (char*)out, sh.len);
}

// 计算域分隔符: domain = keccak256(domain_str)
static void compute_domain(const char* domain_str, uint8_t* out) {
    size_t len = strlen(domain_str);
    keccak256_hash((const uint8_t*)domain_str, len, out);
}

sgx_status_t verify_gc_auth_internal(const uint8_t* cert_msg, uint32_t cert_msg_len, const uint8_t* cert_sig, uint32_t cert_sig_len) {
    if (!cert_msg || cert_msg_len == 0 || !cert_sig || cert_sig_len == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    BLS12381::BLS_INIT();

    // MIRACL's BLS_CORE_VERIFY reads the message/signature into internal scratch
    // bounded by each octet's .max, so .max must be the real capacity (>= .len).
    // The {0, len, val} form leaves .max=0 and makes every real committee
    // signature fail verification (this path is never hit by tests that pass a
    // nil certificate). Required for op 1 and for committee-mvp certs.
    octet M = {static_cast<int>(cert_msg_len), static_cast<int>(cert_msg_len), (char*)cert_msg};
    octet SIG = {static_cast<int>(cert_sig_len), static_cast<int>(cert_sig_len), (char*)cert_sig};
    octet W = {static_cast<int>(sizeof(g_vk_gc)), static_cast<int>(sizeof(g_vk_gc)), (char*)g_vk_gc};

    int ret = BLS12381::BLS_CORE_VERIFY(&SIG, &M, &W);
    return (ret == BLS_OK) ? SGX_SUCCESS : SGX_ERROR_INVALID_SIGNATURE;
}

// 验证当值集合证书Cert_eA
// 对应API: committee_getActiveSetCert
// cert_eA_msg: active_set_cert_digest (委员会签名的消息, 32字节)
// cert_eA_sig: cert_e_a (委员会聚合签名)
// active_tee_list: 活跃TEE列表，每项32字节sigPubKey
// active_tee_count: 活跃TEE数量
// active_set_cert_input: ABI编码的活跃集证书字段（不含域分隔符），格式为:
//   chain_id(32) || contract_address(32) || uint256(1)(32) || epoch(32) ||
//   active_set_hash(32) || active_threshold(32) || active_limit(32) ||
//   pk_ch_hash(32) || committee_pub_key_version(32) = 288字节
// active_set_cert_input_len: active_set_cert_input长度（应为288）
bool verify_cert_ea(
    const uint8_t* cert_eA_msg, uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig, uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list, uint32_t active_tee_count,
    const uint8_t* active_set_cert_input, uint32_t active_set_cert_input_len
) {
    int key_res = create_sig_and_vrf_key();
    if (key_res != 0) {
        return false;
    }

    // 步骤1: 验证委员会聚合签名 Verify_GC(Cert_eA)
    // 委员会对 active_set_cert_digest 签名，产生 cert_e_a
    // active_set_cert_digest = keccak256(abi.encode(
    //   T_GOVERN_ACTIVE_SET_CERT_V1, chain_id, contract_address, 1,
    //   epoch, active_set_hash, active_threshold, active_limit,
    //   pk_ch_hash, committee_pub_key_version))
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        sgx_status_t auth_ret = verify_gc_auth_internal(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len);
        if (auth_ret != SGX_SUCCESS) {
            return false;
        }
    }

    // 步骤2: 重算 active_set_cert_digest 并与 cert_eA_msg 比对
    // active_set_cert_input 包含除域分隔符外的所有ABI编码字段（9 * 32 = 288字节）
    if (cert_eA_msg && cert_eA_msg_len >= 32 &&
        active_set_cert_input && active_set_cert_input_len == 288) {
        // 计算域分隔符 ACTIVE_SET_CERT_DOMAIN = keccak256("T_GOVERN_ACTIVE_SET_CERT_V1")
        uint8_t domain[32];
        compute_domain("T_GOVERN_ACTIVE_SET_CERT_V1", domain);

        // 构建完整ABI编码: domain(32) || active_set_cert_input(288) = 320字节
        uint8_t abi_encoded[320];
        memcpy(abi_encoded, domain, 32);
        memcpy(abi_encoded + 32, active_set_cert_input, 288);

        // 计算active_set_cert_digest = keccak256(abi_encoded)
        uint8_t computed_digest[32];
        keccak256_hash(abi_encoded, sizeof(abi_encoded), computed_digest);

        // 比对重新计算的digest与传入的cert_eA_msg
        if (memcmp(computed_digest, cert_eA_msg, 32) != 0) {
            return false;
        }
    }

    // 步骤3: 验证本节点sigPubKey是否在active_tee_list中
    if (active_tee_list && active_tee_count > 0) {
        for (size_t i = 0; i < active_tee_count; i++) {
            const uint8_t* current_id = active_tee_list + i * 32;
            if (memcmp(current_id, g_sig_key.addr32, 32) == 0) {
                return true;
            }
        }
        return false;
    }

    return true;
}

// 验证授权证书Cert_req^auth
// 对应API: committee_getRedactAuthorization
// cert_req_auth_msg: authorization_digest (委员会签名的消息, 32字节)
// cert_req_auth_sig: cert_req_auth (委员会聚合签名)
// d_req: 编辑请求摘要 (32字节)
// d_policy: 策略摘要 (32字节)
// committee_pub_key_version: 委员会公钥版本 (uint256, 32字节大端)
// expires_at: 过期时间戳 (uint256, 32字节大端)
bool verify_cert_req_auth(
    const uint8_t* cert_req_auth_msg, uint32_t cert_req_auth_msg_len,
    const uint8_t* cert_req_auth_sig, uint32_t cert_req_auth_sig_len,
    const uint8_t* d_req, uint32_t d_req_len,
    const uint8_t* d_policy, uint32_t d_policy_len,
    const uint8_t* committee_pub_key_version, uint32_t cpkv_len,
    const uint8_t* expires_at, uint32_t expires_at_len
) {
    // Authorization certificate validation, including the BLS verification
    // and request-context digest binding below.
    tgovern_bench_scope_t benchmark_scope(
        TGO_BENCH_AUTH_CERT_VERIFY,
        cert_req_auth_msg && cert_req_auth_msg_len > 0 && cert_req_auth_sig && cert_req_auth_sig_len > 0);
    ensure_hardware_id();

    // 步骤1: 验证委员会聚合签名 Verify_GC(Cert_req^auth)
    // 委员会对 authorization_digest 签名，产生 cert_req_auth
    if (cert_req_auth_msg && cert_req_auth_msg_len > 0 &&
        cert_req_auth_sig && cert_req_auth_sig_len > 0) {
        sgx_status_t auth_ret = verify_gc_auth_internal(
            cert_req_auth_msg, cert_req_auth_msg_len,
            cert_req_auth_sig, cert_req_auth_sig_len);
        if (auth_ret != SGX_SUCCESS) {
            return false;
        }
    }

    // 步骤2: 验证d_req与请求上下文一致性
    // authorization_digest = keccak256(abi.encode(
    //   T_GOVERN_REDACT_AUTH_V1, d_req, d_policy,
    //   committee_pub_key_version, expires_at))
    // 重新计算authorization_digest并与cert_req_auth_msg比对
    if (cert_req_auth_msg && cert_req_auth_msg_len >= 32 &&
        d_req && d_req_len == 32 &&
        d_policy && d_policy_len == 32 &&
        committee_pub_key_version && cpkv_len == 32 &&
        expires_at && expires_at_len == 32) {

        // 计算域分隔符 AUTH_DOMAIN = keccak256("T_GOVERN_REDACT_AUTH_V1")
        uint8_t auth_domain[32];
        compute_domain("T_GOVERN_REDACT_AUTH_V1", auth_domain);

        // ABI编码: AUTH_DOMAIN(32) || d_req(32) || d_policy(32) ||
        //          committee_pub_key_version(32) || expires_at(32) = 160字节
        uint8_t abi_encoded[160];
        memset(abi_encoded, 0, sizeof(abi_encoded));
        memcpy(abi_encoded, auth_domain, 32);
        memcpy(abi_encoded + 32, d_req, 32);
        memcpy(abi_encoded + 64, d_policy, 32);
        memcpy(abi_encoded + 96, committee_pub_key_version, 32);
        memcpy(abi_encoded + 128, expires_at, 32);

        // 计算authorization_digest = keccak256(abi_encoded)
        uint8_t computed_digest[32];
        keccak256_hash(abi_encoded, sizeof(abi_encoded), computed_digest);

        // 比对重新计算的authorization_digest与传入的cert_req_auth_msg
        if (memcmp(computed_digest, cert_req_auth_msg, 32) != 0) {
            return false;
        }
    }

    return true;
}

// ============ NITCH ============
static nitch::MiraclBls12381* g_ctx = nullptr;

static std::vector<uint8_t> miracl_g1_to_kilic(const std::vector<uint8_t>& miracl_bytes) {
    if (miracl_bytes.size() > 1 && (miracl_bytes[0] == 0x04 || miracl_bytes[0] == 0x03 || miracl_bytes[0] == 0x02)) {
        return std::vector<uint8_t>(miracl_bytes.begin() + 1, miracl_bytes.end());
    }
    return miracl_bytes;
}

static std::vector<uint8_t> strip_miracl_prefix_local(const std::vector<uint8_t>& miracl_bytes) {
    return miracl_g1_to_kilic(miracl_bytes);
}

static std::vector<uint8_t> miracl_g2_to_kilic(const std::vector<uint8_t>& miracl_bytes) {
    if (miracl_bytes.size() > 1 && (miracl_bytes[0] == 0x04 || miracl_bytes[0] == 0x03 || miracl_bytes[0] == 0x02)) {
        return std::vector<uint8_t>(miracl_bytes.begin() + 1, miracl_bytes.end());
    }
    return miracl_bytes;
}

static std::vector<uint8_t> kilic_to_miracl_g1(const std::vector<uint8_t>& kilic_bytes) {
    std::vector<uint8_t> result;
    result.reserve(kilic_bytes.size() + 1);
    result.push_back(0x04);
    result.insert(result.end(), kilic_bytes.begin(), kilic_bytes.end());
    return result;
}

static std::vector<uint8_t> kilic_to_miracl_g2(const std::vector<uint8_t>& kilic_bytes) {
    std::vector<uint8_t> result;
    result.reserve(kilic_bytes.size() + 1);
    result.push_back(0x04);
    result.insert(result.end(), kilic_bytes.begin(), kilic_bytes.end());
    return result;
}

static bool g_has_secret_key = false;
static nitch::Scalar g_secret_share;
static nitch::G2Point g_verification_key_g2;
static nitch::G1Point g_verification_key_g1;
static int g_my_index = 0;
static int g_threshold = 0;
static int g_num_parties = 0;

static nitch::MiraclBls12381* get_context() {
    if (!g_ctx) {
        g_ctx = new nitch::MiraclBls12381();
    }
    return g_ctx;
}

static octet make_octet(char* buffer, int capacity) {
    octet out;
    out.len = 0;
    out.max = capacity;
    out.val = buffer;
    return out;
}

extern "C" {

    int putchar(int c) {
        return c;
    }

    int puts(const char *s) {
        return 0;
    }

    int printf(const char *format, ...) {
        return 0;
    }

    int sprintf(char *str, const char *format, ...) {
        if (str && format) {
            str[0] = '\0';
        }
        return 0;
    }

    int fprintf(void *stream, const char *format, ...) {
        return 0;
    }
}

#define BLS_G1_SIZE 49
#define BLS_G2_SIZE 193

static bool is_power_of_two_int(int value) {
    return value > 0 && (value & (value - 1)) == 0;
}

static nitch::Scalar evaluation_point_for_index(
    nitch::MiraclBls12381* ctx,
    int domain_size,
    int participant_index)
{
    const nitch::EvaluationDomain& domain = ctx->evaluation_domain(domain_size);
    return domain.powers[static_cast<size_t>(participant_index % domain.size)];
}

static std::vector<nitch::Scalar> batch_inverse_scalars(
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

sgx_status_t ecall_distkg_create_deal(
    int dealer_index,
    int threshold,
    int num_parties,
    uint8_t* commitments_a,
    uint32_t commitments_a_buf_size,
    uint32_t* commitments_a_out_size,
    uint8_t* commitment_b0,
    uint32_t commitment_b0_buf_size,
    uint32_t* commitment_b0_out_size,
    uint8_t* shares_out,
    uint32_t shares_buf_size,
    uint32_t* shares_out_size,
    uint8_t* g1_bytes,
    uint32_t g1_buf_size,
    uint8_t* g2_bytes,
    uint32_t g2_buf_size,
    char* p_hex,
    uint32_t p_hex_buf_size,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!is_power_of_two_int(num_parties) || threshold >= num_parties || dealer_index < 1 || dealer_index > num_parties) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!commitments_a || !commitment_b0 || !shares_out || !g1_bytes || !g2_bytes || !p_hex) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();
    (void)ctx->evaluation_domain(num_parties);

    // TCH.DKG local computation.  Certificate validation and parameter
    // checks above intentionally remain outside this interval.
    tgovern_bench_scope_t benchmark_scope(TGO_BENCH_DKG_LOCAL);

    std::vector<nitch::Scalar> coeffs;
    coeffs.reserve(threshold + 1);
    for (int k = 0; k <= threshold; k++) {
        coeffs.push_back(ctx->random_scalar());
    }

    const int G1_KILIC_SIZE = 96;
    const int G2_KILIC_SIZE = 192;

    int total_a = (threshold + 1) * G1_KILIC_SIZE;
    if (static_cast<uint32_t>(total_a) > commitments_a_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }

    for (int k = 0; k <= threshold; k++) {
        nitch::G1Point pt = ctx->g1_mul(ctx->g1_generator(), coeffs[k]);
        std::vector<uint8_t> pt_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(pt, false));
        if (static_cast<int>(pt_bytes.size()) != G1_KILIC_SIZE) {
            return SGX_ERROR_UNEXPECTED;
        }
        memcpy(commitments_a + k * G1_KILIC_SIZE, pt_bytes.data(), G1_KILIC_SIZE);
    }
    *commitments_a_out_size = static_cast<uint32_t>(total_a);

    nitch::G2Point b0 = ctx->g2_mul(ctx->g2_generator(), coeffs[0]);
    std::vector<uint8_t> b0_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(b0, false));
    if (b0_bytes.size() > commitment_b0_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(commitment_b0, b0_bytes.data(), b0_bytes.size());
    *commitment_b0_out_size = static_cast<uint32_t>(b0_bytes.size());

    uint32_t shares_offset = 0;
    uint8_t temp_ct[128];
    uint32_t temp_ct_size;
    for (int j = 1; j <= num_parties; j++) {
        nitch::Scalar x_scalar = evaluation_point_for_index(ctx, num_parties, j);
        nitch::Scalar eval_result = ctx->scalar_from_u64(0);
        for (int k = threshold; k >= 0; k--) {
            eval_result = ctx->scalar_mul(eval_result, x_scalar);
            eval_result = ctx->scalar_add(eval_result, coeffs[k]);
        }
        std::vector<uint8_t> share_bytes = ctx->scalar_to_bytes(eval_result);
        
        bool has_peer_key = (g_peer_shared_keys.find(j) != g_peer_shared_keys.end());
        if (has_peer_key) {
            sgx_status_t enc_ret = encrypt_for_peer(j, share_bytes.data(), 
                static_cast<uint32_t>(share_bytes.size()), temp_ct, sizeof(temp_ct), &temp_ct_size);
            if (enc_ret != SGX_SUCCESS) {
                return enc_ret;
            }
            uint32_t total_size = 1 + temp_ct_size;
            if (shares_offset + sizeof(uint32_t) + total_size > shares_buf_size) {
                return SGX_ERROR_OUT_OF_MEMORY;
            }
            memcpy(shares_out + shares_offset, &total_size, sizeof(uint32_t));
            shares_offset += sizeof(uint32_t);
            shares_out[shares_offset] = 0x01;
            shares_offset += 1;
            memcpy(shares_out + shares_offset, temp_ct, temp_ct_size);
            shares_offset += temp_ct_size;
        } else {
            uint32_t plain_size = static_cast<uint32_t>(share_bytes.size());
            uint32_t total_size = 1 + plain_size;
            if (shares_offset + sizeof(uint32_t) + total_size > shares_buf_size) {
                return SGX_ERROR_OUT_OF_MEMORY;
            }
            memcpy(shares_out + shares_offset, &total_size, sizeof(uint32_t));
            shares_offset += sizeof(uint32_t);
            shares_out[shares_offset] = 0x00;
            shares_offset += 1;
            memcpy(shares_out + shares_offset, share_bytes.data(), plain_size);
            shares_offset += plain_size;
        }
    }
    *shares_out_size = shares_offset;

    std::vector<uint8_t> g1_ser = miracl_g1_to_kilic(ctx->g1_to_bytes(ctx->g1_generator(), false));
    if (g1_ser.size() > g1_buf_size) return SGX_ERROR_OUT_OF_MEMORY;
    memcpy(g1_bytes, g1_ser.data(), g1_ser.size());

    std::vector<uint8_t> g2_ser = miracl_g2_to_kilic(ctx->g2_to_bytes(ctx->g2_generator(), false));
    if (g2_ser.size() > g2_buf_size) return SGX_ERROR_OUT_OF_MEMORY;
    memcpy(g2_bytes, g2_ser.data(), g2_ser.size());

    nitch::Scalar order_scalar;
    BIG_rcopy(order_scalar.value, CURVE_Order);
    std::string order_hex = ctx->scalar_to_hex(order_scalar);
    if (order_hex.size() + 1 > p_hex_buf_size) return SGX_ERROR_OUT_OF_MEMORY;
    memcpy(p_hex, order_hex.c_str(), order_hex.size() + 1);
    return SGX_SUCCESS;
}

sgx_status_t ecall_distkg_verify_share(
    int receiver_index,
    int dealer_index,
    int threshold,
    int num_parties,
    const uint8_t* commitments_a,
    uint32_t commitments_a_size,
    const uint8_t* commitment_b0,
    uint32_t commitment_b0_size,
    const uint8_t* encrypted_share_bytes,
    uint32_t encrypted_share_bytes_size,
    int* valid,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!commitments_a || !commitment_b0 || !encrypted_share_bytes || !valid) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!is_power_of_two_int(num_parties) || threshold < 0 || threshold >= num_parties ||
        receiver_index < 1 || receiver_index > num_parties) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    uint8_t share_data[64];
    uint32_t share_data_size;

    if (encrypted_share_bytes_size > 0 && encrypted_share_bytes[0] == 0x00) {
        uint32_t plain_size = encrypted_share_bytes_size - 1;
        if (plain_size > sizeof(share_data)) {
            *valid = 0;
            return SGX_ERROR_OUT_OF_MEMORY;
        }
        memcpy(share_data, encrypted_share_bytes + 1, plain_size);
        share_data_size = plain_size;
    } else if (encrypted_share_bytes_size > 0 && encrypted_share_bytes[0] == 0x01) {
        sgx_status_t dec_ret = decrypt_from_peer(dealer_index, encrypted_share_bytes + 1, 
            encrypted_share_bytes_size - 1, share_data, sizeof(share_data), &share_data_size);
        if (dec_ret != SGX_SUCCESS) {
            *valid = 0;
            return dec_ret;
        }
    } else {
        sgx_status_t dec_ret = decrypt_from_peer(dealer_index, encrypted_share_bytes, 
            encrypted_share_bytes_size, share_data, sizeof(share_data), &share_data_size);
        if (dec_ret != SGX_SUCCESS) {
            *valid = 0;
            return dec_ret;
        }
    }

    nitch::MiraclBls12381* ctx = get_context();

    nitch::G1Point g1_gen = ctx->g1_generator();
    std::vector<uint8_t> g1_gen_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(g1_gen, false));
    int g1_pt_size = static_cast<int>(g1_gen_bytes.size());

    if (static_cast<int>(commitments_a_size) != (threshold + 1) * g1_pt_size) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    std::vector<nitch::G1Point> a_points;
    a_points.reserve(threshold + 1);
    for (int k = 0; k <= threshold; k++) {
        const uint8_t* chunk_ptr = commitments_a + k * g1_pt_size;
        std::vector<uint8_t> kilic_chunk(chunk_ptr, chunk_ptr + g1_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g1(kilic_chunk);

        char buf[256];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G1Point pt;
        if (!ECP_fromOctet(&pt.value, &oct)) {
            *valid = 0;
            return SGX_SUCCESS;
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

    nitch::Scalar share_scalar = ctx->scalar_from_bytes_mod_order(
        std::vector<uint8_t>(share_data, share_data + share_data_size));
    nitch::G1Point lhs = ctx->g1_mul(g1_gen, share_scalar);

    if (!ctx->g1_equal(lhs, rhs)) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    // Verify pairing consistency: e(a0, g2_gen) == e(g1_gen, b0)
    // This ensures the G1 and G2 commitments are consistent with each other
    if (commitment_b0_size > 0 && commitment_b0 != nullptr) {
        nitch::G2Point g2_gen = ctx->g2_generator();
        std::vector<uint8_t> kilic_b0(commitment_b0, commitment_b0 + commitment_b0_size);
        std::vector<uint8_t> miracl_b0 = kilic_to_miracl_g2(kilic_b0);

        char b0_buf[384];
        memcpy(b0_buf, miracl_b0.data(), miracl_b0.size());
        octet b0_oct = make_octet(b0_buf, static_cast<int>(miracl_b0.size()));
        b0_oct.len = static_cast<int>(miracl_b0.size());

        nitch::G2Point b0_pt;
        if (!ECP2_fromOctet(&b0_pt.value, &b0_oct)) {
            *valid = 0;
            return SGX_SUCCESS;
        }

        nitch::GTPoint lhs_pair = ctx->pairing(a_points[0], g2_gen);
        nitch::GTPoint rhs_pair = ctx->pairing(g1_gen, b0_pt);

        if (!ctx->gt_equal(lhs_pair, rhs_pair)) {
            *valid = 0;
            return SGX_SUCCESS;
        }
    }

    *valid = 1;
    return SGX_SUCCESS;
}

static std::vector<nitch::Scalar> compute_lagrange_at_zero(
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

sgx_status_t ecall_distkg_extract(
    int my_index,
    int threshold,
    int num_parties,
    const int32_t* qual_indices,
    uint32_t qual_count,
    const uint8_t* all_b0_data,
    uint32_t all_b0_data_size,
    const int32_t* b0_dealer_map,
    uint32_t b0_dealer_map_count,
    const uint8_t* all_a0_data,
    uint32_t all_a0_data_size,
    const int32_t* a0_dealer_map,
    uint32_t a0_dealer_map_count,
    const uint8_t* encrypted_shares_data,
    uint32_t encrypted_shares_data_size,
    const int32_t* shares_from_map,
    uint32_t shares_from_map_count,
    uint8_t* global_pk,
    uint32_t global_pk_buf_size,
    uint32_t* global_pk_out_size,
    uint8_t* global_pk_g1,
    uint32_t global_pk_g1_buf_size,
    uint32_t* global_pk_g1_out_size,
    uint8_t* verification_key,
    uint32_t vk_buf_size,
    uint32_t* vk_out_size,
    const uint8_t* pkch_chain_id,
    uint32_t pkch_chain_id_len,
    const uint8_t* pkch_contract_address,
    uint32_t pkch_contract_address_len,
    const uint8_t* pkch_tee_owner,
    uint32_t pkch_tee_owner_len,
    uint8_t* pkch_signature,
    uint32_t pkch_signature_buf_size,
    uint32_t* pkch_signature_out_size,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!qual_indices || !all_b0_data || !all_a0_data || !encrypted_shares_data ||
        !global_pk || !global_pk_g1 || !verification_key) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (qual_count == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!is_power_of_two_int(num_parties) || threshold < 0 || threshold >= num_parties ||
        my_index < 1 || my_index > num_parties) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

    std::vector<nitch::Scalar> lambdas = compute_lagrange_at_zero(ctx, num_parties, qual_indices, qual_count);
    if (lambdas.size() != qual_count) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    std::vector<uint8_t> g2_id_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(ctx->g2_identity(), false));
    int g2_pt_size = static_cast<int>(g2_id_bytes.size());
    std::vector<uint8_t> g1_id_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(ctx->g1_identity(), false));
    int g1_pt_size = static_cast<int>(g1_id_bytes.size());
    int scalar_size = static_cast<int>(ctx->scalar_to_bytes(ctx->scalar_from_u64(0)).size());

    nitch::G2Point pk = ctx->g2_identity();
    for (uint32_t q = 0; q < qual_count; q++) {
        const uint8_t* b0_ptr = all_b0_data + q * g2_pt_size;
        if (b0_ptr + g2_pt_size > all_b0_data + all_b0_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        std::vector<uint8_t> kilic_chunk(b0_ptr, b0_ptr + g2_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g2(kilic_chunk);

        char buf[384];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G2Point b0_pt;
        if (!ECP2_fromOctet(&b0_pt.value, &oct)) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        nitch::G2Point b0_weighted = ctx->g2_mul(b0_pt, lambdas[q]);
        pk = ctx->g2_add(pk, b0_weighted);
    }

    nitch::G1Point pk_g1 = ctx->g1_identity();
    for (uint32_t q = 0; q < qual_count; q++) {
        const uint8_t* a0_ptr = all_a0_data + q * g1_pt_size;
        if (a0_ptr + g1_pt_size > all_a0_data + all_a0_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        std::vector<uint8_t> kilic_chunk(a0_ptr, a0_ptr + g1_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g1(kilic_chunk);

        char buf[256];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G1Point a0_pt;
        if (!ECP_fromOctet(&a0_pt.value, &oct)) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        nitch::G1Point a0_weighted = ctx->g1_mul(a0_pt, lambdas[q]);
        pk_g1 = ctx->g1_add(pk_g1, a0_weighted);
    }

    nitch::Scalar sk = ctx->scalar_from_u64(0);
    uint32_t offset = 0;
    for (uint32_t i = 0; i < qual_count; i++) {
        if (offset + sizeof(uint32_t) > encrypted_shares_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        uint32_t entry_size;
        memcpy(&entry_size, encrypted_shares_data + offset, sizeof(uint32_t));
        offset += sizeof(uint32_t);
        
        if (offset + entry_size > encrypted_shares_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        
        int dealer_index = shares_from_map[i];
        uint8_t share_buf[64];
        uint32_t share_buf_size;

        if (entry_size > 0 && encrypted_shares_data[offset] == 0x00) {
            uint32_t plain_size = entry_size - 1;
            if (plain_size > sizeof(share_buf)) {
                return SGX_ERROR_OUT_OF_MEMORY;
            }
            memcpy(share_buf, encrypted_shares_data + offset + 1, plain_size);
            share_buf_size = plain_size;
        } else if (entry_size > 0 && encrypted_shares_data[offset] == 0x01) {
            sgx_status_t dec_ret = decrypt_from_peer(dealer_index, 
                encrypted_shares_data + offset + 1, entry_size - 1, share_buf, sizeof(share_buf), &share_buf_size);
            if (dec_ret != SGX_SUCCESS) {
                return dec_ret;
            }
        } else {
            sgx_status_t dec_ret = decrypt_from_peer(dealer_index, 
                encrypted_shares_data + offset, entry_size, share_buf, sizeof(share_buf), &share_buf_size);
            if (dec_ret != SGX_SUCCESS) {
                return dec_ret;
            }
        }
        offset += entry_size;
        
        nitch::Scalar s = ctx->scalar_from_bytes_mod_order(
            std::vector<uint8_t>(share_buf, share_buf + share_buf_size));
        nitch::Scalar s_weighted = ctx->scalar_mul(lambdas[i], s);
        sk = ctx->scalar_add(sk, s_weighted);
    }

    nitch::G1Point vk = ctx->g1_mul(ctx->g1_generator(), sk);
    nitch::G2Point vk_g2 = ctx->g2_mul(ctx->g2_generator(), sk);

    g_secret_share = sk;
    g_verification_key_g2 = vk_g2;
    g_verification_key_g1 = vk;
    g_my_index = my_index;
    g_threshold = threshold;
    g_num_parties = num_parties;
    g_has_secret_key = true;

    std::vector<uint8_t> pk_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(pk, false));
    if (pk_bytes.size() > global_pk_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(global_pk, pk_bytes.data(), pk_bytes.size());
    *global_pk_out_size = static_cast<uint32_t>(pk_bytes.size());

    if (pkch_chain_id || pkch_contract_address || pkch_tee_owner || pkch_signature || pkch_signature_out_size) {
        if (!pkch_chain_id || !pkch_contract_address || !pkch_tee_owner || !pkch_signature || !pkch_signature_out_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        if (pkch_chain_id_len != 32 || pkch_contract_address_len != 32 || pkch_tee_owner_len != 32 || pkch_signature_buf_size < 65) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        if (create_sig_and_vrf_key() != 0) {
            return SGX_ERROR_UNEXPECTED;
        }

        uint8_t pk_ch_hash[32];
        keccak256_hash(pk_bytes.data(), pk_bytes.size(), pk_ch_hash);

        uint8_t pkch_domain[32];
        compute_domain("T_GOVERN_PKCH_SET_V1", pkch_domain);

        uint8_t abi_encoded[160];
        memcpy(abi_encoded, pkch_domain, 32);
        memcpy(abi_encoded + 32, pkch_chain_id, 32);
        memcpy(abi_encoded + 64, pkch_contract_address, 32);
        memcpy(abi_encoded + 96, pkch_tee_owner, 32);
        memcpy(abi_encoded + 128, pk_ch_hash, 32);

        uint8_t pkch_digest[32];
        keccak256_hash(abi_encoded, sizeof(abi_encoded), pkch_digest);

        int sig_len = secp256k1_sign_recoverable_65(pkch_digest, g_sig_key.sk, pkch_signature);
        secure_bzero(pk_ch_hash, sizeof(pk_ch_hash));
        secure_bzero(pkch_domain, sizeof(pkch_domain));
        secure_bzero(abi_encoded, sizeof(abi_encoded));
        secure_bzero(pkch_digest, sizeof(pkch_digest));
        if (sig_len == 0) {
            return SGX_ERROR_UNEXPECTED;
        }
        *pkch_signature_out_size = static_cast<uint32_t>(sig_len);
    }

    std::vector<uint8_t> pkg1_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(pk_g1, false));
    if (pkg1_bytes.size() > global_pk_g1_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(global_pk_g1, pkg1_bytes.data(), pkg1_bytes.size());
    *global_pk_g1_out_size = static_cast<uint32_t>(pkg1_bytes.size());

    std::vector<uint8_t> vk_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(vk, false));
    if (vk_bytes.size() > vk_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(verification_key, vk_bytes.data(), vk_bytes.size());
    *vk_out_size = static_cast<uint32_t>(vk_bytes.size());

    return SGX_SUCCESS;
}

sgx_status_t ecall_partial_adapt(
    const uint8_t* source_msg,
    uint32_t source_msg_len,
    const uint8_t* target_msg,
    uint32_t target_msg_len,
    const uint8_t* cert_req_auth_msg,
    uint32_t cert_req_auth_msg_len,
    const uint8_t* cert_req_auth_sig,
    uint32_t cert_req_auth_sig_len,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* d_req,
    uint32_t d_req_len,
    const uint8_t* d_policy,
    uint32_t d_policy_len,
    const uint8_t* committee_pub_key_version,
    uint32_t cpkv_len,
    const uint8_t* expires_at,
    uint32_t expires_at_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len,
    const uint8_t* d_req_input,
    uint32_t d_req_input_len,
    uint8_t* contribution_out,
    uint32_t contribution_buf_size,
    uint32_t* contribution_out_size,
    uint8_t* proof_challenge_out,
    uint32_t proof_challenge_buf_size,
    uint8_t* proof_response_out,
    uint32_t proof_response_buf_size,
    uint8_t* y_out,
    uint32_t y_buf_size,
    uint32_t* y_out_size,
    uint8_t* sigma_out,
    uint32_t sigma_buf_size,
    uint32_t* sigma_out_size)
{
    if (!source_msg || !target_msg || !contribution_out || !proof_challenge_out
        || !proof_response_out || !y_out || !y_out_size
        || !d_req) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!g_has_secret_key) {
        return SGX_ERROR_UNEXPECTED;
    }

    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }

    // 确保签名密钥已生成（sigma 签名需要）
    int res = create_sig_and_vrf_key();
    if (res != 0) {
        return SGX_ERROR_UNEXPECTED;
    }

    // 重算 d_req 并与传入值比对，防止请求内容与授权证书不匹配
    // d_req = keccak256(abi.encode(T_GOVERN_REDACT_REQ_V1, chain_id, contract_address,
    //   version, epoch, req_id, body_hashes))
    // d_req_input 包含除域分隔符外的所有ABI编码字段（384字节）:
    //   chain_id(32) || contract_address(32) || version(32) || epoch(32) ||
    //   req_id(32) || body_hashes(7*32=224)
    if (d_req && d_req_len == 32 &&
        d_req_input && d_req_input_len == 384) {
        uint8_t req_domain[32];
        compute_domain("T_GOVERN_REDACT_REQ_V1", req_domain);

        // 构建完整ABI编码: domain(32) || d_req_input(384) = 416字节
        uint8_t abi_encoded[416];
        memcpy(abi_encoded, req_domain, 32);
        memcpy(abi_encoded + 32, d_req_input, 384);

        uint8_t computed_d_req[32];
        keccak256_hash(abi_encoded, sizeof(abi_encoded), computed_d_req);

        if (memcmp(computed_d_req, d_req, 32) != 0) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }

    // 验证 d_req_input 中的 epoch 与 active_set_cert_input 中的 epoch 一致
    // 防止跨 epoch 重放攻击
    // d_req_input 中 epoch 偏移: 96 (chain_id(32) + contract_address(32) + version(32))
    // active_set_cert_input 中 epoch 偏移: 96 (chain_id(32) + contract_address(32) + uint256(1)(32))
    if (d_req_input && d_req_input_len == 384 &&
        active_set_cert_input && active_set_cert_input_len == 288) {
        const uint8_t* d_req_epoch = d_req_input + 96;
        const uint8_t* cert_ea_epoch = active_set_cert_input + 96;
        if (memcmp(d_req_epoch, cert_ea_epoch, 32) != 0) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
    }

    // 验证授权证书 Cert_req^auth: BLS签名 + d_req上下文一致性
    if (cert_req_auth_msg && cert_req_auth_msg_len > 0 &&
        cert_req_auth_sig && cert_req_auth_sig_len > 0) {
        bool auth_ok = verify_cert_req_auth(
            cert_req_auth_msg, cert_req_auth_msg_len,
            cert_req_auth_sig, cert_req_auth_sig_len,
            d_req, d_req_len,
            d_policy, d_policy_len,
            committee_pub_key_version, cpkv_len,
            expires_at, expires_at_len);
        if (!auth_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }

    // 验证当值集合证书 Cert_eA: BLS签名 + 本节点在活跃集中
    if (cert_eA_msg && cert_eA_msg_len > 0 &&
        cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }

    nitch::MiraclBls12381* ctx = get_context();

    // TCH.PCollide begins after authorization and active-set validation. It
    // includes the local contribution and NIZK material, but excludes the
    // separate TEE signature measurement below.
    tgovern_bench_scope_t pcollide_benchmark_scope(TGO_BENCH_PCOLLIDE);

    auto simple_hash_to_g1 = [&](const std::string& msg) -> nitch::G1Point {
        nitch::G1Point hash_point = ctx->hash_to_g1(msg);
        return hash_point;
    };

    std::string src_str(reinterpret_cast<const char*>(source_msg), source_msg_len);
    std::string tgt_str(reinterpret_cast<const char*>(target_msg), target_msg_len);

    nitch::G1Point h1m = simple_hash_to_g1(src_str);
    nitch::G1Point h1mPrime = simple_hash_to_g1(tgt_str);
    nitch::G1Point y = ctx->g1_sub(h1m, h1mPrime);

    nitch::G1Point contribution = ctx->g1_mul(y, g_secret_share);

    nitch::Scalar tau = ctx->random_scalar();
    nitch::G1Point g1_gen = ctx->g1_generator();
    nitch::G1Point t1 = ctx->g1_mul(g1_gen, tau);
    nitch::G1Point t2 = ctx->g1_mul(y, tau);

    std::vector<uint8_t> challenge_data;
    auto append_point = [&](const std::vector<uint8_t>& bytes) {
        std::vector<uint8_t> stripped = strip_miracl_prefix_local(bytes);
        challenge_data.insert(challenge_data.end(), stripped.begin(), stripped.end());
    };
    append_point(ctx->g1_to_bytes(g1_gen, false));
    append_point(ctx->g1_to_bytes(y, false));
    append_point(ctx->g1_to_bytes(g_verification_key_g1, false));
    append_point(ctx->g1_to_bytes(contribution, false));
    append_point(ctx->g1_to_bytes(t1, false));
    append_point(ctx->g1_to_bytes(t2, false));

    nitch::G1Point challenge_point = ctx->hash_to_g1(std::string(reinterpret_cast<const char*>(challenge_data.data()), challenge_data.size()));
    nitch::Scalar challenge = ctx->hash_to_scalar(challenge_data);
    nitch::Scalar response = ctx->scalar_add(tau, ctx->scalar_mul(challenge, g_secret_share));

    std::vector<uint8_t> contrib_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(contribution, false));
    if (contrib_bytes.size() > contribution_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(contribution_out, contrib_bytes.data(), contrib_bytes.size());
    *contribution_out_size = static_cast<uint32_t>(contrib_bytes.size());

    if (proof_challenge_buf_size < 32) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    {
        std::vector<uint8_t> challenge_bytes = ctx->scalar_to_bytes(challenge);
        if (challenge_bytes.size() > proof_challenge_buf_size) {
            return SGX_ERROR_OUT_OF_MEMORY;
        }
        memcpy(proof_challenge_out, challenge_bytes.data(), challenge_bytes.size());
    }

    std::vector<uint8_t> response_bytes = ctx->scalar_to_bytes(response);
    if (response_bytes.size() > proof_response_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(proof_response_out, response_bytes.data(), response_bytes.size());

    std::vector<uint8_t> y_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(y, false));
    if (y_bytes.size() > y_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(y_out, y_bytes.data(), y_bytes.size());
    *y_out_size = static_cast<uint32_t>(y_bytes.size());

    // The signature has its own table entry, so it is intentionally outside
    // the PCollide cycle interval.
    pcollide_benchmark_scope.stop();

    {
        // σ_i ← Sign_sk_i^sig(d_req || s_i)
        // 使用 secp256k1 ECDSA 可恢复签名，输出 65 字节
        std::vector<uint8_t> sign_data;
        sign_data.reserve(d_req_len + contrib_bytes.size());
        sign_data.insert(sign_data.end(), d_req, d_req + d_req_len);
        sign_data.insert(sign_data.end(), contrib_bytes.begin(), contrib_bytes.end());

        uint8_t msg_hash[32];
        keccak256_hash(sign_data.data(), sign_data.size(), msg_hash);

        if (sigma_buf_size < 65) {
            return SGX_ERROR_OUT_OF_MEMORY;
        }
        tgovern_bench_scope_t tee_sign_benchmark_scope(TGO_BENCH_TEE_SIGN);
        int sig_len = secp256k1_sign_recoverable_65(msg_hash, g_sig_key.sk, sigma_out);
        if (sig_len == 0) {
            return SGX_ERROR_UNEXPECTED;
        }
        *sigma_out_size = static_cast<uint32_t>(sig_len);
    }

    return SGX_SUCCESS;
}

sgx_status_t ecall_erase_share(
    uint64_t epoch,
    int node_index,
    const uint8_t* chain_id,
    uint32_t chain_id_len,
    const uint8_t* contract_address,
    uint32_t contract_address_len,
    const uint8_t* tee_owner,
    uint32_t tee_owner_len,
    uint8_t* erase_proof_hash_out,
    uint32_t erase_proof_hash_buf_size,
    uint8_t* proof_out,
    uint32_t proof_buf_size,
    uint32_t* proof_out_size)
{
    if (!chain_id || chain_id_len != 32 ||
        !contract_address || contract_address_len != 32 ||
        !tee_owner || tee_owner_len != 32 ||
        !erase_proof_hash_out || erase_proof_hash_buf_size < 32 ||
        !proof_out || !proof_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (proof_buf_size < 65) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }

    // 确保签名密钥已生成
    int res = create_sig_and_vrf_key();
    if (res != 0) {
        return SGX_ERROR_UNEXPECTED;
    }

    // The erase interval covers state zeroisation and generation of the
    // signed erase proof.  Argument validation and identity key setup above
    // are not charged to share erasure.
    tgovern_bench_scope_t erase_benchmark_scope(TGO_BENCH_SHARE_ERASE);

    // 擦除本地陷门份额
    memset(&g_secret_share, 0, sizeof(g_secret_share));
    memset(&g_verification_key_g2, 0, sizeof(g_verification_key_g2));
    memset(&g_verification_key_g1, 0, sizeof(g_verification_key_g1));
    g_has_secret_key = false;
    g_my_index = 0;
    g_threshold = 0;
    g_num_parties = 0;

    // 计算 eraseProofHash = keccak256(epoch(32字节大端) || sigPubKey(32字节) || "ERASED")
    uint8_t epoch_bytes[32];
    memset(epoch_bytes, 0, 32);
    for (int i = 0; i < 8; i++) {
        epoch_bytes[31 - i] = (uint8_t)((epoch >> (i * 8)) & 0xFF);
    }

    static const char erased_tag[] = "ERASED";
    uint8_t erase_data[32 + 32 + 6];
    memcpy(erase_data, epoch_bytes, 32);
    memcpy(erase_data + 32, g_sig_key.addr32, 32);
    memcpy(erase_data + 64, erased_tag, 6);

    keccak256_hash(erase_data, sizeof(erase_data), erase_proof_hash_out);

    // 计算 eraseProofDigest = keccak256(abi.encode(
    //   ERASE_DOMAIN, chainId, contractAddress, sigPubKey, teeOwner, epoch, eraseProofHash))
    // ERASE_DOMAIN = keccak256("T_GOVERN_TEE_ERASE_V1")
    uint8_t erase_domain[32];
    compute_domain("T_GOVERN_TEE_ERASE_V1", erase_domain);

    // ABI 编码: 7 * 32 = 224 字节
    uint8_t abi_encoded[224];
    memcpy(abi_encoded, erase_domain, 32);
    memcpy(abi_encoded + 32, chain_id, 32);
    memcpy(abi_encoded + 64, contract_address, 32);
    memcpy(abi_encoded + 96, g_sig_key.addr32, 32);
    memcpy(abi_encoded + 128, tee_owner, 32);
    memcpy(abi_encoded + 160, epoch_bytes, 32);
    memcpy(abi_encoded + 192, erase_proof_hash_out, 32);

    uint8_t erase_proof_digest[32];
    keccak256_hash(abi_encoded, sizeof(abi_encoded), erase_proof_digest);

    // 使用 secp256k1 ECDSA 可恢复签名生成擦除证明，输出 65 字节
    int sig_len = secp256k1_sign_recoverable_65(erase_proof_digest, g_sig_key.sk, proof_out);
    if (sig_len == 0) {
        return SGX_ERROR_UNEXPECTED;
    }
    *proof_out_size = static_cast<uint32_t>(sig_len);
    return SGX_SUCCESS;
}

// VRF 证明生成
// 对应规格: docs/tee-vrf-generation-spec.md
// 使用 secp256k1 ECDSA 可恢复签名对 vrfDigest 签名，生成 65 字节 proof
// 并派生 32 字节 vrfResult
sgx_status_t ecall_vrf_eval(
    const uint8_t* chain_id,
    uint32_t chain_id_len,
    const uint8_t* contract_address,
    uint32_t contract_address_len,
    uint64_t epoch,
    const uint8_t* hardware_id_input,
    uint32_t hardware_id_len,
    const uint8_t* vrf_pubkey,
    uint32_t vrf_pubkey_len,
    const uint8_t* seed,
    uint32_t seed_len,
    uint8_t* vrf_result_out,
    uint32_t vrf_result_buf_size,
    uint8_t* vrf_proof_out,
    uint32_t vrf_proof_buf_size,
    uint32_t* vrf_proof_out_size)
{
    if (!chain_id || chain_id_len != 32 ||
        !contract_address || contract_address_len != 32 ||
        !hardware_id_input || hardware_id_len != 32 ||
        !vrf_pubkey || vrf_pubkey_len != 32 ||
        !seed || seed_len != 32 ||
        !vrf_result_out || vrf_result_buf_size < 32 ||
        !vrf_proof_out || vrf_proof_buf_size < 65 ||
        !vrf_proof_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // 确保签名和VRF密钥已生成
    int res = create_sig_and_vrf_key();
    if (res != 0) {
        return SGX_ERROR_UNEXPECTED;
    }

    // 验证 sigPubKey 与本节点签名私钥派生值一致
    if (memcmp(hardware_id_input, g_sig_key.addr32, 32) != 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // 验证 vrf_pubkey 与本节点 VRF 私钥派生值一致
    if (memcmp(vrf_pubkey, g_vrf_key.addr32, 32) != 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // 验证 seed 非零
    bool seed_is_zero = true;
    for (size_t i = 0; i < 32; i++) {
        if (seed[i] != 0) {
            seed_is_zero = false;
            break;
        }
    }
    if (seed_is_zero) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // VRF.Comp: digest construction, recoverable-signature proof generation
    // and result derivation.  Key generation and input checks are excluded.
    tgovern_bench_scope_t vrf_benchmark_scope(TGO_BENCH_VRF_COMP);

    // 计算 VRF_DOMAIN = keccak256("T_GOVERN_TEE_VRF_V1")
    uint8_t vrf_domain[32];
    compute_domain("T_GOVERN_TEE_VRF_V1", vrf_domain);

    // epoch 转 32 字节大端
    uint8_t epoch_bytes[32];
    memset(epoch_bytes, 0, 32);
    for (int i = 0; i < 8; i++) {
        epoch_bytes[31 - i] = (uint8_t)((epoch >> (i * 8)) & 0xFF);
    }

    // 计算 vrfDigest = keccak256(abi.encode(
    //   VRF_DOMAIN, chain_id, contract_address, epoch, sigPubKey, vrf_pubkey, seed))
    // ABI 编码: 7 * 32 = 224 字节
    uint8_t abi_encoded[224];
    memcpy(abi_encoded, vrf_domain, 32);
    memcpy(abi_encoded + 32, chain_id, 32);
    memcpy(abi_encoded + 64, contract_address, 32);
    memcpy(abi_encoded + 96, epoch_bytes, 32);
    memcpy(abi_encoded + 128, g_sig_key.addr32, 32);
    memcpy(abi_encoded + 160, vrf_pubkey, 32);
    memcpy(abi_encoded + 192, seed, 32);

    uint8_t vrf_digest[32];
    keccak256_hash(abi_encoded, sizeof(abi_encoded), vrf_digest);

    // 使用 VRF 私钥对 vrfDigest 做 secp256k1 ECDSA 可恢复签名
    int sig_len = secp256k1_sign_recoverable_65(vrf_digest, g_vrf_key.sk, vrf_proof_out);
    if (sig_len != 65) {
        return SGX_ERROR_UNEXPECTED;
    }
    *vrf_proof_out_size = 65;

    // 派生 vrfResult = keccak256(abi.encodePacked(
    //   VRF_DOMAIN || seed || vrf_pubkey || r || s || v))
    // 即 32 + 32 + 32 + 32 + 32 + 1 = 161 字节
    uint8_t packed[161];
    memcpy(packed, vrf_domain, 32);
    memcpy(packed + 32, seed, 32);
    memcpy(packed + 64, vrf_pubkey, 32);
    memcpy(packed + 96, vrf_proof_out, 64);      // r || s
    memcpy(packed + 160, vrf_proof_out + 64, 1);  // v

    keccak256_hash(packed, sizeof(packed), vrf_result_out);

    return SGX_SUCCESS;
}

sgx_status_t ecall_get_enclave_state(int* has_secret_key, int* stored_index) {
    if (!has_secret_key || !stored_index) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    *has_secret_key = g_has_secret_key ? 1 : 0;
    *stored_index = g_my_index;
    return SGX_SUCCESS;
}

// Benchmark support ECALLs.  They expose aggregate diagnostics only; no
// enclave secret, intermediate cryptographic value or per-request trace is
// returned to the untrusted host.
sgx_status_t ecall_benchmark_noop() {
    asm volatile("" ::: "memory");
    return SGX_SUCCESS;
}

sgx_status_t ecall_benchmark_reset_stats() {
    sgx_spin_lock(&g_tgovern_bench_lock);
    memset(g_tgovern_bench_stats, 0, sizeof(g_tgovern_bench_stats));
    sgx_spin_unlock(&g_tgovern_bench_lock);
    return SGX_SUCCESS;
}

sgx_status_t ecall_benchmark_get_stats(
    uint32_t operation,
    uint64_t* calls,
    uint64_t* total_cycles,
    uint64_t* min_cycles,
    uint64_t* max_cycles)
{
    if (operation >= TGO_BENCH_OPERATION_COUNT || !calls || !total_cycles ||
        !min_cycles || !max_cycles) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    sgx_spin_lock(&g_tgovern_bench_lock);
    const tgovern_bench_stat_t& stat = g_tgovern_bench_stats[operation];
    *calls = stat.calls;
    *total_cycles = stat.total_cycles;
    *min_cycles = stat.min_cycles;
    *max_cycles = stat.max_cycles;
    sgx_spin_unlock(&g_tgovern_bench_lock);
    return SGX_SUCCESS;
}

sgx_status_t ecall_dkrs_create_deal(
    int dealer_index,
    int new_threshold,
    int new_num_parties,
    uint8_t* commitments_a,
    uint32_t commitments_a_buf_size,
    uint32_t* commitments_a_out_size,
    uint8_t* commitment_b0,
    uint32_t commitment_b0_buf_size,
    uint32_t* commitment_b0_out_size,
    uint8_t* shares_out,
    uint32_t shares_buf_size,
    uint32_t* shares_out_size,
    uint8_t* g1_bytes,
    uint32_t g1_buf_size,
    uint8_t* g2_bytes,
    uint32_t g2_buf_size,
    char* p_hex,
    uint32_t p_hex_buf_size,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!g_has_secret_key) {
        return SGX_ERROR_UNEXPECTED;
    }
    if (!is_power_of_two_int(new_num_parties) || new_threshold >= new_num_parties || dealer_index < 1) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!commitments_a || !commitment_b0 || !shares_out || !g1_bytes || !g2_bytes || !p_hex) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();
    (void)ctx->evaluation_domain(new_num_parties);

    std::vector<nitch::Scalar> coeffs;
    coeffs.reserve(new_threshold + 1);
    coeffs.push_back(g_secret_share);
    for (int k = 1; k <= new_threshold; k++) {
        coeffs.push_back(ctx->random_scalar());
    }

    const int G1_KILIC_SIZE = 96;
    const int G2_KILIC_SIZE = 192;

    int total_a = (new_threshold + 1) * G1_KILIC_SIZE;
    if (static_cast<uint32_t>(total_a) > commitments_a_buf_size) {
        return sgx_diag_return("dkrs_create_deal", "commitments_a_buffer", SGX_ERROR_OUT_OF_MEMORY,
            commitments_a_buf_size, static_cast<uint32_t>(total_a), static_cast<uint32_t>(new_threshold + 1), dealer_index);
    }

    for (int k = 0; k <= new_threshold; k++) {
        nitch::G1Point pt = ctx->g1_mul(ctx->g1_generator(), coeffs[k]);
        std::vector<uint8_t> pt_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(pt, false));
        if (static_cast<int>(pt_bytes.size()) != G1_KILIC_SIZE) {
            return SGX_ERROR_UNEXPECTED;
        }
        memcpy(commitments_a + k * G1_KILIC_SIZE, pt_bytes.data(), G1_KILIC_SIZE);
    }
    *commitments_a_out_size = static_cast<uint32_t>(total_a);

    nitch::G2Point b0 = ctx->g2_mul(ctx->g2_generator(), coeffs[0]);
    std::vector<uint8_t> b0_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(b0, false));
    if (b0_bytes.size() > commitment_b0_buf_size) {
        return sgx_diag_return("dkrs_create_deal", "commitment_b0_buffer", SGX_ERROR_OUT_OF_MEMORY,
            commitment_b0_buf_size, static_cast<uint32_t>(b0_bytes.size()), 0, dealer_index);
    }
    memcpy(commitment_b0, b0_bytes.data(), b0_bytes.size());
    *commitment_b0_out_size = static_cast<uint32_t>(b0_bytes.size());

    uint32_t shares_offset = 0;
    uint8_t temp_ct[128];
    uint32_t temp_ct_size;
    for (int j = 1; j <= new_num_parties; j++) {
        nitch::Scalar x_scalar = evaluation_point_for_index(ctx, new_num_parties, j);
        nitch::Scalar eval_result = ctx->scalar_from_u64(0);
        for (int k = new_threshold; k >= 0; k--) {
            eval_result = ctx->scalar_mul(eval_result, x_scalar);
            eval_result = ctx->scalar_add(eval_result, coeffs[k]);
        }
        std::vector<uint8_t> share_bytes = ctx->scalar_to_bytes(eval_result);

        bool has_peer_key = (g_peer_shared_keys.find(j) != g_peer_shared_keys.end());
        if (has_peer_key) {
            sgx_status_t enc_ret = encrypt_for_peer(j, share_bytes.data(),
                static_cast<uint32_t>(share_bytes.size()), temp_ct, sizeof(temp_ct), &temp_ct_size);
            if (enc_ret != SGX_SUCCESS) {
                return enc_ret;
            }
            uint32_t total_size = 1 + temp_ct_size;
            if (shares_offset + sizeof(uint32_t) + total_size > shares_buf_size) {
                return sgx_diag_return("dkrs_create_deal", "shares_out_encrypted_buffer", SGX_ERROR_OUT_OF_MEMORY,
                    shares_buf_size, shares_offset + static_cast<uint32_t>(sizeof(uint32_t)) + total_size,
                    total_size, j);
            }
            memcpy(shares_out + shares_offset, &total_size, sizeof(uint32_t));
            shares_offset += sizeof(uint32_t);
            shares_out[shares_offset] = 0x01;
            shares_offset += 1;
            memcpy(shares_out + shares_offset, temp_ct, temp_ct_size);
            shares_offset += temp_ct_size;
        } else {
            uint32_t plain_size = static_cast<uint32_t>(share_bytes.size());
            uint32_t total_size = 1 + plain_size;
            if (shares_offset + sizeof(uint32_t) + total_size > shares_buf_size) {
                return sgx_diag_return("dkrs_create_deal", "shares_out_plain_buffer", SGX_ERROR_OUT_OF_MEMORY,
                    shares_buf_size, shares_offset + static_cast<uint32_t>(sizeof(uint32_t)) + total_size,
                    total_size, j);
            }
            memcpy(shares_out + shares_offset, &total_size, sizeof(uint32_t));
            shares_offset += sizeof(uint32_t);
            shares_out[shares_offset] = 0x00;
            shares_offset += 1;
            memcpy(shares_out + shares_offset, share_bytes.data(), plain_size);
            shares_offset += plain_size;
        }
    }
    *shares_out_size = shares_offset;

    std::vector<uint8_t> g1_ser = miracl_g1_to_kilic(ctx->g1_to_bytes(ctx->g1_generator(), false));
    if (g1_ser.size() > g1_buf_size) {
        return sgx_diag_return("dkrs_create_deal", "g1_buffer", SGX_ERROR_OUT_OF_MEMORY,
            g1_buf_size, static_cast<uint32_t>(g1_ser.size()), 0, dealer_index);
    }
    memcpy(g1_bytes, g1_ser.data(), g1_ser.size());

    std::vector<uint8_t> g2_ser = miracl_g2_to_kilic(ctx->g2_to_bytes(ctx->g2_generator(), false));
    if (g2_ser.size() > g2_buf_size) {
        return sgx_diag_return("dkrs_create_deal", "g2_buffer", SGX_ERROR_OUT_OF_MEMORY,
            g2_buf_size, static_cast<uint32_t>(g2_ser.size()), 0, dealer_index);
    }
    memcpy(g2_bytes, g2_ser.data(), g2_ser.size());

    nitch::Scalar order_scalar;
    BIG_rcopy(order_scalar.value, CURVE_Order);
    std::string order_hex = ctx->scalar_to_hex(order_scalar);
    if (order_hex.size() + 1 > p_hex_buf_size) {
        return sgx_diag_return("dkrs_create_deal", "p_hex_buffer", SGX_ERROR_OUT_OF_MEMORY,
            p_hex_buf_size, static_cast<uint32_t>(order_hex.size() + 1), 0, dealer_index);
    }
    memcpy(p_hex, order_hex.c_str(), order_hex.size() + 1);

    return SGX_SUCCESS;
}

sgx_status_t ecall_dkrs_verify_share(
    int receiver_index,
    int dealer_index,
    int new_threshold,
    int new_num_parties,
    const uint8_t* commitments_a,
    uint32_t commitments_a_size,
    const uint8_t* commitment_b0,
    uint32_t commitment_b0_size,
    const uint8_t* encrypted_share_bytes,
    uint32_t encrypted_share_bytes_size,
    int* valid,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!commitments_a || !commitment_b0 || !encrypted_share_bytes || !valid) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!is_power_of_two_int(new_num_parties) || new_threshold < 0 || new_threshold >= new_num_parties ||
        receiver_index < 1 || receiver_index > new_num_parties) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    uint8_t share_data[64];
    uint32_t share_data_size;

    if (encrypted_share_bytes_size > 0 && encrypted_share_bytes[0] == 0x00) {
        uint32_t plain_size = encrypted_share_bytes_size - 1;
        if (plain_size > sizeof(share_data)) {
            *valid = 0;
            return SGX_ERROR_OUT_OF_MEMORY;
        }
        memcpy(share_data, encrypted_share_bytes + 1, plain_size);
        share_data_size = plain_size;
    } else if (encrypted_share_bytes_size > 0 && encrypted_share_bytes[0] == 0x01) {
        sgx_status_t dec_ret = decrypt_from_peer(dealer_index, encrypted_share_bytes + 1,
            encrypted_share_bytes_size - 1, share_data, sizeof(share_data), &share_data_size);
        if (dec_ret != SGX_SUCCESS) {
            *valid = 0;
            return dec_ret;
        }
    } else {
        sgx_status_t dec_ret = decrypt_from_peer(dealer_index, encrypted_share_bytes,
            encrypted_share_bytes_size, share_data, sizeof(share_data), &share_data_size);
        if (dec_ret != SGX_SUCCESS) {
            *valid = 0;
            return dec_ret;
        }
    }

    nitch::MiraclBls12381* ctx = get_context();

    nitch::G1Point g1_gen = ctx->g1_generator();
    std::vector<uint8_t> g1_gen_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(g1_gen, false));
    int g1_pt_size = static_cast<int>(g1_gen_bytes.size());

    if (static_cast<int>(commitments_a_size) != (new_threshold + 1) * g1_pt_size) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    std::vector<nitch::G1Point> a_points;
    a_points.reserve(new_threshold + 1);
    for (int k = 0; k <= new_threshold; k++) {
        const uint8_t* chunk_ptr = commitments_a + k * g1_pt_size;
        std::vector<uint8_t> kilic_chunk(chunk_ptr, chunk_ptr + g1_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g1(kilic_chunk);

        char buf[256];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G1Point pt;
        if (!ECP_fromOctet(&pt.value, &oct)) {
            *valid = 0;
            return SGX_SUCCESS;
        }
        a_points.push_back(pt);
    }

    nitch::G1Point rhs = ctx->g1_identity();
    nitch::Scalar j_power = ctx->scalar_from_u64(1);
    nitch::Scalar j_scalar = evaluation_point_for_index(ctx, new_num_parties, receiver_index);

    for (int k = 0; k <= new_threshold; k++) {
        nitch::G1Point term = ctx->g1_mul(a_points[k], j_power);
        rhs = ctx->g1_add(rhs, term);
        j_power = ctx->scalar_mul(j_power, j_scalar);
    }

    nitch::Scalar share_scalar = ctx->scalar_from_bytes_mod_order(
        std::vector<uint8_t>(share_data, share_data + share_data_size));
    nitch::G1Point lhs = ctx->g1_mul(g1_gen, share_scalar);

    if (!ctx->g1_equal(lhs, rhs)) {
        *valid = 0;
        return SGX_SUCCESS;
    }

    // Verify pairing consistency: e(a0, g2_gen) == e(g1_gen, b0)
    if (commitment_b0_size > 0 && commitment_b0 != nullptr) {
        nitch::G2Point g2_gen = ctx->g2_generator();
        std::vector<uint8_t> kilic_b0(commitment_b0, commitment_b0 + commitment_b0_size);
        std::vector<uint8_t> miracl_b0 = kilic_to_miracl_g2(kilic_b0);

        char b0_buf[384];
        memcpy(b0_buf, miracl_b0.data(), miracl_b0.size());
        octet b0_oct = make_octet(b0_buf, static_cast<int>(miracl_b0.size()));
        b0_oct.len = static_cast<int>(miracl_b0.size());

        nitch::G2Point b0_pt;
        if (!ECP2_fromOctet(&b0_pt.value, &b0_oct)) {
            *valid = 0;
            return SGX_SUCCESS;
        }

        nitch::GTPoint lhs_pair = ctx->pairing(a_points[0], g2_gen);
        nitch::GTPoint rhs_pair = ctx->pairing(g1_gen, b0_pt);

        if (!ctx->gt_equal(lhs_pair, rhs_pair)) {
            *valid = 0;
            return SGX_SUCCESS;
        }
    }

    *valid = 1;
    return SGX_SUCCESS;
}

sgx_status_t ecall_dkrs_extract(
    int my_index,
    int new_threshold,
    int new_num_parties,
    int old_num_parties,
    const int32_t* qual_old_indices,
    uint32_t qual_old_count,
    const uint8_t* all_b0_data,
    uint32_t all_b0_data_size,
    const int32_t* b0_dealer_map,
    uint32_t b0_dealer_map_count,
    const uint8_t* all_a0_data,
    uint32_t all_a0_data_size,
    const int32_t* a0_dealer_map,
    uint32_t a0_dealer_map_count,
    const uint8_t* encrypted_shares_data,
    uint32_t encrypted_shares_data_size,
    const int32_t* shares_from_map,
    uint32_t shares_from_map_count,
    uint8_t* global_pk,
    uint32_t global_pk_buf_size,
    uint32_t* global_pk_out_size,
    uint8_t* global_pk_g1,
    uint32_t global_pk_g1_buf_size,
    uint32_t* global_pk_g1_out_size,
    uint8_t* verification_key,
    uint32_t vk_buf_size,
    uint32_t* vk_out_size,
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len,
    const uint8_t* active_tee_list,
    uint32_t active_tee_count,
    const uint8_t* active_set_cert_input,
    uint32_t active_set_cert_input_len)
{
    ensure_enclave_key();
    if (cert_eA_msg && cert_eA_msg_len > 0 && cert_eA_sig && cert_eA_sig_len > 0) {
        bool ea_ok = verify_cert_ea(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len,
            active_tee_list, active_tee_count,
            active_set_cert_input, active_set_cert_input_len);
        if (!ea_ok) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }
    if (!qual_old_indices || !all_b0_data || !all_a0_data || !encrypted_shares_data ||
        !global_pk || !global_pk_g1 || !verification_key) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (qual_old_count == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!is_power_of_two_int(new_num_parties) || !is_power_of_two_int(old_num_parties) ||
        new_threshold < 0 || new_threshold >= new_num_parties ||
        my_index < 1 || my_index > new_num_parties) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

    std::vector<nitch::Scalar> lambdas = compute_lagrange_at_zero(ctx, old_num_parties, qual_old_indices, qual_old_count);
    if (lambdas.size() != qual_old_count) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    std::vector<uint8_t> g2_id_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(ctx->g2_identity(), false));
    int g2_pt_size = static_cast<int>(g2_id_bytes.size());
    std::vector<uint8_t> g1_id_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(ctx->g1_identity(), false));
    int g1_pt_size = static_cast<int>(g1_id_bytes.size());

    nitch::G2Point pk = ctx->g2_identity();
    for (uint32_t q = 0; q < qual_old_count; q++) {
        const uint8_t* b0_ptr = all_b0_data + q * g2_pt_size;
        if (b0_ptr + g2_pt_size > all_b0_data + all_b0_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        std::vector<uint8_t> kilic_chunk(b0_ptr, b0_ptr + g2_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g2(kilic_chunk);

        char buf[384];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G2Point b0_pt;
        if (!ECP2_fromOctet(&b0_pt.value, &oct)) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        nitch::G2Point b0_weighted = ctx->g2_mul(b0_pt, lambdas[q]);
        pk = ctx->g2_add(pk, b0_weighted);
    }

    nitch::G1Point pk_g1 = ctx->g1_identity();
    for (uint32_t q = 0; q < qual_old_count; q++) {
        const uint8_t* a0_ptr = all_a0_data + q * g1_pt_size;
        if (a0_ptr + g1_pt_size > all_a0_data + all_a0_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        std::vector<uint8_t> kilic_chunk(a0_ptr, a0_ptr + g1_pt_size);
        std::vector<uint8_t> miracl_chunk = kilic_to_miracl_g1(kilic_chunk);

        char buf[256];
        memcpy(buf, miracl_chunk.data(), miracl_chunk.size());
        octet oct = make_octet(buf, static_cast<int>(miracl_chunk.size()));
        oct.len = static_cast<int>(miracl_chunk.size());

        nitch::G1Point a0_pt;
        if (!ECP_fromOctet(&a0_pt.value, &oct)) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        nitch::G1Point a0_weighted = ctx->g1_mul(a0_pt, lambdas[q]);
        pk_g1 = ctx->g1_add(pk_g1, a0_weighted);
    }

    nitch::Scalar sk = ctx->scalar_from_u64(0);
    uint32_t offset = 0;
    for (uint32_t i = 0; i < qual_old_count; i++) {
        if (offset + sizeof(uint32_t) > encrypted_shares_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        uint32_t entry_size;
        memcpy(&entry_size, encrypted_shares_data + offset, sizeof(uint32_t));
        offset += sizeof(uint32_t);

        if (offset + entry_size > encrypted_shares_data_size) {
            return SGX_ERROR_INVALID_PARAMETER;
        }

        int dealer_index = shares_from_map[i];
        uint8_t share_buf[64];
        uint32_t share_buf_size;

        if (entry_size > 0 && encrypted_shares_data[offset] == 0x00) {
            uint32_t plain_size = entry_size - 1;
            if (plain_size > sizeof(share_buf)) {
                return SGX_ERROR_OUT_OF_MEMORY;
            }
            memcpy(share_buf, encrypted_shares_data + offset + 1, plain_size);
            share_buf_size = plain_size;
        } else if (entry_size > 0 && encrypted_shares_data[offset] == 0x01) {
            sgx_status_t dec_ret = decrypt_from_peer(dealer_index,
                encrypted_shares_data + offset + 1, entry_size - 1, share_buf, sizeof(share_buf), &share_buf_size);
            if (dec_ret != SGX_SUCCESS) {
                return dec_ret;
            }
        } else {
            sgx_status_t dec_ret = decrypt_from_peer(dealer_index,
                encrypted_shares_data + offset, entry_size, share_buf, sizeof(share_buf), &share_buf_size);
            if (dec_ret != SGX_SUCCESS) {
                return dec_ret;
            }
        }
        offset += entry_size;

        nitch::Scalar s = ctx->scalar_from_bytes_mod_order(
            std::vector<uint8_t>(share_buf, share_buf + share_buf_size));
        nitch::Scalar s_weighted = ctx->scalar_mul(lambdas[i], s);
        sk = ctx->scalar_add(sk, s_weighted);
    }

    nitch::G1Point vk = ctx->g1_mul(ctx->g1_generator(), sk);
    nitch::G2Point vk_g2 = ctx->g2_mul(ctx->g2_generator(), sk);

    g_secret_share = sk;
    g_verification_key_g2 = vk_g2;
    g_verification_key_g1 = vk;
    g_my_index = my_index;
    g_threshold = new_threshold;
    g_num_parties = new_num_parties;
    g_has_secret_key = true;

    std::vector<uint8_t> pk_bytes = miracl_g2_to_kilic(ctx->g2_to_bytes(pk, false));
    if (pk_bytes.size() > global_pk_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(global_pk, pk_bytes.data(), pk_bytes.size());
    *global_pk_out_size = static_cast<uint32_t>(pk_bytes.size());

    std::vector<uint8_t> pkg1_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(pk_g1, false));
    if (pkg1_bytes.size() > global_pk_g1_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(global_pk_g1, pkg1_bytes.data(), pkg1_bytes.size());
    *global_pk_g1_out_size = static_cast<uint32_t>(pkg1_bytes.size());

    std::vector<uint8_t> vk_bytes = miracl_g1_to_kilic(ctx->g1_to_bytes(vk, false));
    if (vk_bytes.size() > vk_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(verification_key, vk_bytes.data(), vk_bytes.size());
    *vk_out_size = static_cast<uint32_t>(vk_bytes.size());

    return SGX_SUCCESS;
}
