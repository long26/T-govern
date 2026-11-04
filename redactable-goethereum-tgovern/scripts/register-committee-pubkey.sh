#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

need_cmd jq
need_cmd curl

committee_url="http://127.0.0.1:$(committee_rpc_port 1)/rpc"
geth_url="http://127.0.0.1:$(node_rpc_port 1)"

wait_rpc "$geth_url" eth_blockNumber 60

echo "query committee public key..."
pubkey="$(curl -sS "$committee_url" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"committee_getPublicKey","params":{}}' \
  | jq -r '.result.committee_pub_key // empty')"

if [ -z "$pubkey" ]; then
  echo "failed to query committee pubkey; see $COMMITTEE_LOG_DIR/node-1.log" >&2
  exit 1
fi

payload="$(jq -n --arg p "0x${pubkey#0x}" \
  '{jsonrpc:"2.0",id:1,method:"eth_registerCommitteePubKey",params:[$p]}')"

echo "register committee pubkey on geth..."
curl -sS "$geth_url" -H 'Content-Type: application/json' -d "$payload"
echo
