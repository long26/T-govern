#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SGX_SDK="${SGX_SDK:-/opt/intel/sgxsdk}"
LOAD_DATADIR="${LOAD_DATADIR:-$ROOT_DIR/.tmp/sgx-load}"
LOAD_LOG="${LOAD_LOG:-$ROOT_DIR/.tmp/sgx-load.log}"
RPC_PORT="${RPC_PORT:-18545}"
P2P_PORT="${P2P_PORT:-30399}"

if [ ! -f "$SGX_SDK/environment" ]; then
  echo "missing SGX SDK environment: $SGX_SDK/environment" >&2
  exit 1
fi

source "$SGX_SDK/environment"

cd "$ROOT_DIR"

echo "== build gate =="
make geth-sgx SGX_MODE=SIM SGX_SDK="$SGX_SDK"
test -x "$ROOT_DIR/build/bin/geth-sgx"

echo "== load gate =="
mkdir -p "$LOAD_DATADIR"
set +e
timeout 25s "$ROOT_DIR/build/bin/geth-sgx" \
  --datadir "$LOAD_DATADIR" \
  --networkid 739191 \
  --nodiscover \
  --maxpeers 0 \
  --nousb \
  --rpc \
  --rpcaddr 127.0.0.1 \
  --rpcport "$RPC_PORT" \
  --port "$P2P_PORT" \
  --teehost 2>&1 | tee "$LOAD_LOG"
status=${PIPESTATUS[0]}
set -e
if [ "$status" -ne 0 ] && [ "$status" -ne 124 ]; then
  echo "geth-sgx load gate failed with status $status" >&2
  exit "$status"
fi
grep -q "SGX TEE backend initialized" "$LOAD_LOG"
grep -Eq "hardwareID=[0-9a-fA-F]{64}" "$LOAD_LOG"

echo "== cross-lib encoding gate =="
go test -tags sgx -run TestPartialAdaptContributionCrossLib -v ./sgx/

echo "SGX SIM build/load/cross-lib gates passed"
