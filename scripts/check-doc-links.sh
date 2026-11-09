#!/usr/bin/env bash
# Every relative link and image of README.md and docs/**/*.md must point at a file (and a heading) that exists.
exec node "$(dirname "$0")/check-doc-links.mjs"
