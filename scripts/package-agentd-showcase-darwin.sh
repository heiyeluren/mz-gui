#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP_DIR=${1:-"$ROOT_DIR/build/MZ-GUI-Agentd-Showcase.app"}
if [[ "$APP_DIR" != /* ]]; then
  APP_DIR="$ROOT_DIR/$APP_DIR"
fi

mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
cd "$ROOT_DIR"
go build -o "$APP_DIR/Contents/MacOS/mz-gui-agentd-showcase" ./examples/framework-agentd-showcase
go build -o "$APP_DIR/Contents/MacOS/mz-gui-showcase-agentd" ./examples/framework-agentd-showcase/agentd

cat > "$APP_DIR/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key><string>黑夜路人的 APP · 多进程</string>
  <key>CFBundleName</key><string>黑夜路人的 APP · 多进程</string>
  <key>CFBundleExecutable</key><string>mz-gui-agentd-showcase</string>
  <key>CFBundleIdentifier</key><string>com.mzgui.framework.agentdshowcase</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.0.2</string>
  <key>CFBundleVersion</key><string>2</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

printf '%s\n' "$APP_DIR"
