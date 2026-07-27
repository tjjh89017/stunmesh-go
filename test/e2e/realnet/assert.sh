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

# In proxy mode the device's peer endpoint is the proxy's own loopback inner
# socket and the real remote lives in the proxy's demux mapping, so the
# endpoint that proves the round-trip is the remote the proxy programmed.
# Windows has no other mode; elsewhere this line is simply absent.
proxy_remote=$(jq -rc 'select(.message=="peer endpoint substituted with proxy inner socket")|.remote' "$DAEMON_LOG" 2>/dev/null | tail -1)
wg_endpoint=$(sed -n 's/^wg_endpoint=//p' "$RESULTS" | head -1)

{
	echo "discovered=${discovered:-(none)}"
	echo "discovered_all=${discovered_all:-(none)}"
	echo "peer_endpoint=${proxy_remote:-${wg_endpoint:-(none)}}"
	echo "error_count=${errors:-0}"
} >> "$RESULTS"

role=$(sed -n 's/^role=//p' "$RESULTS" | head -1)

# Values may themselves contain '=', so split on the first one only.
# results.json is what report.sh consumes: matrix rows share a single job
# output namespace, so the conclusions travel as an artifact instead.
jq -Rc -n '[inputs | select(length > 0) | split("=") |
	{(.[0]): (.[1:] | join("="))}] | add' < "$RESULTS" > "$WORK/results.json"
echo "results=$(cat "$WORK/results.json")" >> "$OUT"

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
