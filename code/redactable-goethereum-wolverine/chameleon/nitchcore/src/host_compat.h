#ifndef NITCHCORE_HOST_COMPAT_H
#define NITCHCORE_HOST_COMPAT_H
// Host-side replacement for the SGX trts surface used by miracl_bls12381.cpp.
// The enclave build uses Intel SGX's sgx_trts.h; this header provides the same
// two symbols backed by Linux getrandom(), mirroring the tee-connect REE bench
// shim (sgx/enclave/bench/sgx_trts.h).
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
#endif
