#!/bin/bash
# Wait until every committee member has extracted its share and every node has
# observed enough public DistKG material to validate NITCH transactions.
# Usage: ./wait_dkg_ready.sh <committee-size> <total-nodes> [timeout-seconds]
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    echo "Usage: $0 <committee-size> <total-nodes> [timeout-seconds]" >&2
    exit 1
fi

COMMITTEE_SIZE=$1
TOTAL_NODES=$2
TIMEOUT_SECONDS=${3:-180}
BASE_RPC_PORT=8545
START_TIME=$(date +%s)

if [ "$COMMITTEE_SIZE" -lt 1 ] || [ "$COMMITTEE_SIZE" -gt "$TOTAL_NODES" ]; then
    echo "committee-size must be within 1..total-nodes" >&2
    exit 1
fi

rpc_status() {
    local port=$1
    curl --silent --show-error --fail \
        -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_dkgStatus","params":[]}' \
        "http://127.0.0.1:${port}"
}

while true; do
    committee_ready=true
    public_key_ready=true

    for ((node=1; node<=TOTAL_NODES; node++)); do
        port=$((BASE_RPC_PORT + node - 1))
        status=$(rpc_status "$port" 2>/dev/null || true)
        if [[ "$status" != *'"publicKeyReady":true'* ]]; then
            public_key_ready=false
        fi
        if [ "$node" -le "$COMMITTEE_SIZE" ] && [[ "$status" != *'"extracted":true'* ]]; then
            committee_ready=false
        fi
    done

    if [ "$committee_ready" = true ] && [ "$public_key_ready" = true ]; then
        echo "DKG ready: ${COMMITTEE_SIZE} committee nodes extracted; ${TOTAL_NODES} nodes have the public key."
        exit 0
    fi
    if [ $(( $(date +%s) - START_TIME )) -ge "$TIMEOUT_SECONDS" ]; then
        echo "Timed out waiting for DKG: committee_extracted=${committee_ready}, public_key_ready=${public_key_ready}" >&2
        exit 1
    fi
    sleep 1
done
