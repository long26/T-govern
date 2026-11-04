#!/usr/bin/env bash
# cleanup-e2e.sh - Kill residual geth/committee processes and remove work dir
set -euo pipefail

WORK_DIR="/root/committee-sig/committee-mvp/.tmp/e2e-interactive"

echo "Killing residual geth processes..."
pkill -f 'geth.*--datadir.*e2e-interactive' 2>/dev/null || true

echo "Killing residual committee-mvp processes..."
pkill -f 'committee-mvp.*e2e-interactive' 2>/dev/null || true

sleep 2

if [ -d "$WORK_DIR" ]; then
  echo "Removing work dir: $WORK_DIR"
  rm -rf "$WORK_DIR"
fi

echo "✅ Cleanup done"
