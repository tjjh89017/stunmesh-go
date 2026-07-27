#!/bin/sh
# Turns a realnet peer run into machine-readable conclusions: merges the
# results.env recorded by phases.sh with facts derived from the daemon log,
# then emits the whole set as one compact JSON job output (guarded -- falls
# back to stdout when run outside Actions) plus a human table.
#
# Conclusions travel in the output, evidence stays in the logs. The verdict
# itself is report.sh's job, since it needs both sides; this script never
# fails the peer job on a soft check.
#
# Usage: assert.sh RESULT_DIR
set -eu

WORK=${1:?usage: assert.sh RESULT_DIR}
RESULTS=$WORK/results.env
DAEMON_LOG=$WORK/daemon.log
OUT=${GITHUB_OUTPUT:-/dev/stdout}
SUM=${GITHUB_STEP_SUMMARY:-/dev/stdout}

# Derived facts from the daemon log. discovered_all is every endpoint STUN
# found this run: report.sh checks membership rather than the final value,
# since the NAT may rebind between the two sides' snapshot moments.
discovered=$(jq -rc 'select(.message=="discovered IPv4 endpoint")|.ipv4' "$DAEMON_LOG" 2>/dev/null | tail -1)
discovered_all=$(jq -rc 'select(.message=="discovered IPv4 endpoint")|.ipv4' "$DAEMON_LOG" 2>/dev/null | sort -u | paste -sd, -)
errors=$(jq -c 'select(.level=="error")' "$DAEMON_LOG" 2>/dev/null | wc -l | tr -d ' ')

{
	echo "discovered=${discovered:-(none)}"
	echo "discovered_all=${discovered_all:-(none)}"
	echo "error_count=${errors:-0}"
} >> "$RESULTS"

role=$(sed -n 's/^role=//p' "$RESULTS" | head -1)

# Values may themselves contain '=', so split on the first one only.
json=$(jq -Rc -n '[inputs | select(length > 0) | split("=") |
	{(.[0]): (.[1:] | join("="))}] | add' < "$RESULTS")
echo "results=$json" >> "$OUT"

{
	echo "### realnet peer: ${role:-unknown}"
	echo ""
	echo "| check | result |"
	echo "|---|---|"
	while IFS='=' read -r k v; do
		[ -n "$k" ] || continue
		echo "| $k | $v |"
	done < "$RESULTS"
} >> "$SUM"

echo "== recorded conclusions =="
cat "$RESULTS"
if [ "${errors:-0}" != 0 ]; then
	echo "note: daemon logged $errors error line(s), tolerated here -- report.sh owns the verdict:"
	jq -c 'select(.level=="error")|{message,error}' "$DAEMON_LOG" 2>/dev/null | head -20
fi
