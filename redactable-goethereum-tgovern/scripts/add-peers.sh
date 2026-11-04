#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

NODE_COUNT="${1:-18}"

need_cmd jq
need_cmd curl

declare -a enodes

normalize_enode() {
  local enode="$1"
  local p2p_port="$2"
  local prefix
  prefix="${enode%%@*}"
  echo "${prefix}@127.0.0.1:${p2p_port}"
}

for i in $(seq 1 "$NODE_COUNT"); do
  rpc_port="$(node_rpc_port "$i")"
  wait_rpc "http://127.0.0.1:$rpc_port" admin_nodeInfo 60
  response="$(rpc_call "http://127.0.0.1:$rpc_port" admin_nodeInfo "[]")"
  enode_raw="$(echo "$response" | jq -r '.result.enode')"
  enodes[$i]="$(normalize_enode "$enode_raw" "$(node_p2p_port "$i")")"
  echo "node$i enode=${enodes[$i]}"
done

id=1
for i in $(seq 1 "$NODE_COUNT"); do
  rpc_port="$(node_rpc_port "$i")"
  for j in $(seq 1 "$NODE_COUNT"); do
    if [ "$i" = "$j" ]; then
      continue
    fi
    payload="$(jq -n --arg enode "${enodes[$j]}" --argjson id "$id" \
      '{jsonrpc:"2.0",method:"admin_addPeer",params:[$enode],id:$id}')"
    response="$(curl -sS "http://127.0.0.1:$rpc_port" -H 'Content-Type: application/json' -d "$payload")"
    echo "node$i add node$j => $response"
    id=$((id + 1))
    sleep 0.05
  done
done

echo "peers connected"
