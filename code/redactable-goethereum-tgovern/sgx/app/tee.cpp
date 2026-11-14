#include <fstream>
#include <thread>
#include <iostream>
#include <vector>
#include <cstdlib>

#include "Enclave_u.h"
#include "tee.h"
#include <sgx_urts.h>

#include <stdio.h>
#include <string.h>
#include <assert.h>

#include <vector>
#include <string>
#include <cstring>
#include <cstdint>
#include <chrono>
#include <numeric>
#include <iomanip>
#include <algorithm>

#include "sgx_urts.h"
#include "sgx_report.h"
#include "sgx_dcap_ql_wrapper.h"
#include "sgx_quote_3.h"
#include "sgx_pce.h"
#include "sgx_ql_quote.h"
#include "sgx_dcap_quoteverify.h"
#include "ErrorSupport.h"
#include "sgx_tcrypto.h"

#include "../lib/miracl/bls_BLS12381.h"
#include "../lib/miracl/core.h"

static sgx_enclave_id_t eid = 0;

static std::string resolve_enclave_path()
{
    const char* env_path = std::getenv("SGX_ENCLAVE_PATH");
    if (env_path && env_path[0] != '\0') {
        return std::string(env_path);
    }

    const char* candidates[] = {
        "./sgx/enclave.signed.so",
        "./enclave.signed.so",
        "../sgx/enclave.signed.so",
    };
    for (const char* candidate : candidates) {
        std::ifstream f(candidate);
        if (f.good()) {
            return std::string(candidate);
        }
    }
    return std::string("./sgx/enclave.signed.so");
}

int tee_init(void)
{
    std::string enclave_path = resolve_enclave_path();
    sgx_status_t st = sgx_create_enclave(enclave_path.c_str(), SGX_DEBUG_FLAG, nullptr, nullptr, &eid, nullptr);
    if (st != SGX_SUCCESS) {
        std::cerr << "sgx_create_enclave(" << enclave_path << ") error = 0x" << std::hex << st << std::endl;
        return -1;
    }
    std::cerr << "[SGX-DIAG] sgx_create_enclave path=" << enclave_path << std::endl;
    const char* identity_seed = std::getenv("SGX_SIM_ID_SEED");
    if (identity_seed && identity_seed[0] != '\0') {
        sgx_status_t retval = SGX_SUCCESS;
        st = ecall_set_identity_seed(
            eid,
            &retval,
            reinterpret_cast<const uint8_t*>(identity_seed),
            static_cast<uint32_t>(std::strlen(identity_seed)));
        if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
            std::cerr << "ecall_set_identity_seed error = 0x" << std::hex << st
                      << " retval = 0x" << retval << std::endl;
            sgx_destroy_enclave(eid);
            eid = 0;
            return -1;
        }
    }
    return 0;
}

void tee_close(void) { if (eid) sgx_destroy_enclave(eid); }

void ocall_print_string(const char *str)
{
    printf("%s\n", str);
}

void ocall_print_num(uint32_t *num)
{
    std::cout << *num << std::endl;
}

bool create_app_enclave_report(sgx_target_info_t &qe_target_info,sgx_report_t *app_report, uint8_t *hardware_id, uint32_t hardware_id_len, uint8_t *committee_pubkey, uint32_t committee_pubkey_len, uint8_t *sig_pubkey, uint32_t sig_pubkey_len, uint8_t *vrf_pubkey, uint32_t vrf_pubkey_len) {
    bool ret = true;
    uint32_t retval = 0;
    sgx_status_t sgx_status = SGX_SUCCESS;
    sgx_status = ecall_create_report(eid,
            &retval,
            &qe_target_info,
            app_report,
            hardware_id,
            hardware_id_len,
            committee_pubkey,
            committee_pubkey_len,
            sig_pubkey,
            sig_pubkey_len,
            vrf_pubkey,
            vrf_pubkey_len);    
    if ((SGX_SUCCESS != sgx_status) || (0 != retval)) {
        printf("Error: Call to ecall_create_report() failed\n");
        ret = false;
        goto CLEANUP;
    }
CLEANUP:
    return ret;
}

