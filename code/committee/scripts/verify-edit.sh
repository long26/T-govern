#!/usr/bin/env bash
# verify-edit.sh - Quick verify a redaction result
# Usage: ./verify-edit.sh <rpc_url> <block_number> <tx_index> <expected_hex>
set -euo pipefail

RPC="${1:-http://127.0.0.1:8545}"
BLOCK="$2"
INDEX="$3"
EXPECTED="${4:-}"

if [ -z "$BLOCK" ] || [ -z "$INDEX" ]; then
  echo "Usage: $0 <rpc_url> <block_number> <tx_index> [expected_input_hex]"
  exit 1
fi

BLOCK_HEX="0x$(printf '%x' "$BLOCK")"

echo "Fetching block $BLOCK, tx index $INDEX..."
RESULT=$(curl -sS "$RPC" -H 'Content-Type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBlockByNumber\",\"params\":[\"$BLOCK_HEX\",true]}")

TX_COUNT=$(echo "$RESULT" | python3 -c "import sys,json; b=json.load(sys.stdin)['result']; print(len(b['transactions']) if b else 0)")

if [ "$TX_COUNT" -le "$INDEX" ]; then
  echo "ERROR: block has $TX_COUNT transactions, index $INDEX out of range"
  exit 1
fi

INPUT=$(echo "$RESULT" | python3 -c "import sys,json; b=json.load(sys.stdin)['result']; print(b['transactions'][$INDEX]['input'])")
HASH=$(echo "$RESULT" | python3 -c "import sys,json; b=json.load(sys.stdin)['result']; print(b['transactions'][$INDEX]['hash'])")

echo "  tx hash:  $HASH"
echo "  tx input: $INPUT"

if [ -n "$EXPECTED" ]; then
  if [ "${INPUT,,}" = "${EXPECTED,,}" ]; then
    echo "  ✅ MATCH: input matches expected value"
  else
    echo "  ❌ MISMATCH:"
    echo "    expected: $EXPECTED"
    echo "    actual:   $INPUT"
    exit 1
  fi
fi
