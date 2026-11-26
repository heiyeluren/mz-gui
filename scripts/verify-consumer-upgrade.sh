#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CURRENT_REF=${MZGUI_CONSUMER_REF:-main}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/mzgui-consumer-upgrade.XXXXXX")
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

for consumer in basic storage; do
  printf '消费者 %s：安装 v0.0.1\n' "$consumer"
  cp -R "$ROOT_DIR/tests/consumers/$consumer" "$WORK_DIR/$consumer"
  cd "$WORK_DIR/$consumer"
  go mod tidy
  go test ./...
  printf '消费者 %s：升级到 %s\n' "$consumer" "$CURRENT_REF"
  go get "github.com/heiyeluren/mz-gui@$CURRENT_REF"
  go test ./...
  go build ./...
done
