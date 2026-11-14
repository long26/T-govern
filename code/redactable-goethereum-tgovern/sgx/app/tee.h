#pragma once
#include <stdint.h>
#include <stdlib.h>

#define ENCLAVE_NAME_SEAL "libenclave_seal.signed.so"
#define ENCLAVE_NAME_UNSEAL "libenclave_unseal.signed.so"
#define ORIGIN_MAC_TEXT "origin_mac_text.txt"
#define ORIGIN_DATA "origin_data.txt"
#define SEALED_DATA_FILE "sealed_data.txt"
#define UNSEALED_MAC_TEXT "unsealed_mac_text.txt"
#define UNSEALED_DECRYPT_DATA "unsealed_decrypt_data.txt"
#define SEALED_KEY_FILE "sealed_key.txt"
#define SEALED_RSA_PRI_KEY_FILE "sealed_rsa_pri_key.txt"
#define KEY_FACTOR_FOLDER "test/key_factor/"
#define DATA_FOLDER "test/data/"
#define EN_DATA "encrypted_data.txt"
#define DE_DATA "decrypted_data.txt"
#define RSA_FACTOR_N "rsa_factor_n.txt"
#define RSA_FACTOR_D "rsa_factor_d.txt"
#define RSA_FACTOR_P "rsa_factor_p.txt"
#define RSA_FACTOR_Q "rsa_factor_q.txt"
#define RSA_FACTOR_DMP1 "rsa_factor_dmp1.txt"
#define RSA_FACTOR_DMQ1 "rsa_factor_dmq1.txt"
#define RSA_FACTOR_IQMP "rsa_factor_iqmp.txt"
#define RSA_FACTOR_FILE "rsa_factor.txt"
#define RSA_ENCRYPTED_DATA_FILE "rsa_encrypted_data.txt"
#define RSA_DECRYPTED_DATA_FILE "rsa_decrypted_data.txt"
#define RSA_SIGNATURE "rsa_signature.txt"

#define SGX_DEAL_MAX_A_BUF    4096
#define SGX_DEAL_MAX_B0_BUF   256
/* Per-peer share slots for DKRS/DistKG create_deal. Worst case is the AES-GCM
 * encrypted branch: 4 (len) + 1 (flag) + 76 (12 IV + 16 MAC + 48 scalar) = 81 B
 * per share. Sized to cover 64 peers plus headroom for future diagnostics. */
#define SGX_DEAL_MAX_SHARES_BUF 8192
#define SGX_MAX_G1_BUF        256
#define SGX_MAX_G2_BUF        256
#define SGX_MAX_P_HEX_BUF     128
#define SGX_MAX_SCALAR_BUF    64

#define log(msg, ...) printf("[APP] " msg "\n", ##__VA_ARGS__)

#ifdef __cplusplus
extern "C" {
#endif
int tee_init(void);
void tee_close(void);

int tee_generate_quote(uint8_t *hardware_id, uint8_t *committee_pubkey, uint8_t *sig_pubkey, uint8_t *vrf_pubkey);
int tee_get_identity(uint8_t *hardware_id, uint8_t *committee_pubkey, uint8_t *sig_pubkey, uint8_t *vrf_pubkey);

int tee_distkg_create_deal(
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
    uint32_t active_set_cert_input_len);

int tee_distkg_verify_share(
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
    uint32_t active_set_cert_input_len);

int tee_distkg_extract(
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
    uint32_t active_set_cert_input_len);

int tee_erase_share(
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
    uint32_t* proof_out_size);

int tee_vrf_eval(
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
    uint32_t* vrf_proof_out_size);

int tee_partial_adapt(
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
    uint32_t* sigma_out_size);

int tee_get_enclave_state(int* has_secret_key, int* stored_index);

/* Table 6.2 benchmark support. Values are aggregate enclave cycle counters
 * for the documented operation ids 0..6. */
int tee_benchmark_noop(void);
int tee_benchmark_reset_stats(void);
int tee_benchmark_get_stats(
    uint32_t operation,
    uint64_t* calls,
    uint64_t* total_cycles,
    uint64_t* min_cycles,
    uint64_t* max_cycles);

int tee_get_enclave_pk(uint8_t* pk, uint32_t pk_buf_size, uint32_t* pk_out_size);

int tee_set_committee_pubkey(const uint8_t* pubkey, uint32_t pubkey_len);

int tee_register_peer_epk(int peer_index, const uint8_t* peer_pk, uint32_t peer_pk_size);

int tee_dkrs_create_deal(
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
    uint32_t active_set_cert_input_len,
    uint32_t* out_st,
    uint32_t* out_retval);

int tee_dkrs_verify_share(
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
    uint32_t active_set_cert_input_len);

int tee_dkrs_extract(
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
    uint32_t active_set_cert_input_len);

#ifdef __cplusplus
}
#endif
