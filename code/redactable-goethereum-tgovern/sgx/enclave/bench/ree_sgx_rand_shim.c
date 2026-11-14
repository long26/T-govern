// REE-side replacement for the enclave's sgx_read_rand().
//
// table_6_2_ree_bench reuses the enclave's NITCH/MIRACL sources verbatim, and
// miracl_bls12381.cpp therefore calls sgx_read_rand() for its random scalar
// seed. Inside the enclave that resolves to the trusted RNG; on the host it
// has no definition. Per docs/T_GOVERN_TABLE_6_2_BENCHMARK.md, the REE
// baseline substitutes Linux getrandom() so the same algorithmic path runs
// without SGX lifecycle or ECALL cost. This shim provides that definition.
//
// It is compiled and linked only into the REE benchmark executable; it is
// never part of the enclave or of libsgx_module.so.
#include <stddef.h>
#include <errno.h>
#include <sys/random.h>

#include "sgx_trts.h"  /* matches the declaration used by miracl_bls12381.cpp */

sgx_status_t sgx_read_rand(unsigned char* random_bytes, size_t length_in_bytes) {
    if (random_bytes == NULL && length_in_bytes > 0) {
        return SGX_ERROR_INVALID_PARAMETER;
    }
    size_t offset = 0;
    while (offset < length_in_bytes) {
        const ssize_t count = getrandom(random_bytes + offset,
                                        length_in_bytes - offset, 0);
        if (count < 0) {
            if (errno == EINTR) continue;
            return SGX_ERROR_UNEXPECTED;
        }
        offset += (size_t)count;
    }
    return SGX_SUCCESS;
}
