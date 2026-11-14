#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

for pidfile in "$PID_DIR"/*.pid; do
  [ -e "$pidfile" ] || continue
  pid="$(cat "$pidfile")"
  if kill -0 "$pid" >/dev/null 2>&1; then
    echo "stopping pid=$pid ($pidfile)"
    kill "$pid" || true
  fi
  rm -f "$pidfile"
done

sleep 2

pkill -f "$GETH_BIN.*--networkid $NETWORK_ID" >/dev/null 2>&1 || true
pkill -f "$COMMITTEE_BIN node --config $COMMITTEE_WORK_DIR" >/dev/null 2>&1 || true

echo "stopped"
