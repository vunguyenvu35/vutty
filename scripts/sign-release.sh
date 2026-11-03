#!/usr/bin/env bash
# Sign the checksum list of a published release, so installed copies of Vutty can update themselves to it.
# Usage: scripts/sign-release.sh v1.3.0 [key file]      (the key defaults to ~/.config/vutty/update-signing.key)
# It downloads SHA256SUMS of the release, signs it, checks the signature against the public key built into the app, and uploads
# SHA256SUMS.sig next to it. Needs the GitHub CLI (gh), signed in to the repository.
set -euo pipefail
cd "$(dirname "$0")/.."

TAG="${1:?usage: scripts/sign-release.sh <tag> [key file]}"
KEY="${2:-$HOME/.config/vutty/update-signing.key}"
[ -f "$KEY" ] || { echo "error: no signing key at $KEY (make one with: go run ./tools/updatesign keygen <file>)" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

gh release download "$TAG" --pattern SHA256SUMS --dir "$WORK"
go run ./tools/updatesign sign "$KEY" "$WORK/SHA256SUMS"
PUB="$(sed -n 's/.*PublicKeyBase64 = "\(.*\)"/\1/p' internal/updater/pubkey.go)"
go run ./tools/updatesign verify "$PUB" "$WORK/SHA256SUMS"
gh release upload "$TAG" "$WORK/SHA256SUMS.sig" --clobber
echo "signed $TAG: SHA256SUMS.sig is on the release"
