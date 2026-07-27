#!/bin/sh
# Combined verdict for a realnet run, over every subject platform.
#
# Verification lives here rather than in either peer by construction: a
# subject cannot self-certify the canary blob it fetched, and only something
# that sees both sides can check that each side's WireGuard peer endpoint is
# an address the *other* side actually discovered via STUN.
#
# Hard checks -- a failure means our bug, not network weather:
#   * both sides of a pair reported conclusions at all
#   * the publish -> store -> establish round-trip propagated real endpoints
#     in both directions across two independent public IPs
#   * a fetched canary blob is byte-identical to the one served
# Everything else (handshake, ping, throughput, escape survival) is recorded
# for trend-watching only: cloud runners give no NAT-behavior guarantee, so a
# hard assert there would fail for reasons that are not ours. Promote a check
# once its observed success rate justifies it.
#
# Usage: report.sh ARTIFACT_DIR
#   ARTIFACT_DIR holds one directory per uploaded result set, named
#   realnet-results-<pair>-<role>, each containing results.json.
# Env: REALNET_PAIRS  pairs that were expected to run; a pair listed here but
#                     missing from ARTIFACT_DIR is a hard failure, which is
#                     how a job that died before reporting gets caught.
set -eu

DIR=${1:?usage: report.sh ARTIFACT_DIR}
SUM=${GITHUB_STEP_SUMMARY:-/dev/stdout}
fail=0

# Default to whatever arrived, which cannot notice a wholly missing pair --
# the workflow always passes the matrix explicitly.
PAIRS=${REALNET_PAIRS:-$(find "$DIR" -mindepth 1 -maxdepth 1 -name 'realnet-results-*-anchor' 2>/dev/null |
	sed -n 's/.*realnet-results-\(.*\)-anchor$/\1/p' | sort -u | tr '\n' ' ')}

get() { # FILE KEY -> value, or "(missing)"
	[ -f "$1" ] || { echo "(missing)"; return; }
	jq -r --arg k "$2" '.[$k] // "(missing)"' "$1" 2>/dev/null || echo "(missing)"
}
hard() { # PAIR NAME OK DETAIL
	if [ "$3" = ok ]; then _v="pass"; else _v="FAIL"; fail=1; fi
	echo "| $1 | $2 | **$_v** | $4 |" >> "$SUM"
	echo "hard[$1]: $2 -> $_v ($4)"
}
info() { # PAIR NAME VALUE
	echo "| $1 | $2 | ${3:-(missing)} | recorded, not gating |" >> "$SUM"
	echo "info[$1]: $2 = ${3:-(missing)}"
}
member() { # NEEDLE CSV
	[ -n "$1" ] && [ "$1" != "(none)" ] && [ "$1" != "(missing)" ] || return 1
	case ",$2," in *",$1,"*) return 0 ;; *) return 1 ;; esac
}

{
	echo "## realnet verdict"
	echo ""
	echo "| pair | check | result | detail |"
	echo "|---|---|---|---|"
} >> "$SUM"

for pair in $PAIRS; do
	a=$DIR/realnet-results-$pair-anchor/results.json
	s=$DIR/realnet-results-$pair-subject/results.json

	ok=ok; [ -f "$a" ] || ok=bad
	hard "$pair" "anchor reported" "$ok" "${a#"$DIR"/}"
	ok=ok; [ -f "$s" ] || ok=bad
	hard "$pair" "subject reported" "$ok" "${s#"$DIR"/}"
	if [ ! -f "$a" ] || [ ! -f "$s" ]; then
		continue
	fi

	a_disc=$(get "$a" discovered_all); s_disc=$(get "$s" discovered_all)
	a_ep=$(get "$a" peer_endpoint); s_ep=$(get "$s" peer_endpoint)

	# The round-trip a same-host harness can never prove: two independent
	# public IPs, each side learning the other's endpoint only through the
	# storage backend.
	ok=ok; member "$s_ep" "$a_disc" || ok=bad
	hard "$pair" "endpoint round-trip (anchor to subject)" "$ok" \
		"subject peer endpoint '$s_ep' among anchor's discovered {$a_disc}"
	ok=ok; member "$a_ep" "$s_disc" || ok=bad
	hard "$pair" "endpoint round-trip (subject to anchor)" "$ok" \
		"anchor peer endpoint '$a_ep' among subject's discovered {$s_disc}"

	# Integrity is hard whenever a fetch succeeded: a mismatch is corruption,
	# never weather. A fetch that never completed stays informational.
	canary=$(get "$s" fulltunnel_canary)
	if [ "$canary" = pass ]; then
		served=$(get "$a" canary_sha256); fetched=$(get "$s" fulltunnel_canary_sha256)
		ok=ok
		[ "$fetched" = "$served" ] && [ "$served" != "(missing)" ] || ok=bad
		hard "$pair" "canary blob integrity" "$ok" "fetched $fetched vs served $served"
	fi

	info "$pair" "subject platform" "$(get "$s" os)"
	info "$pair" "storage preflight (anchor / subject)" \
		"$(get "$a" dht_preflight) / $(get "$s" dht_preflight)"
	info "$pair" "handshake (anchor)" \
		"$(get "$a" split_handshake) after $(get "$a" split_handshake_secs)s"
	info "$pair" "handshake (subject)" \
		"$(get "$s" split_handshake) after $(get "$s" split_handshake_secs)s"
	info "$pair" "overlay ping (anchor / subject)" \
		"$(get "$a" split_ping) / $(get "$s" split_ping)"
	info "$pair" "full-MTU ping" "$(get "$s" split_ping_mtu)"
	info "$pair" "iperf3 up / down" \
		"$(get "$s" split_iperf_up) / $(get "$s" split_iperf_down)"
	info "$pair" "transfer counters rose" "$(get "$s" split_transfer_rise)"
	info "$pair" "ping resumes after 60s idle" "$(get "$s" split_idle_resume)"

	# The full-tunnel scenario needs the Linux crash bunker; elsewhere the
	# skip reason is recorded so an absent result is never read as a pass.
	if [ "$(get "$s" fulltunnel_ran)" = yes ]; then
		info "$pair" "canary fetch" "$canary"
		info "$pair" "STUN route escapes the tunnel" "$(get "$s" fulltunnel_escape_route)"
		info "$pair" "canary routed over WireGuard" "$(get "$s" fulltunnel_route_canary)"
		info "$pair" "escape survives refresh cycles" "$(get "$s" fulltunnel_survival)"
		info "$pair" "no endpoint flap to loopback" "$(get "$s" fulltunnel_endpoint_flap)"
	else
		info "$pair" "full-tunnel scenario" "skipped: $(get "$s" fulltunnel_skipped)"
	fi
	info "$pair" "daemon error lines (anchor / subject)" \
		"$(get "$a" error_count) / $(get "$s" error_count)"
done

if [ -z "$PAIRS" ]; then
	echo "| (none) | pairs reported | **FAIL** | no results found in $DIR |" >> "$SUM"
	echo "no realnet results found in $DIR"
	fail=1
fi

if [ "$fail" = 0 ]; then
	echo "PASS"
else
	echo "realnet hard checks failed"
fi
exit "$fail"
