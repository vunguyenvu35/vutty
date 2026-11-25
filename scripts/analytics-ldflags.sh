#!/usr/bin/env bash
# Prints the -ldflags words that give a build its Google Analytics stream (see docs/development/analytics.md), or
# nothing when VUTTY_GA_MEASUREMENT_ID and VUTTY_GA_API_SECRET are not both set: such a build never reports anything.
# Used by the Makefile and by the release workflow, so the two cannot disagree. A value that is not what Google issues
# is refused, so a stray character cannot become another linker flag.
set -euo pipefail
id="${VUTTY_GA_MEASUREMENT_ID:-}"
secret="${VUTTY_GA_API_SECRET:-}"
[ -n "$id" ] && [ -n "$secret" ] || exit 0
if ! [[ "$id" =~ ^G-[A-Za-z0-9]{4,20}$ ]]; then echo "error: VUTTY_GA_MEASUREMENT_ID must look like G-XXXXXXXXXX" >&2; exit 1; fi
if ! [[ "$secret" =~ ^[A-Za-z0-9_-]{8,64}$ ]]; then echo "error: VUTTY_GA_API_SECRET has characters Google does not use" >&2; exit 1; fi
printf '%s' "-X vutty/internal/analytics.MeasurementID=$id -X vutty/internal/analytics.APISecret=$secret"
