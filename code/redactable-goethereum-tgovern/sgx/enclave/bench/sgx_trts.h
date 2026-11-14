#ifndef T_GOVERN_REE_SGX_TRTS_COMPAT_H
#define T_GOVERN_REE_SGX_TRTS_COMPAT_H

// Host-only compatibility surface required by miracl_bls12381.cpp.  This
// header is intentionally visible only to the Table 6.2 REE build target;
// enclave builds continue to use Intel SGX's real sgx_trts.h.
#include <cerrno>
#include <cstddef>
#include <cstdint>

#include <sys/random.h>

using sgx_status_t = uint32_t;
constexpr sgx_status_t SGX_SUCCESS = 0;
constexpr sgx_status_t SGX_ERROR_UNEXPECTED = 1;

static inline sgx_status_t sgx_read_rand(uint8_t* output, size_t length) {
    size_t offset = 0;
    while (offset < length) {
        const ssize_t result = getrandom(output + offset, length - offset, 0);
        if (result < 0 && errno == EINTR) continue;
        if (result <= 0) return SGX_ERROR_UNEXPECTED;
        offset += static_cast<size_t>(result);
    }
    return SGX_SUCCESS;
}

#endif  // T_GOVERN_REE_SGX_TRTS_COMPAT_H
