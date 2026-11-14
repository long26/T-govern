#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

COMMITTEE_COUNT="${1:-3}"
THRESHOLD="${2:-2}"

need_cmd jq
need_file "$ACCOUNTS_FILE"

contract="$(jq -r '.address // ""' "$GETH_WORK_DIR/node1/storage.json")"
if [ -z "$contract" ] || [ "$contract" = "null" ]; then
  echo "missing contract address in $GETH_WORK_DIR/node1/storage.json; start geth first" >&2
  exit 1
fi
submitter="$(json_get '.nodes.node1.address' "$ACCOUNTS_FILE")"

python3 - "$COMMITTEE_WORK_DIR" "$CHAIN_DIR" "$contract" "$submitter" "$COMMITTEE_COUNT" "$THRESHOLD" \
  "$BASE_COMMITTEE_P2P_PORT" "$BASE_COMMITTEE_RPC_PORT" <<'PY'
from __future__ import annotations

import json
import sys
from pathlib import Path

out_dir = Path(sys.argv[1])
chain_dir = Path(sys.argv[2])
contract = sys.argv[3]
submitter = sys.argv[4]
count = int(sys.argv[5])
threshold = int(sys.argv[6])
base_p2p = int(sys.argv[7])
base_rpc = int(sys.argv[8])

out_dir.mkdir(parents=True, exist_ok=True)
static_nodes = [f"node-{i}" for i in range(1, count + 1)]
static_addrs = {f"node-{i}": f"127.0.0.1:{base_p2p + i}" for i in range(1, count + 1)}

for i in range(1, count + 1):
    cfg = {
        "node_id": f"node-{i}",
        "listen_addr": f"127.0.0.1:{base_p2p + i}",
        "static_nodes": static_nodes,
        "static_node_addrs": static_addrs,
        "control_addr": f"127.0.0.1:{base_rpc + i}",
        "chain_rpc_mode": "ipc",
        "geth_ipc_path": str(chain_dir / f"node{i}" / "geth.ipc"),
        "vrf_contract_address": contract,
        "vrf_submitter_address": submitter,
        "committee_size": count,
        "threshold": threshold,
        "coordinator_id": "node-1",
        "domain_separation": "committee-sig/mvp/v1",
        "message_version": "v1",
        "attestation_provider": "noop",
    }
    (out_dir / f"node-{i}.json").write_text(json.dumps(cfg, indent=2) + "\n")
    print(f"wrote {out_dir / f'node-{i}.json'}")
PY

echo "committee configs generated"
