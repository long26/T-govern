#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SGX_SDK="${SGX_SDK:-/opt/intel/sgxsdk}"
GETH_BIN="${GETH_BIN:-$ROOT/bin/geth-sgx}"
if [ ! -x "$GETH_BIN" ] && [ -x "$ROOT/build/bin/geth-sgx" ]; then
  GETH_BIN="$ROOT/build/bin/geth-sgx"
fi
SGX_MODULE="${SGX_MODULE:-$ROOT/sgx/libsgx_module.so}"
SGX_ENCLAVE_PATH="${SGX_ENCLAVE_PATH:-$ROOT/sgx/enclave.signed.so}"

failures=0

ok() {
  printf '[OK] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*"
}

fail() {
  printf '[FAIL] %s\n' "$*"
  failures=$((failures + 1))
}

have_lib() {
  local lib="$1"
  ldconfig -p 2>/dev/null | grep -q "[[:space:]]$lib" \
    || [ -e "$SGX_SDK/lib64/$lib" ] \
    || [ -e "/usr/lib/x86_64-linux-gnu/$lib" ] \
    || [ -e "/usr/lib64/$lib" ]
}

check_file() {
  local path="$1"
  local label="$2"
  if [ -e "$path" ]; then
    ok "$label: $path"
  else
    fail "$label missing: $path"
  fi
}

echo "SGX environment check"
echo "root: $ROOT"
echo "sgx_sdk: $SGX_SDK"
echo

if [ -e /dev/sgx_enclave ]; then
  ok "SGX device: /dev/sgx_enclave"
elif [ -e /dev/sgx ]; then
  ok "SGX device: /dev/sgx"
else
  fail "SGX device missing (/dev/sgx_enclave or /dev/sgx)"
fi

if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active --quiet aesmd 2>/dev/null; then
    ok "AESM service active"
  else
    warn "AESM service is not active according to systemctl"
  fi
elif pgrep -x aesmd >/dev/null 2>&1; then
  ok "AESM daemon running"
else
  warn "cannot confirm AESM daemon status"
fi

for lib in \
  libsgx_urts.so \
  libsgx_dcap_ql.so \
  libsgx_qe3_logic.so \
  libsgx_pce_logic.so \
  libsgx_enclave_common.so; do
  if have_lib "$lib"; then
    ok "library found: $lib"
  else
    fail "library missing: $lib"
  fi
done

check_file "$SGX_ENCLAVE_PATH" "enclave"
check_file "$SGX_MODULE" "sgx module"

if [ -x "$GETH_BIN" ]; then
  ok "geth-sgx binary: $GETH_BIN"
  echo
  echo "geth-sgx SGX linkage:"
  if ldd "$GETH_BIN" | grep -i sgx; then
    if ldd "$GETH_BIN" | grep -q 'libsgx_urts_sim\.so'; then
      fail "geth-sgx links libsgx_urts_sim.so"
    else
      ok "geth-sgx does not link libsgx_urts_sim.so"
    fi
  else
    warn "ldd did not show SGX libraries for geth-sgx"
  fi
else
  fail "geth-sgx binary missing or not executable: $GETH_BIN"
fi

if [ -e "$SGX_MODULE" ]; then
  echo
  echo "libsgx_module.so SGX linkage:"
  if ldd "$SGX_MODULE" | grep -i sgx; then
    if ldd "$SGX_MODULE" | grep -q 'libsgx_urts_sim\.so'; then
      fail "libsgx_module.so links libsgx_urts_sim.so"
    else
      ok "libsgx_module.so does not link libsgx_urts_sim.so"
    fi
  else
    warn "ldd did not show SGX libraries for libsgx_module.so"
  fi
fi

echo
if command -v python3 >/dev/null 2>&1; then
  ok "python3: $(command -v python3)"
  if python3 - <<'PY'
import eth_account
import eth_utils
PY
  then
    ok "python deps: eth_account eth_utils"
  else
    fail "python deps missing: install eth_account and eth_utils"
  fi
else
  fail "python3 missing"
fi

echo
if [ "$failures" -eq 0 ]; then
  ok "SGX environment check passed"
else
  fail "$failures required check(s) failed"
fi

exit "$failures"