bool generate_quote(uint8_t *hardware_id, uint8_t *committee_pubkey, uint8_t *sig_pubkey, uint8_t *vrf_pubkey) {
    int ret = 0;
    quote3_error_t qe3_ret = SGX_QL_SUCCESS;
    uint32_t quote_size = 0;
    uint8_t* p_quote_buffer = NULL;
    sgx_quote3_t *p_quote = NULL;
    sgx_target_info_t qe_target_info = { 0 };
    sgx_report_t app_report = { 0 };
    FILE *fptr = NULL;
    qe3_ret = sgx_qe_set_enclave_load_policy(SGX_QL_PERSISTENT);
    if(SGX_QL_SUCCESS != qe3_ret) {
        printf("Error: set enclave load policy error: 0x%04x\n", qe3_ret);
        return -1;
    }

    printf("Step1: Call sgx_qe_get_target_info:\n");
    qe3_ret = sgx_qe_get_target_info(&qe_target_info);
    if (SGX_QL_SUCCESS != qe3_ret) {
        printf("Error in sgx_qe_get_target_info. 0x%04x\n", qe3_ret);
        return -1;
    }

    printf("Step2: Call create_app_report\n");
    if(true != create_app_enclave_report(qe_target_info, &app_report, hardware_id, 32, committee_pubkey, 128, sig_pubkey, 32, vrf_pubkey, 32)) {
        printf("Info: Call to ecall_create_report() failed\n");
        return -1;
    }

    fptr = fopen("report.dat","wb");
    if( fptr ) {
        fwrite(&app_report, sizeof(app_report), 1, fptr);
        fclose(fptr);
    }

    printf("Step3: Call sgx_qe_get_quote_size\n");
    qe3_ret = sgx_qe_get_quote_size(&quote_size);
    if (SGX_QL_SUCCESS != qe3_ret) {
        printf("Error: sgx_qe_get_quote_size error 0x%04x\n", qe3_ret);
        return -1;
    }

    p_quote_buffer = (uint8_t*)malloc(quote_size);
    if (NULL == p_quote_buffer) {
        printf("Info: Couldn't allocate quote_buffer\n");
        return -1;
    }
    memset(p_quote_buffer, 0, quote_size);

    printf("Step4: Call sgx_qe_get_quote\n");
    qe3_ret = sgx_qe_get_quote(&app_report,
        quote_size,
        p_quote_buffer);
    if (SGX_QL_SUCCESS != qe3_ret) {
        printf("Error: sgx_qe_get_quote got error 0x%04x\n", qe3_ret);
        ret = -1;
        goto CLEANUP;
    }

    p_quote = (sgx_quote3_t*)p_quote_buffer;

    fptr = fopen("quote.dat","wb");
    if(fptr) {
        fwrite(p_quote, quote_size, 1, fptr);
        fclose(fptr);
    }

    printf("Info: Clean up the enclave load policy\n");
    qe3_ret = sgx_qe_cleanup_by_policy();
    if(SGX_QL_SUCCESS != qe3_ret) {
        printf("Error: cleanup enclave load policy with error 0x%04x\n", qe3_ret);
        ret = -1;
    }
CLEANUP:
    if (NULL != p_quote_buffer) {
        free(p_quote_buffer);
    }
    return ret;
}

int tee_generate_quote(uint8_t *hardware_id, uint8_t *committee_pubkey, uint8_t *sig_pubkey, uint8_t *vrf_pubkey) {
    if (generate_quote(hardware_id, committee_pubkey, sig_pubkey, vrf_pubkey) == 0) {
        std::cout << "Generate quote succeeded." << std::endl;
        return 0;
    } else {
        std::cerr << "Failed to generate quote." << std::endl;
        return -1;
    }
}

