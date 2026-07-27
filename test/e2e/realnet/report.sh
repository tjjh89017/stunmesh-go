#!/bin/sh
# Combined verdict for a realnet run, from both sides' conclusions.
#
# Verification lives here rather than in either peer by construction: the
# subject cannot self-certify the canary it fetched, and only a step that
# sees both sides can check that each side's WireGuard peer endpoint is
# something the *other* side actually discovered via STUN.
#
# Hard checks -- a failure here means our bug, not network weather:
#   * both peer runs completed
#   * the publish -> store -> establish round-trip propagated real endpoints
#     in both directions across two independent public IPs
#   * a fetched canary blob is byte-identical to the one served
# Everything else (handshake, ping, throughput, escape survival) is recorded
# for trend-watching only: two cloud runners give no NAT-behavior guarantee,
# so a hard assert there would fail for reasons that are not ours. Promote a
# check once its observed success rate justifies it.
#
# Env: ANCHOR_RESULTS / SUBJECT_RESULTS  the JSON emitted by assert.sh
#      ANCHOR_JOB / SUBJECT_JOB          job results ("success" when fine)
set -eu

ANCHOR_RESULTS=${ANCHOR_RESULTS:-}
SUBJECT_RESULTS=${SUBJECT_RESULTS:-}
ANCHOR_JOB=${ANCHOR_JOB:-unknown}
SUBJECT_JOB=${SUBJECT_JOB:-unknown}
SUM=${GITHUB_STEP_SUMMARY:-/dev/stdout}
fail=0

get() { # BLOB KEY -> value, or "(missing)"
	[ -n "$1" ] || { echo "(missing)"; return; }
	printf '%s' "$1" | jq -r --arg k "$2" '.[$k] // "(missing)"' 2>/dev/null || echo "(missing)"
}
hard() { # NAME OK DETAIL
	if [ "$2" = ok ]; then _v="pass"; else _v="FAIL"; fail=1; fi
	echo "| $1 | **$_v** | $3 |" >> "$SUM"
	echo "hard: $1 -> $_v ($3)"
}
info() { # NAME VALUE
	echo "| $1 | ${2:-(missing)} | recorded, not gating |" >> "$SUM"
	echo "info: $1 = ${2:-(missing)}"
}
member() { # NEEDLE CSV
	[ -n "$1" ] && [ "$1" != "(none)" ] && [ "$1" != "(missing)" ] || return 1
	case ",$2," in *",$1,"*) return 0 ;; *) return 1 ;; esac
}

{
	echo "## realnet verdict"
	echo ""
	echo "| check | result | detail |"
	echo "|---|---|---|"
} >> "$SUM"

ok=ok; [ "$ANCHOR_JOB" = success ] || ok=bad
hard "anchor run" "$ok" "$ANCHOR_JOB"
ok=ok; [ "$SUBJECT_JOB" = success ] || ok=bad
hard "subject run" "$ok" "$SUBJECT_JOB"

a_disc=$(get "$ANCHOR_RESULTS" discovered_all)
s_disc=$(get "$SUBJECT_RESULTS" discovered_all)
a_ep=$(get "$ANCHOR_RESULTS" peer_endpoint)
s_ep=$(get "$SUBJECT_RESULTS" peer_endpoint)

# The round-trip a same-host harness can never prove: two independent public
# IPs, each side's peer endpoint learned only through the storage backend.
ok=ok; member "$s_ep" "$a_disc" || ok=bad
hard "endpoint round-trip (anchor to subject)" "$ok" \
	"subject peer endpoint '$s_ep' among anchor's discovered {$a_disc}"
ok=ok; member "$a_ep" "$s_disc" || ok=bad
hard "endpoint round-trip (subject to anchor)" "$ok" \
	"anchor peer endpoint '$a_ep' among subject's discovered {$s_disc}"

# Integrity is hard whenever a fetch succeeded: a mismatch is corruption,
# never weather. A fetch that never completed stays informational.
canary=$(get "$SUBJECT_RESULTS" fulltunnel_canary)
if [ "$canary" = pass ]; then
	served=$(get "$ANCHOR_RESULTS" canary_sha256)
	fetched=$(get "$SUBJECT_RESULTS" fulltunnel_canary_sha256)
	ok=ok
	[ "$fetched" = "$served" ] && [ "$served" != "(missing)" ] || ok=bad
	hard "canary blob integrity" "$ok" "fetched $fetched vs served $served"
else
	info "canary fetch" "$canary"
fi

info "storage preflight (anchor)" "$(get "$ANCHOR_RESULTS" dht_preflight)"
info "storage preflight (subject)" "$(get "$SUBJECT_RESULTS" dht_preflight)"
info "handshake (anchor)" "$(get "$ANCHOR_RESULTS" split_handshake) after $(get "$ANCHOR_RESULTS" split_handshake_secs)s"
info "handshake (subject)" "$(get "$SUBJECT_RESULTS" split_handshake) after $(get "$SUBJECT_RESULTS" split_handshake_secs)s"
info "overlay ping (anchor to subject)" "$(get "$ANCHOR_RESULTS" split_ping)"
info "overlay ping (subject to anchor)" "$(get "$SUBJECT_RESULTS" split_ping)"
info "full-MTU ping" "$(get "$SUBJECT_RESULTS" split_ping_mtu)"
info "iperf3 up / down" "$(get "$SUBJECT_RESULTS" split_iperf_up) / $(get "$SUBJECT_RESULTS" split_iperf_down)"
info "transfer counters rose" "$(get "$SUBJECT_RESULTS" split_transfer_rise)"
info "ping resumes after 60s idle" "$(get "$SUBJECT_RESULTS" split_idle_resume)"
info "full-tunnel scenario ran" "$(get "$SUBJECT_RESULTS" fulltunnel_ran)"
info "STUN route escapes the tunnel" "$(get "$SUBJECT_RESULTS" fulltunnel_escape_route)"
info "canary routed over WireGuard" "$(get "$SUBJECT_RESULTS" fulltunnel_route_canary)"
info "escape survives refresh cycles" "$(get "$SUBJECT_RESULTS" fulltunnel_survival)"
info "no endpoint flap to loopback" "$(get "$SUBJECT_RESULTS" fulltunnel_endpoint_flap)"
info "daemon error lines (anchor / subject)" \
	"$(get "$ANCHOR_RESULTS" error_count) / $(get "$SUBJECT_RESULTS" error_count)"

if [ "$fail" = 0 ]; then
	echo "PASS"
else
	echo "realnet hard checks failed"
fi
exit "$fail"
