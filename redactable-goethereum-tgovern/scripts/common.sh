#!/usr/bin/env bash
set -euo pipefail

ROOT="${TGV_ROOT:-/opt/tgovern}"
BIN_DIR="$ROOT/bin"
CONFIG_DIR="$ROOT/config"
CHAIN_DIR="$ROOT/chain"
WORK_DIR="$ROOT/work"
GETH_WORK_DIR="$WORK_DIR/geth"
COMMITTEE_WORK_DIR="$WORK_DIR/committee"
LOG_DIR="$ROOT/logs"
GETH_LOG_DIR="$LOG_DIR/geth"
COMMITTEE_LOG_DIR="$LOG_DIR/committee"
PID_DIR="$ROOT/run"

GETH_BIN="${TGV_GETH_BIN:-$BIN_DIR/geth}"
COMMITTEE_BIN="${TGV_COMMITTEE_BIN:-$BIN_DIR/committee-mvp}"
GENESIS_FILE="${TGV_GENESIS_FILE:-$CONFIG_DIR/genesis.json}"
HELPER_FILE="${TGV_HELPER_FILE:-$CONFIG_DIR/helper.json}"
ACCOUNTS_FILE="${TGV_ACCOUNTS_FILE:-$CONFIG_DIR/all_privatekeys.json}"

NETWORK_ID="${TGV_NETWORK_ID:-12345}"
PASSWORD="${TGV_PASSWORD:-1}"
BASE_P2P_PORT="${TGV_BASE_P2P_PORT:-30300}"
BASE_RPC_PORT="${TGV_BASE_RPC_PORT:-8544}"
BASE_COMMITTEE_P2P_PORT="${TGV_BASE_COMMITTEE_P2P_PORT:-3400}"
BASE_COMMITTEE_RPC_PORT="${TGV_BASE_COMMITTEE_RPC_PORT:-4400}"

mkdir -p "$BIN_DIR" "$CONFIG_DIR" "$CHAIN_DIR" "$GETH_WORK_DIR" \
  "$COMMITTEE_WORK_DIR" "$GETH_LOG_DIR" "$COMMITTEE_LOG_DIR" "$PID_DIR"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_file() {
  if [ ! -f "$1" ]; then
    echo "missing required file: $1" >&2
    exit 1
  fi
}

need_executable() {
  if [ ! -x "$1" ]; then
    echo "missing executable: $1" >&2
    exit 1
  fi
}

node_rpc_port() {
  local index="$1"
  echo $((BASE_RPC_PORT + index))
}

node_p2p_port() {
  local index="$1"
  echo $((BASE_P2P_PORT + index))
}

committee_p2p_port() {
  local index="$1"
  echo $((BASE_COMMITTEE_P2P_PORT + index))
}

committee_rpc_port() {
  local index="$1"
  echo $((BASE_COMMITTEE_RPC_PORT + index))
}

rpc_call() {
  local url="$1"
  local method="$2"
  local params="${3:-[]}"
  curl -sS "$url" \
    -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}"
}

wait_rpc() {
  local url="$1"
  local method="${2:-eth_blockNumber}"
  local timeout="${3:-60}"
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if rpc_call "$url" "$method" "[]" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  echo "RPC not ready after ${timeout}s: $url" >&2
  return 1
}

json_get() {
  jq -r "$1" "$2"
}
