#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SECONDS=${MZGUI_SOAK_SECONDS:-1800}
mkdir -p "$ROOT_DIR/build/soak"

cd "$ROOT_DIR"
go run ./tools/mzguisoak --duration "${SECONDS}s" --report "$ROOT_DIR/build/soak/go-report.json" > "$ROOT_DIR/build/soak/go.log" &
GO_PID=$!
(cd "$ROOT_DIR/framework/ui-kit" && MZGUI_SOAK_SECONDS="$SECONDS" npm run test:e2e -- e2e/soak.spec.ts) > "$ROOT_DIR/build/soak/ui.log" &
UI_PID=$!

cleanup() {
  kill "$GO_PID" "$UI_PID" 2>/dev/null || true
}
trap cleanup EXIT

wait "$GO_PID"
wait "$UI_PID"
printf 'soak 完成：Go=%s UI=%s 秒\n' "$SECONDS" "$SECONDS"
