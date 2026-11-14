# nitchcore

Vendored BLS12-381 crypto core (the `nitch` layer plus the MIRACL core subset it
is built on), extracted from the SGX enclave of the `tee-connect` repository so
it can be linked into the nitch geth fork via cgo. Everything here compiles and
runs natively (Linux, g++) — no SGX SDK required.

## Origin

Vendored from `/root/redactable-goethereum-tee-connect` at commit
`a08b3b92defe343278ab571192be87cff76aae80`:

- `nitch/*.hpp` — from `sgx/enclave/nitch/` (unchanged; `adapt.hpp` and
  `setup.cpp`'s enclave-specific companions were not needed beyond the headers
  listed below).
- `src/{miracl_bls12381,distkg,serialization}.cpp` — from `sgx/enclave/src/`
  (`setup.cpp`, `adapt.cpp`, `enclave.cpp` are enclave-lifecycle code and were
  intentionally NOT vendored).
- `miracl/` — subset of `sgx/lib/miracl/` (see below).

The native-buildability of this exact source set is proven by the tee-connect
target `sgx$ make table-6-2-ree-bench`, a host executable built from the same
three `.cpp` files against the prebuilt `libcore.a`.

## The MIRACL subset

The upstream MIRACL build (`config64.py`) generates per-curve `.cpp` files
(`big_B384_58.cpp`, `fp_BLS12381.cpp`, ...) from the `XXX`/`YYY`/`ZZZ` templates
by textual substitution, compiles them into `core.a`, and then **deletes** them.
The tee-connect tree therefore ships only templates + generated headers + the
prebuilt archive. `miracl/templates/gen_bls12381.py` replays the exact upstream
`curveset()` substitution for BLS12381 (`B384_58`, base 58, `nlen=7`) to
materialize those per-curve sources as real files. Verified: the oracle output
of this vendored build is byte-identical to the same oracle linked against the
original prebuilt `libcore.a`.

Kept (the object list of `libcore.a`, minus provably unrelated families):

- Curve-specific (generated): `big_B384_58.cpp`, `fp_BLS12381.cpp`,
  `fp2_BLS12381.cpp`, `fp4_BLS12381.cpp`, `fp12_BLS12381.cpp`,
  `ecp_BLS12381.cpp`, `ecp2_BLS12381.cpp`, `pair_BLS12381.cpp`.
- Curve ROM data: `rom_curve_BLS12381.cpp`, `rom_field_BLS12381.cpp`.
- Common core: `aes.cpp`, `gcm.cpp`, `hash.cpp`, `hmac.cpp`, `oct.cpp`,
  `rand.cpp`, `randapi.cpp`, `share.cpp`.
- Headers: `arch.h`, `core.h`, `big_B384_58.h`, `config_big_B384_58.h`,
  `config_field_BLS12381.h`, `config_curve_BLS12381.h`, `fp_BLS12381.h`,
  `fp2_BLS12381.h`, `fp4_BLS12381.h`, `fp12_BLS12381.h`, `ecp_BLS12381.h`,
  `ecp2_BLS12381.h`, `pair_BLS12381.h`, `randapi.h`.
- `miracl/templates/` — the upstream `XXX` templates, `config*.h` templates,
  `config64.py`, and `gen_bls12381.py`. NOT compiled (outside the Makefile
  wildcard); kept for provenance and regeneration.

Dropped: other curves' rom files, all `*_BLS1238x/BN*/NIST*/Ed*/C*` variants,
and the algorithm families unused by nitch (`dilithium`, `eddsa`, `ecdh`,
`mpin*`, `bls*` other-curve variants, `blsrev*`, `kyber`, `newhope`, `x509`,
`rsa`, `ff`, `hpke`, `dog`, tests `test*.cpp`, `benchtest_all.cpp`). No `.o`
or `.a` files were vendored.

## Local patches

1. **`src/miracl_bls12381.cpp`**: `#include "sgx_trts.h"` →
   `#include "host_compat.h"`. The shim `src/host_compat.h` provides the two
   SGX trts symbols the file uses (`sgx_status_t`, `sgx_read_rand`) backed by
   Linux `getrandom()`, mirroring the tee-connect REE bench shim
   (`sgx/enclave/bench/sgx_trts.h`).

2. **`miracl/big.cpp` (template) → `big_B384_58.cpp` (generated)**: the three
   function-local `static chunk R=0;` blinding counters in `BIG_cmove`,
   `BIG_cswap`, and `BIG_dcmove` are now `static thread_local chunk R=0;`.
   As plain `static` they are process-global and mutate on every call, a data
   race when multiple cgo threads drive the crypto concurrently. `R` is only a
   blinding value; per-thread counters leave the algorithm's semantics
   unchanged.

## Build

    make -j4          # → libnitchcore.a
    make smoke        # → tools/h1_oracle
    make clean

## The hash oracle

`tools/h1_oracle` prints, for each argv message, the hex of the
uncompressed-kilic (prefix-stripped) H1 point and the H2 scalar:

    ./tools/h1_oracle old new abc

It is used as the cross-implementation equivalence oracle for the Go/cgo port.
