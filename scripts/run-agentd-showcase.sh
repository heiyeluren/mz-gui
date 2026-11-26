#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RUN_DIR=${MZGUI_AGENTD_RUN_DIR:-/tmp/mzgui-agentd-showcase}
if [[ "$RUN_DIR" != /* ]]; then
  printf 'MZGUI_AGENTD_RUN_DIR 必须是绝对路径\n' >&2
  exit 2
fi
mkdir -p "$RUN_DIR"
chmod 700 "$RUN_DIR"
APP_DIR=$(bash "$ROOT_DIR/scripts/package-agentd-showcase-darwin.sh")
AGENTD="$APP_DIR/Contents/MacOS/mz-gui-showcase-agentd"
DESKTOP="$APP_DIR/Contents/MacOS/mz-gui-agentd-showcase"

"$AGENTD" -run-dir "$RUN_DIR" &
AGENT_PID=$!
cleanup() {
  kill -TERM "$AGENT_PID" 2>/dev/null || true
  wait "$AGENT_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

MZGUI_AGENTD_RUN_DIR="$RUN_DIR" "$DESKTOP"
