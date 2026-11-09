#!/usr/bin/env bash
# Heavy-output benchmark of the terminal in the real macOS WKWebView (docs/evaluation/assessment.md G4, Known issues 6).
# The page is the app's own terminal set-up (xterm.js 5.5, DOM renderer, 10000 lines of scrollback, the output highlighter, the
# same base64 -> bytes -> term.write path as a term:data event); a small native Swift app feeds it chunks through
# evaluateJavaScript, like Wails does. "paced" = one chunk of up to 64 KB every 16 ms (what the backend's batcher sends at most when
# it flushes on its timer); "flood" = the next chunk as soon as the last was delivered (no pacing, no flow control).
# "flow" = flood with flow control (what the app does since the Gate: no more is sent while 512 KB is sent but not parsed).
# ONLY="dom 1" limits it to one renderer/highlighter setting.
# MODES="flood flow" limits the modes (default: paced flood flow).
# Usage: tools/termperf/run.sh [MB per scenario, default 24]   (macOS only; takes about 10 minutes at 24 MB)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HERE="$ROOT/tools/termperf"
MB="${1:-24}"
WORK="$(mktemp -d)"
cd "$ROOT/frontend" && [ -d node_modules ] || npm ci --no-audit --no-fund
# the WebGL renderer is not part of the app: it is installed here only to compare with the DOM renderer
(cd "$WORK" && npm init -y >/dev/null && npm i --no-audit --no-fund @xterm/addon-webgl@0.18.0 >/dev/null 2>&1)
cp "$HERE/index.html" "$WORK/"
cp "$ROOT/frontend/node_modules/@xterm/xterm/css/xterm.css" "$WORK/out.css"
NODE_PATH="$WORK/node_modules" "$ROOT/frontend/node_modules/.bin/esbuild" "$HERE/harness.ts" --bundle \
  --outfile="$WORK/out.js" --define:process.env.NODE_ENV='"production"' --log-level=warning
swiftc -O "$HERE/runner.swift" -o "$WORK/runner"
SC=plain,log,ansi,unicode,fullscreen
for mode in ${MODES:-paced flood flow}; do
  for cfg in "dom 1" "dom 0" "webgl 1"; do
    [ -z "${ONLY:-}" ] || [ "$cfg" = "$ONLY" ] || continue
    set -- $cfg
    "$WORK/runner" "$WORK" "$1" "$2" "$MB" "$mode" "$SC" 2>&1 | grep -E "hl=|error"
  done
done
