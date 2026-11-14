#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

NODE_COUNT="${1:-18}"
COMMITTEE_COUNT="${2:-3}"
THRESHOLD="${3:-2}"

"$SCRIPT_DIR/init-chain.sh" "$NODE_COUNT"
"$SCRIPT_DIR/start-geth.sh" "$NODE_COUNT"
"$SCRIPT_DIR/add-peers.sh" "$NODE_COUNT"
"$SCRIPT_DIR/gen-committee-configs.sh" "$COMMITTEE_COUNT" "$THRESHOLD"
"$SCRIPT_DIR/start-committee.sh" "$COMMITTEE_COUNT"
sleep 3
"$SCRIPT_DIR/register-committee-pubkey.sh"

echo "all services started"
