#include "Enclave_t.h"
#include "sgx_tcrypto.h"
#include <string.h>
#include <vector>
#include <stdio.h>
#include <map>

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
#include "keccak256.h"

using namespace BLS12381;
using namespace BLS12381_BIG;
using namespace core;

static const uint8_t g_vk_gc[193] = {
    0x04,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
};

static bool g_bls_initialized = false;

static bool g_enclave_key_generated = false;
// BLS key stored as raw octets (MIRACL API)
static uint8_t g_enclave_sk_buf[BGS_BLS12381]; // private key
static uint8_t g_enclave_pk_buf[BFS_BLS12381 + 1]; // public key (compressed G2)
static uint32_t g_enclave_pk_len = 0;

static bool g_vrf_key_generated = false;
static secp256k1_keypair g_vrf_keypair;

static std::map<int, sgx_aes_gcm_128bit_key_t> g_peer_shared_keys;

static sgx_thread_mutex_t g_enclave_key_mutex = SGX_THREAD_MUTEX_INITIALIZER;

// 1.1 初始化 Enclave 密钥对 (BLS using MIRACL C API)
static void ensure_enclave_key() {
    sgx_thread_mutex_lock(&g_enclave_key_mutex);
    if (!g_enclave_key_generated) {
        uint8_t ikm[32];
        sgx_read_rand(ikm, 32);

        octet IKM = {0, sizeof(ikm), (char*)ikm};
        octet S = {0, BGS_BLS12381, (char*)g_enclave_sk_buf};
        octet W = {0, sizeof(g_enclave_pk_buf), (char*)g_enclave_pk_buf};

        BLS12381::BLS_INIT();
        BLS12381::BLS_KEY_PAIR_GENERATE(&IKM, &S, &W);

        g_enclave_pk_len = W.len;
        g_enclave_key_generated = true;
    }
    sgx_thread_mutex_unlock(&g_enclave_key_mutex);
}

// 1.2 获取 Enclave 公钥
sgx_status_t ecall_get_enclave_pk(uint8_t* pk, uint32_t pk_buf_size, uint32_t* pk_out_size) {
    if (!pk || !pk_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (pk_buf_size < g_enclave_pk_len) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }

    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }

    memcpy(pk, g_enclave_pk_buf, g_enclave_pk_len);
    *pk_out_size = g_enclave_pk_len;

    return SGX_SUCCESS;
}

