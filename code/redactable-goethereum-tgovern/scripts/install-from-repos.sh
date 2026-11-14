#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

ROOT="${TGV_ROOT:-/opt/tgovern}"
GETH_REPO="${GETH_REPO:-/root/redactable-goethereum}"
COMMITTEE_REPO="${COMMITTEE_REPO:-/root/committee-sig/committee-mvp}"

GETH_BIN_SRC="$GETH_REPO/build/bin/geth"
COMMITTEE_BIN_SRC="$COMMITTEE_REPO/build/committee-mvp"
GENESIS_SRC="$GETH_REPO/genesis.json"
HELPER_SRC="$GETH_REPO/helper.json"

need_file() {
  if [ ! -f "$1" ]; then
    echo "missing required file: $1" >&2
    exit 1
  fi
}

need_file "$GETH_BIN_SRC"
need_file "$COMMITTEE_BIN_SRC"
need_file "$GENESIS_SRC"
need_file "$HELPER_SRC"

mkdir -p "$ROOT/bin" "$ROOT/config" "$ROOT/scripts"

cp "$GETH_BIN_SRC" "$ROOT/bin/geth"
cp "$COMMITTEE_BIN_SRC" "$ROOT/bin/committee-mvp"
cp "$GENESIS_SRC" "$ROOT/config/genesis.json"
cp "$HELPER_SRC" "$ROOT/config/helper.json"
for item in "$SCRIPT_DIR"/*; do
  [ "$(basename "$item")" = "__pycache__" ] && continue
  cp -r "$item" "$ROOT/scripts/"
done

chmod +x "$ROOT/bin/geth" "$ROOT/bin/committee-mvp" "$ROOT/scripts"/*

cat <<EOF
installed T-govern runtime package:
  root:      $ROOT
  geth:      $ROOT/bin/geth
  committee: $ROOT/bin/committee-mvp
  scripts:   $ROOT/scripts

next:
  cd $ROOT
  ./scripts/start-all.sh 18 3 2
EOF
