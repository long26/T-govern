#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

NODE_COUNT="${1:-18}"
COMMITTEE_COUNT="${2:-3}"

echo "geth:"
for i in $(seq 1 "$NODE_COUNT"); do
  port="$(node_rpc_port "$i")"
  if curl -sS "http://127.0.0.1:$port" -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' >/tmp/tgv-status.json 2>/dev/null; then
    block="$(jq -r '.result // "?"' /tmp/tgv-status.json)"
    echo "  node$i rpc=$port block=$block"
  else
    echo "  node$i rpc=$port down"
  fi
done

echo "committee:"
for i in $(seq 1 "$COMMITTEE_COUNT"); do
  port="$(committee_rpc_port "$i")"
  if curl -sS "http://127.0.0.1:$port/rpc" -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"committee_getPublicKey","params":{}}' >/tmp/tgv-status.json 2>/dev/null; then
    ok="$(jq -r 'has("result")' /tmp/tgv-status.json)"
    echo "  node-$i control=$port ok=$ok"
  else
    echo "  node-$i control=$port down"
  fi
done
