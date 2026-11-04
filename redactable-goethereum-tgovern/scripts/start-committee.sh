#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

COMMITTEE_COUNT="${1:-3}"

need_executable "$COMMITTEE_BIN"

for i in $(seq 1 "$COMMITTEE_COUNT"); do
  cfg="$COMMITTEE_WORK_DIR/node-$i.json"
  need_file "$cfg"

  if pgrep -f "$COMMITTEE_BIN node --config $cfg" >/dev/null 2>&1; then
    echo "committee node-$i already running"
    continue
  fi

  TGOVERN_COMM_STATS="${TGOVERN_COMM_STATS:-0}" TGOVERN_COMM_NODE="committee-node-$i" \
    nohup "$COMMITTEE_BIN" node --config "$cfg" \
    > "$COMMITTEE_LOG_DIR/node-$i.log" 2>&1 &
  echo $! > "$PID_DIR/committee-node-$i.pid"
  echo "started committee node-$i control=127.0.0.1:$(committee_rpc_port "$i")"
done

echo "committee nodes started"