int tee_get_identity(uint8_t *hardware_id, uint8_t *committee_pubkey, uint8_t *sig_pubkey, uint8_t *vrf_pubkey) {
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_get_identity(
        eid,
        &retval,
        hardware_id, 32,
        committee_pubkey, 128,
        sig_pubkey, 32,
        vrf_pubkey, 32);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_get_identity error = 0x" << std::hex << st << " retval = 0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t active_set_cert_input_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_distkg_create_deal(
        eid, &retval,
        dealer_index, threshold, num_parties,
        commitments_a, commitments_a_buf_size, commitments_a_out_size,
        commitment_b0, commitment_b0_buf_size, commitment_b0_out_size,
        shares_out, shares_buf_size, shares_out_size,
        g1_bytes, g1_buf_size,
        g2_bytes, g2_buf_size,
        p_hex, p_hex_buf_size,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_distkg_create_deal failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t active_set_cert_input_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_distkg_verify_share(
        eid, &retval,
        receiver_index, dealer_index, threshold, num_parties,
        commitments_a, commitments_a_size,
        commitment_b0, commitment_b0_size,
        encrypted_share_bytes, encrypted_share_bytes_size,
        valid,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_distkg_verify_share failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t active_set_cert_input_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_distkg_extract(
        eid, &retval,
        my_index, threshold, num_parties,
        qual_indices, qual_count,
        all_b0_data, all_b0_data_size,
        b0_dealer_map, b0_dealer_map_count,
        all_a0_data, all_a0_data_size,
        a0_dealer_map, a0_dealer_map_count,
        encrypted_shares_data, encrypted_shares_data_size,
        shares_from_map, shares_from_map_count,
        global_pk, global_pk_buf_size, global_pk_out_size,
        global_pk_g1, global_pk_g1_buf_size, global_pk_g1_out_size,
        verification_key, vk_buf_size, vk_out_size,
        pkch_chain_id, pkch_chain_id_len,
        pkch_contract_address, pkch_contract_address_len,
        pkch_tee_owner, pkch_tee_owner_len,
        pkch_signature, pkch_signature_buf_size, pkch_signature_out_size,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_distkg_extract failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t* proof_out_size)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_erase_share(
        eid, &retval,
        epoch, node_index,
        chain_id, chain_id_len,
        contract_address, contract_address_len,
        tee_owner, tee_owner_len,
        erase_proof_hash_out, erase_proof_hash_buf_size,
        proof_out, proof_buf_size, proof_out_size);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_erase_share failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t* vrf_proof_out_size)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_vrf_eval(
        eid, &retval,
        chain_id, chain_id_len,
        contract_address, contract_address_len,
        epoch,
        hardware_id, hardware_id_len,
        vrf_pubkey, vrf_pubkey_len,
        seed, seed_len,
        vrf_result_out, vrf_result_buf_size,
        vrf_proof_out, vrf_proof_buf_size,
        vrf_proof_out_size);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_vrf_eval failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t* sigma_out_size)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_partial_adapt(
        eid, &retval,
        source_msg, source_msg_len,
        target_msg, target_msg_len,
        cert_req_auth_msg, cert_req_auth_msg_len,
        cert_req_auth_sig, cert_req_auth_sig_len,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        d_req, d_req_len,
        d_policy, d_policy_len,
        committee_pub_key_version, cpkv_len,
        expires_at, expires_at_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len,
        d_req_input, d_req_input_len,
        contribution_out, contribution_buf_size, contribution_out_size,
        proof_challenge_out, proof_challenge_buf_size,
        proof_response_out, proof_response_buf_size,
        y_out, y_buf_size, y_out_size,
        sigma_out, sigma_buf_size, sigma_out_size);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_partial_adapt failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

int tee_get_enclave_state(int* has_secret_key, int* stored_index)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_get_enclave_state(eid, &retval, has_secret_key, stored_index);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_get_enclave_state failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

int tee_benchmark_noop(void)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    const sgx_status_t st = ecall_benchmark_noop(eid, &retval);
    return (st == SGX_SUCCESS && retval == SGX_SUCCESS) ? 0 : -1;
}

