#ifndef SECP256K1_SGX_COMPAT_H
#define SECP256K1_SGX_COMPAT_H

/* No-op printf for secp256k1 debug output in SGX enclave */
#define printf(...) ((void)0)
#define fprintf(...) ((int)0)
#define stderr ((void*)0)

#endif
