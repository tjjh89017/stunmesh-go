#!/bin/sh
# Wait until this pair's anchor job is at least running, so the subject's
# fixed handshake window never starts ticking against an anchor that is
# still queued. The mirror of subject-done.sh, and like it advisory-only:
# outside Actions, without gh, or on timeout it just returns -- the daemon's
# refresh loop retries anyway, so the worst case is what we have today.
#
# Usage: wait-anchor.sh PAIR
# Env:   ANCHOR_WAIT_SECS  how long to wait before giving up (default 600)
set -eu

pair=${1:?usage: wait-anchor.sh PAIR}
[ -n "${GITHUB_RUN_ID:-}" ] || exit 0
[ -n "${GITHUB_REPOSITORY:-}" ] || exit 0
command -v gh >/dev/null 2>&1 || exit 0

deadline=$(($(date +%s) + ${ANCHOR_WAIT_SECS:-600}))
while [ "$(date +%s)" -lt "$deadline" ]; do
	status=$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/jobs?per_page=100" \
		--jq ".jobs[] | select(.name==\"E2E realnet (linux anchor for $pair)\") | .status" 2>/dev/null | head -1)
	case "$status" in
	in_progress | completed)
		echo "anchor for '$pair' is $status; proceeding"
		exit 0
		;;
	esac
	echo "anchor for '$pair' is '${status:-unknown}'; waiting"
	sleep 15
done
echo "anchor for '$pair' never started within ${ANCHOR_WAIT_SECS:-600}s; proceeding anyway"