int tee_benchmark_reset_stats(void)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    const sgx_status_t st = ecall_benchmark_reset_stats(eid, &retval);
    return (st == SGX_SUCCESS && retval == SGX_SUCCESS) ? 0 : -1;
}

int tee_benchmark_get_stats(
    uint32_t operation,
    uint64_t* calls,
    uint64_t* total_cycles,
    uint64_t* min_cycles,
    uint64_t* max_cycles)
{
    if (eid == 0 || !calls || !total_cycles || !min_cycles || !max_cycles) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    const sgx_status_t st = ecall_benchmark_get_stats(
        eid, &retval, operation, calls, total_cycles, min_cycles, max_cycles);
    return (st == SGX_SUCCESS && retval == SGX_SUCCESS) ? 0 : -1;
}

int tee_get_enclave_pk(uint8_t* pk, uint32_t pk_buf_size, uint32_t* pk_out_size)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_get_enclave_pk(eid, &retval, pk, pk_buf_size, pk_out_size);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_get_enclave_pk failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

int tee_set_committee_pubkey(const uint8_t* pubkey, uint32_t pubkey_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_set_committee_pubkey(eid, &retval, pubkey, pubkey_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_set_committee_pubkey failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

int tee_register_peer_epk(int peer_index, const uint8_t* peer_pk, uint32_t peer_pk_size)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_register_peer_epk(eid, &retval, peer_index, peer_pk, peer_pk_size);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_register_peer_epk failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t* out_retval)
{
    if (out_st) *out_st = SGX_SUCCESS;
    if (out_retval) *out_retval = SGX_SUCCESS;
    if (eid == 0) {
        if (out_st) *out_st = SGX_ERROR_INVALID_ENCLAVE_ID;
        return -1;
    }
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_dkrs_create_deal(
        eid, &retval,
        dealer_index, new_threshold, new_num_parties,
        commitments_a, commitments_a_buf_size, commitments_a_out_size,
        commitment_b0, commitment_b0_buf_size, commitment_b0_out_size,
        shares_out, shares_buf_size, shares_out_size,
        g1_bytes, g1_buf_size,
        g2_bytes, g2_buf_size,
        p_hex, p_hex_buf_size,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (out_st) *out_st = static_cast<uint32_t>(st);
    if (out_retval) *out_retval = static_cast<uint32_t>(retval);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_dkrs_create_deal failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t active_set_cert_input_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_dkrs_verify_share(
        eid, &retval,
        receiver_index, dealer_index, new_threshold, new_num_parties,
        commitments_a, commitments_a_size,
        commitment_b0, commitment_b0_size,
        encrypted_share_bytes, encrypted_share_bytes_size,
        valid,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_dkrs_verify_share failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}

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
    uint32_t active_set_cert_input_len)
{
    if (eid == 0) return -1;
    sgx_status_t retval = SGX_SUCCESS;
    sgx_status_t st = ecall_dkrs_extract(
        eid, &retval,
        my_index, new_threshold, new_num_parties, old_num_parties,
        qual_old_indices, qual_old_count,
        all_b0_data, all_b0_data_size,
        b0_dealer_map, b0_dealer_map_count,
        all_a0_data, all_a0_data_size,
        a0_dealer_map, a0_dealer_map_count,
        encrypted_shares_data, encrypted_shares_data_size,
        shares_from_map, shares_from_map_count,
        global_pk, global_pk_buf_size, global_pk_out_size,
        global_pk_g1, global_pk_g1_buf_size, global_pk_g1_out_size,
        verification_key, vk_buf_size, vk_out_size,
        cert_eA_msg, cert_eA_msg_len,
        cert_eA_sig, cert_eA_sig_len,
        active_tee_list, active_tee_count,
        active_set_cert_input, active_set_cert_input_len);
    if (st != SGX_SUCCESS || retval != SGX_SUCCESS) {
        std::cerr << "ecall_dkrs_extract failed: st=0x" << std::hex << st
                  << " retval=0x" << retval << std::endl;
        return -1;
    }
    return 0;
}
