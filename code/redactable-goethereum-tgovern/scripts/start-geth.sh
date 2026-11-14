#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

NODE_COUNT="${1:-18}"

need_cmd jq
need_cmd curl
need_executable "$GETH_BIN"
need_file "$ACCOUNTS_FILE"

start_node() {
  local i="$1"
  local datadir="$CHAIN_DIR/node$i"
  local workdir="$GETH_WORK_DIR/node$i"
  local addr rpc_port p2p_port

  addr="$(json_get ".nodes.node$i.address" "$ACCOUNTS_FILE")"
  rpc_port="$(node_rpc_port "$i")"
  p2p_port="$(node_p2p_port "$i")"

  if [ ! -d "$datadir/geth" ]; then
    echo "node$i is not initialized: $datadir" >&2
    exit 1
  fi
  need_file "$workdir/storage.json"
  need_file "$workdir/helper.json"
  need_file "$workdir/password.txt"

  if pgrep -f "$GETH_BIN.*--datadir $datadir" >/dev/null 2>&1; then
    echo "node$i already running"
    return 0
  fi

  local args=(
    "$GETH_BIN"
    --datadir "$datadir"
    --networkid "$NETWORK_ID"
    --port "$p2p_port"
    --rpc
    --rpcport "$rpc_port"
    --rpcaddr "127.0.0.1"
    --rpcvhosts "*"
    --rpccorsdomain "*"
    --rpcapi "eth,net,web3,personal,admin,debug,miner,txpool"
    --ipcpath "$datadir/geth.ipc"
    --allow-insecure-unlock
    --unlock "$addr"
    --password "$workdir/password.txt"
    --nodiscover
    --etherbase "$addr"
    --verbosity 4
  )

  if [ "$i" = "1" ]; then
    args+=(--mine --minerthreads 1)
  fi

  (
    cd "$workdir"
    TGOVERN_COMM_STATS="${TGOVERN_COMM_STATS:-0}" TGOVERN_COMM_NODE="geth-node-$i" \
      nohup "${args[@]}" > "$GETH_LOG_DIR/node$i.log" 2>&1 &
    echo $! > "$PID_DIR/geth-node$i.pid"
  )
  echo "started geth node$i rpc=127.0.0.1:$rpc_port ipc=$datadir/geth.ipc"
}

start_node 1
wait_rpc "http://127.0.0.1:$(node_rpc_port 1)" eth_blockNumber 90

echo "waiting for PermissionManagerV3 deployment from node1..."
contract=""
for _ in $(seq 1 120); do
  contract="$(jq -r '.address // ""' "$GETH_WORK_DIR/node1/storage.json")"
  if [ -n "$contract" ] && [ "$contract" != "null" ]; then
    break
  fi
  sleep 1
done

if [ -z "$contract" ] || [ "$contract" = "null" ]; then
  echo "contract deployment timeout; see $GETH_LOG_DIR/node1.log" >&2
  exit 1
fi

echo "contract=$contract"

for i in $(seq 2 "$NODE_COUNT"); do
  jq --arg contract "$contract" '.address=$contract' \
    "$GETH_WORK_DIR/node$i/storage.json" > "$GETH_WORK_DIR/node$i/storage.tmp.json"
  mv "$GETH_WORK_DIR/node$i/storage.tmp.json" "$GETH_WORK_DIR/node$i/storage.json"
  start_node "$i"
done

echo "geth nodes started"
