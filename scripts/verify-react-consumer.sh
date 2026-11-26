#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/mzgui-react-consumer.XXXXXX")
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

cd "$ROOT_DIR/framework/ui-kit"
npm pack --pack-destination "$WORK_DIR"
cp -R "$ROOT_DIR/tests/consumers/react" "$WORK_DIR/consumer"
cd "$WORK_DIR/consumer"
npm install "$WORK_DIR"/mz-gui-ui-kit-*.tgz
npm run typecheck
npx playwright install chromium
npm test
npm run build
