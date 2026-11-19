#!/usr/bin/env bash
# Package the macOS builds into dist/vutty-<version>-macos-<arch>.{dmg,zip}, for Apple silicon (arm64, M1 and later) and Intel (amd64).
# Usage: scripts/package-macos.sh [--build]   (--build runs `make build` first, which makes both)
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="$(cat VERSION)"
OUT="dist"

if [ "${1:-}" = "--build" ]; then
  make build
fi

mkdir -p "$OUT"

package() {
  local arch="$1"
  local app="build/bin/$arch/vutty.app"
  local base="vutty-${VERSION}-macos-${arch}"

  if [ ! -d "$app" ]; then
    echo "error: $app not found (run 'make build' or pass --build)" >&2
    exit 1
  fi
  # A bundle without its executable is "damaged or incomplete" to macOS (and
  # offers no "Open Anyway"), so refuse to package one.
  if [ ! -x "$app/Contents/MacOS/vutty" ]; then
    echo "error: $app/Contents/MacOS/vutty is missing or not executable (run 'make build' or pass --build)" >&2
    exit 1
  fi
  # ...and one built for the other kind of Mac would not start.
  local want="arm64"; [ "$arch" = amd64 ] && want="x86_64"
  if ! lipo -archs "$app/Contents/MacOS/vutty" | grep -qw "$want"; then
    echo "error: $app is not built for $arch" >&2
    exit 1
  fi

  rm -f "$OUT/$base.zip" "$OUT/$base.dmg"

  # ZIP: ditto keeps symlinks, resource forks and signatures intact.
  ditto -c -k --sequesterRsrc --keepParent "$app" "$OUT/$base.zip"

  # DMG: app + /Applications shortcut for drag-to-install.
  local stage
  stage="$(mktemp -d)"
  cp -R "$app" "$stage/"
  cp THIRD_PARTY_NOTICES.md LICENSE "$stage/"
  ln -s /Applications "$stage/Applications"
  hdiutil create -volname "vutty $VERSION ($arch)" -srcfolder "$stage" \
    -fs HFS+ -format UDZO -ov "$OUT/$base.dmg" >/dev/null
  rm -rf "$stage"

  (cd "$OUT" && shasum -a 256 "$base.dmg" "$base.zip" | tee "$base.sha256")
  echo "done: $OUT/$base.dmg, $OUT/$base.zip"
}

package arm64
package amd64
