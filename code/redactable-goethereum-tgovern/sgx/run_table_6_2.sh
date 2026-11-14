#!/usr/bin/env bash
#
# run_table_6_2.sh — reproduce the Table 6.2 TEE/REE benchmark.
#
# TEE runs in SGX HW mode. The script builds the REE baseline binary, the HW
# enclave + libsgx_module.so, then runs the Go driver (sgx/table_6_2_test.go)
# which measures the empty ECALL (ecall_benchmark_noop) and all seven
# instrumented operations, and prints a unified TEE-vs-REE comparison:
#   REE_ms        algorithm-only host baseline (table_6_2_ree_bench)
#   TEE_alg_ms    in-enclave algorithm time isolated by the RDTSC scope,
#                 converted via an in-run TSC-frequency calibration
#   cyc/op        aggregate enclave RDTSC cycles (min..max)
#   TEE_ecall_ms  end-to-end host wall-clock of the wrapping ECALL
#   overhead      (TEE_alg_ms - REE_ms) / REE_ms
#
# Usage:  bash sgx/run_table_6_2.sh
#
# Offline note: the repo's go.mod currently requires kilic/x/sys versions that
# are not in the local module cache and there is no network here, so the script
# idempotently pins both to the cached, API-compatible versions. With network
# access you can drop the two `go mod edit -replace` lines and `go mod download`.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SGX_DIR="$REPO_ROOT/sgx"
SGX_SDK="${SGX_SDK:-/opt/intel/sgxsdk}"
REE_BIN="$SGX_DIR/enclave/bench/table_6_2_ree_bench"

# Go is not on PATH in this image.
export PATH="/usr/local/go/bin:$PATH"

cd "$REPO_ROOT"

log() { printf '\n\033[1m[%s]\033[0m %s\n' "$1" "$2"; }

# ---- 1. SGX HW environment ----
log "1/5" "Checking SGX HW environment"
if systemctl is-active --quiet aesmd 2>/dev/null; then echo "  aesmd: active"; else echo "  WARN: aesmd not active"; fi
if [ -e /dev/sgx_enclave ]; then echo "  /dev/sgx_enclave: present"; else
    echo "  FAIL: /dev/sgx_enclave missing — this machine has no SGX HW"; exit 1
fi
if [ ! -d "$SGX_SDK" ]; then echo "  FAIL: SGX_SDK not found at $SGX_SDK"; exit 1; fi
command -v go >/dev/null || { echo "  FAIL: go toolchain missing"; exit 1; }

# ---- 2. Offline build setup (idempotent; see header note) ----
log "2/5" "Ensuring offline-compatible go.mod (kilic/x/sys -> cached versions)"
go mod edit -replace=github.com/kilic/bls12-381@v0.1.1-0.20220929213557-ca162e8a70f4=github.com/kilic/bls12-381@v0.1.0
go mod edit -replace=golang.org/x/sys@v0.0.0-20210124154548-22da62e12c0c=golang.org/x/sys@v0.0.0-20201101102859-da207088b7d1

# ---- 3. Build REE bench + HW enclave + libsgx_module.so ----
# Incremental: `make` only recompiles changed sources. Force a from-scratch
# rebuild with REBUILD=1 bash sgx/run_table_6_2.sh (that recompiles secp256k1,
# which is memory-heavy and can trip the OOM killer under a tight cgroup).
log "3/5" "Building REE bench + HW enclave + libsgx_module.so (SGX_MODE=HW)"
if [ "${REBUILD:-0}" = "1" ]; then make -C "$SGX_DIR" clean; fi
make -C "$SGX_DIR" all SGX_MODE=HW
make -C "$SGX_DIR" table-6-2-ree-bench

# sanity: the REE baseline must self-verify its algorithms before timing.
log "3b/5" "REE baseline self-test"
"$REE_BIN" --self-test

# ---- 4. REE baseline (algorithm-only host numbers) ----
log "4/5" "REE baseline timing (n=8, t=4, 100 iters/op)"
"$REE_BIN" 100 | sed 's/^/  /'

# ---- 5. TEE/REE driver (HW): empty ECALL + 7 ops + unified comparison ----
log "5/5" "Table 6.2 TEE/REE driver (HW SGX, go test -tags sgx_hw)"
export PKG_CONFIG_PATH="$SGX_SDK/pkgconfig"
export GOPROXY=off
export CGO_ENABLED=1
export LD_LIBRARY_PATH="$SGX_DIR"

go test -tags sgx_hw -v -run '^TestTable6_2$' -timeout 600s ./sgx/ 2>&1 \
    | grep -vE 'SGX-DIAG|DKRS-DBG' \
    | sed -E 's@^\s*table_6_2_test\.go:[0-9]+:\s*@@' \
    | sed -E 's@^--- (PASS|FAIL).*@@'

echo
echo "Legend: TEE_alg_ms = avg_cycles / in-run_TSC_freq ; overhead = (TEE_alg_ms - REE_ms)/REE_ms."
echo "        TEE_ecall_ms = end-to-end host wall-clock of the wrapping ECALL (incl. ECALL boundary)."
