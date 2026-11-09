#!/usr/bin/env bash
# Screenshots of the features the numbered walkthrough does not cover (see guide-shots.mjs) into docs/assets/screenshots/guide.
# Like update-shots.sh: a fresh data directory, wails dev in the browser, and the demo servers on ports 2203 and 2204.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
export PATH="$PATH:$(go env GOPATH)/bin"
trap 'kill ${WAILS_PID:-0} 2>/dev/null || true' EXIT

free_port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
for p in 2203 2204; do
  if lsof -nP -iTCP:$p -sTCP:LISTEN >/dev/null 2>&1; then echo "port $p is busy: the demo servers need 2203 and 2204." >&2; exit 1; fi
done

cd "$ROOT"
go build -o "$WORK/demoserver" ./cmd/demoserver
mkdir -p "$WORK/data" "$WORK/shots"
cat > "$WORK/aws.json" <<'JSON'
{"Reservations":[{"Instances":[
 {"InstanceId":"i-0a1b2c3d4e5f60001","PublicIpAddress":"203.0.113.10","PrivateIpAddress":"10.0.1.10","KeyName":"prod-key","State":{"Name":"running"},"Tags":[{"Key":"Name","Value":"api-1"},{"Key":"Environment","Value":"prod"},{"Key":"team","Value":"core"}]},
 {"InstanceId":"i-0a1b2c3d4e5f60002","PublicIpAddress":"203.0.113.11","PrivateIpAddress":"10.0.1.11","KeyName":"prod-key","State":{"Name":"running"},"Tags":[{"Key":"Name","Value":"api-2"},{"Key":"Environment","Value":"prod"},{"Key":"team","Value":"core"}]},
 {"InstanceId":"i-0a1b2c3d4e5f60003","PrivateIpAddress":"10.0.2.20","KeyName":"stg-key","State":{"Name":"running"},"Tags":[{"Key":"Name","Value":"worker-stg"},{"Key":"Environment","Value":"staging"}]},
 {"InstanceId":"i-0a1b2c3d4e5f60004","PublicIpAddress":"203.0.113.30","State":{"Name":"stopped"},"Tags":[{"Key":"Name","Value":"old-batch"}]}
]}]}
JSON

PORT="$(free_port)"
URL="http://127.0.0.1:$PORT"
# `wails dev -browser` opens the default browser at the dev server, and its unlock screen makes macOS turn on secure input for every
# app. `open` and `xdg-open` do nothing for the wails process.
mkdir -p "$WORK/noopen"
for c in open xdg-open; do printf '#!/bin/sh\nexit 0\n' > "$WORK/noopen/$c"; chmod +x "$WORK/noopen/$c"; done
PATH="$WORK/noopen:$PATH" VUTTY_NO_NATIVE_DIALOGS=1 VUTTY_DATA_DIR="$WORK/data" wails dev -browser -devserver "127.0.0.1:$PORT" -skipbindings -loglevel error > "$WORK/wails.log" 2>&1 &
WAILS_PID=$!
UP=0
for _ in $(seq 1 90); do curl -s -o /dev/null -m 2 "$URL" && UP=1 && break; sleep 2; done
[ "$UP" = 1 ] || { echo "the dev server did not start; see $WORK/wails.log" >&2; exit 1; }

cd "$ROOT/e2e"
[ -d node_modules ] || npm install --no-audit --no-fund
npx playwright install chromium >/dev/null
VUTTY_URL="$URL" DEMOSERVER="$WORK/demoserver" LISTING="$WORK/aws.json" SHOT_DIR="$WORK/shots" node guide-shots.mjs || { echo "kept in $WORK/shots (FAILURE.png)" >&2; trap - EXIT; kill $WAILS_PID 2>/dev/null || true; exit 1; }
mkdir -p "$ROOT/docs/assets/screenshots/guide"
cp "$WORK"/shots/*.png "$ROOT/docs/assets/screenshots/guide/"
echo "screenshots copied to docs/assets/screenshots/guide"
