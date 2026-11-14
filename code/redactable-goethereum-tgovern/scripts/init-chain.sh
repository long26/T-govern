#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

NODE_COUNT="${1:-18}"

need_cmd jq
need_cmd python3
need_executable "$GETH_BIN"
need_file "$GENESIS_FILE"
need_file "$HELPER_FILE"

if [ ! -f "$ACCOUNTS_FILE" ]; then
  echo "accounts file not found; generating: $ACCOUNTS_FILE"
  python3 "$SCRIPT_DIR/gen-accounts.py" -n "$NODE_COUNT" -o "$ACCOUNTS_FILE"
fi

echo "initializing local chain: nodes=$NODE_COUNT root=$ROOT"

cp "$GENESIS_FILE" "$CHAIN_DIR/genesis.json"
jq '.alloc = {}' "$CHAIN_DIR/genesis.json" > "$CHAIN_DIR/genesis.tmp.json"
mv "$CHAIN_DIR/genesis.tmp.json" "$CHAIN_DIR/genesis.json"

for i in $(seq 1 "$NODE_COUNT"); do
  addr="$(json_get ".nodes.node$i.address" "$ACCOUNTS_FILE")"
  if [ -z "$addr" ] || [ "$addr" = "null" ]; then
    echo "missing account for node$i in $ACCOUNTS_FILE" >&2
    exit 1
  fi
  jq --arg addr "${addr#0x}" \
    '.alloc[$addr] = {"balance":"1000000000000000000000"}' \
    "$CHAIN_DIR/genesis.json" > "$CHAIN_DIR/genesis.tmp.json"
  mv "$CHAIN_DIR/genesis.tmp.json" "$CHAIN_DIR/genesis.json"
done

for i in $(seq 1 "$NODE_COUNT"); do
  datadir="$CHAIN_DIR/node$i"
  workdir="$GETH_WORK_DIR/node$i"
  mkdir -p "$datadir" "$workdir"

  pk="$(json_get ".nodes.node$i.privateKey" "$ACCOUNTS_FILE")"
  rpc_port="$(node_rpc_port "$i")"

  printf '%s\n' "$pk" > "$workdir/privatekey.txt"
  printf '%s\n' "$PASSWORD" > "$workdir/password.txt"
  cp "$HELPER_FILE" "$workdir/helper.json"
  jq -n --arg pk "$pk" --arg port "$rpc_port" \
    '{address:"", privateKey:$pk, port:$port}' > "$workdir/storage.json"

  "$GETH_BIN" --datadir "$datadir" account import \
    --password "$workdir/password.txt" "$workdir/privatekey.txt" >/dev/null 2>&1 || true

  # The modified geth genesis path reads helper.json from process cwd.
  (cd "$CONFIG_DIR" && "$GETH_BIN" --datadir "$datadir" init "$CHAIN_DIR/genesis.json" >/dev/null)
  echo "initialized node$i datadir=$datadir"
done

echo "done"