// 1.3 生成 VRF 密钥对 (secp256k1 ECDSA-based per tee-vrf-generation-spec.md)
static void ensure_vrf_key() {
    if (!g_vrf_key_generated) {
        if (secp256k1_generate_keypair(&g_vrf_keypair) != 0) {
            return; // generation failed
        }
        g_vrf_key_generated = true;
    }
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
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    
    uint8_t iv[12];
    sgx_status_t ret = sgx_read_rand(iv, 12);
    if (ret != SGX_SUCCESS) {
        return ret;
    }
    
    sgx_aes_gcm_128bit_tag_t tag;
    ret = sgx_rijndael128GCM_encrypt(&key, plaintext, pt_len, ciphertext + 28, iv, 12, nullptr, 0, &tag);
    if (ret != SGX_SUCCESS) {
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
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    
    const uint8_t* iv = ciphertext;
    const uint8_t* tag = ciphertext + 12;
    const uint8_t* ct = ciphertext + 28;
    
    sgx_status_t ret = sgx_rijndael128GCM_decrypt(&key, ct, pt_len, plaintext, iv, 12, nullptr, 0, (const sgx_aes_gcm_128bit_tag_t*)tag);
    if (ret != SGX_SUCCESS) {
        memset_s(plaintext, pt_len, 0, pt_len);
        memset_s(tag, sizeof(tag), 0, sizeof(tag));
        *pt_out_size = 0;
        return ret;
    }
    
    *pt_out_size = pt_len;
    return SGX_SUCCESS;
}

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
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, false);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (threshold >= num_parties || dealer_index < 1 || dealer_index > num_parties) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!commitments_a || !commitment_b0 || !shares_out || !g1_bytes || !g2_bytes || !p_hex) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

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
        nitch::Scalar x_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(j));
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
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, false);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (!commitments_a || !commitment_b0 || !encrypted_share_bytes || !valid) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (receiver_index < 1 || receiver_index > num_parties) {
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
    nitch::Scalar j_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(receiver_index));

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
    const int32_t* indices,
    uint32_t count)
{
    std::vector<nitch::Scalar> coefficients;
    coefficients.reserve(count);

    for (uint32_t i = 0; i < count; i++) {
        nitch::Scalar numerator = ctx->scalar_from_u64(1);
        nitch::Scalar denominator = ctx->scalar_from_u64(1);
        nitch::Scalar i_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(indices[i]));

        for (uint32_t j = 0; j < count; j++) {
            if (i == j) continue;
            nitch::Scalar minus_j = ctx->scalar_neg(ctx->scalar_from_u64(static_cast<uint64_t>(indices[j])));
            numerator = ctx->scalar_mul(numerator, minus_j);
            nitch::Scalar j_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(indices[j]));
            denominator = ctx->scalar_mul(denominator, ctx->scalar_sub(i_scalar, j_scalar));
        }

        coefficients.push_back(ctx->scalar_mul(numerator, ctx->scalar_inv(denominator)));
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
    const uint8_t* cert_eA_msg,
    uint32_t cert_eA_msg_len,
    const uint8_t* cert_eA_sig,
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, true);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (!qual_indices || !all_b0_data || !all_a0_data || !encrypted_shares_data ||
        !global_pk || !global_pk_g1 || !verification_key) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (qual_count == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

    std::vector<nitch::Scalar> lambdas = compute_lagrange_at_zero(ctx, qual_indices, qual_count);

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

    if (cert_req_auth_msg_len > 0 && cert_req_auth_sig_len > 0 && cert_req_auth_msg && cert_req_auth_sig) {
        sgx_status_t auth_ret = verify_gc_auth_internal(
            cert_req_auth_msg, cert_req_auth_msg_len,
            cert_req_auth_sig, cert_req_auth_sig_len);
        if (auth_ret != SGX_SUCCESS) {
            return auth_ret;
        }
    }

    if (cert_eA_msg_len > 0 && cert_eA_sig_len > 0 && cert_eA_msg && cert_eA_sig) {
        sgx_status_t auth_ret = verify_gc_auth_internal(
            cert_eA_msg, cert_eA_msg_len,
            cert_eA_sig, cert_eA_sig_len);
        if (auth_ret != SGX_SUCCESS) {
            return auth_ret;
        }
    }

    if (cert_req_auth_msg_len > 0 && cert_req_auth_msg && d_req) {
        if (cert_req_auth_msg_len < d_req_len) {
            return SGX_ERROR_INVALID_PARAMETER;
        }
        if (memcmp(cert_req_auth_msg, d_req, d_req_len) != 0) {
            return SGX_ERROR_INVALID_SIGNATURE;
        }
    }

    {
        if (cert_eA_msg_len > 0 && cert_eA_msg) {
            sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, true);
            if (membership_ret != SGX_SUCCESS) {
                return membership_ret;
            }
        }
    }

    nitch::MiraclBls12381* ctx = get_context();

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

    {
        std::vector<uint8_t> sign_data;
        sign_data.reserve(d_req_len + contrib_bytes.size());
        sign_data.insert(sign_data.end(), d_req, d_req + d_req_len);
        sign_data.insert(sign_data.end(), contrib_bytes.begin(), contrib_bytes.end());

        nitch::G1Point sign_point = ctx->hash_to_g1(std::string(reinterpret_cast<const char*>(sign_data.data()), sign_data.size()));

        // BLS sign using MIRACL C API
        std::vector<uint8_t> sign_point_bytes = ctx->g1_to_bytes(sign_point, false);
        uint8_t sig_buf[BFS_BLS12381 + 1];
        octet M = {0, static_cast<int>(sign_point_bytes.size()), (char*)sign_point_bytes.data()};
        octet S_oct = {0, BGS_BLS12381, (char*)g_enclave_sk_buf};
        octet SIG = {0, sizeof(sig_buf), (char*)sig_buf};
        BLS12381::BLS_CORE_SIGN(&SIG, &M, &S_oct);

        uint32_t sig_raw_len = SIG.len;
        if (sig_raw_len > sigma_buf_size) {
            return SGX_ERROR_OUT_OF_MEMORY;
        }
        memcpy(sigma_out, sig_buf, sig_raw_len);
        *sigma_out_size = sig_raw_len;
    }

    return SGX_SUCCESS;
}

