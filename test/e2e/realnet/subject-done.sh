#!/bin/sh
# Exit 0 once this pair's subject job has completed, so the anchor can
# release its hold early instead of sleeping out the full window. CI-only:
# outside Actions (no run id, no gh, no token) it exits nonzero, which the
# caller treats as "keep holding" -- the fixed hold window is the fallback,
# so a failure here can only ever cost time, never correctness.
#
# Usage: subject-done.sh PAIR
set -eu

pair=${1:?usage: subject-done.sh PAIR}
[ -n "${GITHUB_RUN_ID:-}" ] || exit 1
[ -n "${GITHUB_REPOSITORY:-}" ] || exit 1
command -v gh >/dev/null 2>&1 || exit 1

status=$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/jobs?per_page=100" \
	--jq ".jobs[] | select(.name==\"E2E realnet ($pair)\") | .status" 2>/dev/null | head -1)
[ "$status" = completed ]