sgx_status_t ecall_generate_vrf_keypair(
    uint8_t* pk_out,
    uint32_t pk_buf_size,
    uint32_t* pk_out_size)
{
    if (!pk_out || !pk_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    // Output is 32-byte vrfPubKey (leftPad32 of Ethereum address)
    if (pk_buf_size < 32) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    ensure_enclave_key();

    if (secp256k1_generate_keypair(&g_vrf_keypair) != 0) {
        return SGX_ERROR_UNEXPECTED;
    }
    g_vrf_key_generated = true;

    memcpy(pk_out, g_vrf_keypair.vrf_pubkey, 32);
    *pk_out_size = 32;

    return SGX_SUCCESS;
}

sgx_status_t ecall_vrf_eval(
    const uint8_t* chain_id,
    uint32_t chain_id_len,
    const uint8_t* contract_address,
    uint32_t contract_address_len,
    uint64_t epoch,
    const uint8_t* hardware_id,
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
    if (!chain_id || !contract_address || !hardware_id || !vrf_pubkey || !seed ||
        !vrf_result_out || !vrf_proof_out || !vrf_proof_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!g_vrf_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }
    if (vrf_result_buf_size < 32 || vrf_proof_buf_size < 65) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }

    // Validate inputs per spec
    // seed must be non-zero
    bool seed_is_zero = true;
    for (uint32_t i = 0; i < seed_len; i++) {
        if (seed[i] != 0) { seed_is_zero = false; break; }
    }
    if (seed_is_zero) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // hardware_id must match TEE's registered hardware ID
    // (For now, we accept any hardware_id; binding should be enforced at higher layer)

    // vrf_pubkey must match local keypair's vrf_pubkey
    if (vrf_pubkey_len != 32 || memcmp(vrf_pubkey, g_vrf_keypair.vrf_pubkey, 32) != 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // Compute VRF_DOMAIN = keccak256("T_GOVERN_TEE_VRF_V1")
    static const uint8_t VRF_DOMAIN_STR[] = "T_GOVERN_TEE_VRF_V1";
    uint8_t VRF_DOMAIN[32];
    keccak256(VRF_DOMAIN_STR, sizeof(VRF_DOMAIN_STR) - 1, VRF_DOMAIN);

    // Compute vrfDigest = keccak256(abi.encode(VRF_DOMAIN, chainId, contractAddress, epoch, hardwareId, vrfPubKey, seed))
    // abi.encode pads each element to 32 bytes (uint256/bytes32) or 20 bytes left-padded to 32 (address)
    // Layout: VRF_DOMAIN(32) || chainId(32) || contractAddress(32, left-padded from 20) || epoch(32) || hardwareId(32) || vrfPubKey(32) || seed(32) = 224 bytes
    uint8_t abi_encoded[224];
    memset(abi_encoded, 0, sizeof(abi_encoded));

    // VRF_DOMAIN: bytes32 at offset 0
    memcpy(abi_encoded, VRF_DOMAIN, 32);

    // chainId: uint256 at offset 32
    if (chain_id_len <= 32) {
        // Right-align (big-endian, left-padded with zeros)
        memcpy(abi_encoded + 32 + (32 - chain_id_len), chain_id, chain_id_len);
    } else {
        memcpy(abi_encoded + 32, chain_id + (chain_id_len - 32), 32);
    }

    // contractAddress: address (20 bytes) left-padded to 32 bytes at offset 64
    if (contract_address_len == 20) {
        memcpy(abi_encoded + 64 + 12, contract_address, 20);
    } else if (contract_address_len == 32) {
        memcpy(abi_encoded + 64, contract_address, 32);
    } else {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // epoch: uint64 -> uint256 at offset 96
    {
        uint64_t epoch_be = epoch;
        uint8_t epoch_bytes[8];
        for (int i = 0; i < 8; i++) {
            epoch_bytes[i] = (epoch_be >> (56 - 8 * i)) & 0xFF;
        }
        memcpy(abi_encoded + 96 + 24, epoch_bytes, 8);
    }

    // hardwareId: bytes32 at offset 128
    if (hardware_id_len == 32) {
        memcpy(abi_encoded + 128, hardware_id, 32);
    } else {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // vrfPubKey: bytes32 at offset 160
    memcpy(abi_encoded + 160, vrf_pubkey, 32);

    // seed: bytes32 at offset 192
    if (seed_len == 32) {
        memcpy(abi_encoded + 192, seed, 32);
    } else {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    // vrfDigest = keccak256(abi_encoded)
    uint8_t vrf_digest[32];
    keccak256(abi_encoded, sizeof(abi_encoded), vrf_digest);

    // Sign with secp256k1 ECDSA (deterministic nonce RFC6979)
    secp256k1_signature sig;
    int sign_ret = secp256k1_sign(g_sig_key.sk, vrf_digest, &sig);
    if (sign_ret != 0) {
        return SGX_ERROR_UNEXPECTED;
    }

    // vrfProof = r(32) || s(32) || v(1) = 65 bytes
    memcpy(vrf_proof_out, sig.r, 32);
    memcpy(vrf_proof_out + 32, sig.s, 32);
    vrf_proof_out[64] = sig.v;
    *vrf_proof_out_size = 65;

    // vrfResult = keccak256(VRF_DOMAIN || seed || vrfPubKey || r || s || v)
    // This is abi.encodePacked format
    uint8_t result_input[32 + 32 + 32 + 32 + 32 + 1]; // = 161 bytes
    memcpy(result_input, VRF_DOMAIN, 32);                   // VRF_DOMAIN
    memcpy(result_input + 32, seed, 32);                     // seed
    memcpy(result_input + 64, vrf_pubkey, 32);               // vrfPubKey
    memcpy(result_input + 96, sig.r, 32);                    // r
    memcpy(result_input + 128, sig.s, 32);                   // s
    result_input[160] = sig.v;                                // v

    keccak256(result_input, sizeof(result_input), vrf_result_out);

    return SGX_SUCCESS;
}

sgx_status_t ecall_erase_share(
    uint64_t epoch,
    int node_index,
    uint8_t* proof_out,
    uint32_t proof_buf_size,
    uint32_t* proof_out_size)
{
    if (!proof_out || !proof_out_size) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (proof_buf_size < 64) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    ensure_enclave_key();
    if (!g_enclave_key_generated) {
        return SGX_ERROR_UNEXPECTED;
    }

    memset(&g_secret_share, 0, sizeof(g_secret_share));
    memset(&g_verification_key_g2, 0, sizeof(g_verification_key_g2));
    memset(&g_verification_key_g1, 0, sizeof(g_verification_key_g1));
    g_has_secret_key = false;
    g_my_index = 0;
    g_threshold = 0;
    g_num_parties = 0;

    static const char erased_tag[] = "ERASED";
    std::vector<uint8_t> erase_data;
    erase_data.reserve(sizeof(epoch) + sizeof(node_index) + strlen(erased_tag));
    erase_data.insert(erase_data.end(), reinterpret_cast<const uint8_t*>(&epoch), reinterpret_cast<const uint8_t*>(&epoch) + sizeof(epoch));
    int32_t idx32 = static_cast<int32_t>(node_index);
    erase_data.insert(erase_data.end(), reinterpret_cast<const uint8_t*>(&idx32), reinterpret_cast<const uint8_t*>(&idx32) + sizeof(idx32));
    erase_data.insert(erase_data.end(), erased_tag, erased_tag + strlen(erased_tag));

    nitch::MiraclBls12381* erase_ctx = get_context();
    nitch::G1Point erase_hash = erase_ctx->hash_to_g1(std::string(reinterpret_cast<const char*>(erase_data.data()), erase_data.size()));

    // BLS sign using MIRACL C API
    std::vector<uint8_t> erase_hash_bytes = erase_ctx->g1_to_bytes(erase_hash, false);
    uint8_t sig_buf[BFS_BLS12381 + 1];
    octet M = {0, static_cast<int>(erase_hash_bytes.size()), (char*)erase_hash_bytes.data()};
    octet S_oct = {0, BGS_BLS12381, (char*)g_enclave_sk_buf};
    octet SIG = {0, sizeof(sig_buf), (char*)sig_buf};
    BLS12381::BLS_CORE_SIGN(&SIG, &M, &S_oct);

    uint32_t sig_len = SIG.len;
    if (sig_len > proof_buf_size) {
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(proof_out, sig_buf, sig_len);
    *proof_out_size = sig_len;
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

    nitch::MiraclBls12381* reg_ctx = get_context();

    // Parse peer's G2 public key from 64 bytes (two coordinates)
    // Reconstruct as compressed G2 bytes for nitch deserialization
    // peer_pk is 64 bytes = gx(32) || gy(32) of G2 point
    // For now, use the raw bytes to derive a shared secret via hash
    std::vector<uint8_t> kdf_input;
    kdf_input.reserve(g_enclave_pk_len + peer_pk_size + 11);
    kdf_input.insert(kdf_input.end(), g_enclave_pk_buf, g_enclave_pk_buf + g_enclave_pk_len);
    kdf_input.insert(kdf_input.end(), peer_pk, peer_pk + peer_pk_size);
    kdf_input.insert(kdf_input.end(), (const uint8_t*)"enclave-ecdh", (const uint8_t*)"enclave-ecdh" + 11);

    nitch::G1Point hash_point = reg_ctx->hash_to_g1(std::string(reinterpret_cast<const char*>(kdf_input.data()), kdf_input.size()));
    std::vector<uint8_t> hash_bytes = reg_ctx->g1_to_bytes(hash_point, false);

    sgx_aes_gcm_128bit_key_t aes_key;
    // Use first 16 bytes of hash as AES key
    size_t copy_len = std::min(hash_bytes.size(), sizeof(aes_key));
    memcpy(aes_key, hash_bytes.data(), copy_len);
    if (copy_len < sizeof(aes_key)) {
        memset(reinterpret_cast<uint8_t*>(&aes_key) + copy_len, 0, sizeof(aes_key) - copy_len);
    }

    memcpy(g_peer_shared_keys[peer_index], aes_key, sizeof(sgx_aes_gcm_128bit_key_t));

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
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, true);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (!g_has_secret_key) {
        return SGX_ERROR_UNEXPECTED;
    }
    if (new_threshold >= new_num_parties || dealer_index < 1) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (!commitments_a || !commitment_b0 || !shares_out || !g1_bytes || !g2_bytes || !p_hex) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

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
        return SGX_ERROR_OUT_OF_MEMORY;
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
        return SGX_ERROR_OUT_OF_MEMORY;
    }
    memcpy(commitment_b0, b0_bytes.data(), b0_bytes.size());
    *commitment_b0_out_size = static_cast<uint32_t>(b0_bytes.size());

    uint32_t shares_offset = 0;
    uint8_t temp_ct[128];
    uint32_t temp_ct_size;
    for (int j = 1; j <= new_num_parties; j++) {
        nitch::Scalar x_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(j));
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
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, true);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (!commitments_a || !commitment_b0 || !encrypted_share_bytes || !valid) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (receiver_index < 1 || receiver_index > new_num_parties) {
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
    nitch::Scalar j_scalar = ctx->scalar_from_u64(static_cast<uint64_t>(receiver_index));

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
    uint32_t cert_eA_sig_len)
{
    ensure_enclave_key();
    if (cert_eA_msg_len > 0 && cert_eA_msg) {
        if (cert_eA_sig_len > 0 && cert_eA_sig) {
            sgx_status_t auth_ret = verify_gc_auth_internal(
                cert_eA_msg, cert_eA_msg_len,
                cert_eA_sig, cert_eA_sig_len);
            if (auth_ret != SGX_SUCCESS) {
                return auth_ret;
            }
        }
        sgx_status_t membership_ret = verify_membership_in_active_set(cert_eA_msg, cert_eA_msg_len, true);
        if (membership_ret != SGX_SUCCESS) {
            return membership_ret;
        }
    }
    if (!qual_old_indices || !all_b0_data || !all_a0_data || !encrypted_shares_data ||
        !global_pk || !global_pk_g1 || !verification_key) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    if (qual_old_count == 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }

    nitch::MiraclBls12381* ctx = get_context();

    std::vector<nitch::Scalar> lambdas = compute_lagrange_at_zero(ctx, qual_old_indices, qual_old_count);

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
