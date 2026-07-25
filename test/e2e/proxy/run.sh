#!/bin/sh
# Netns e2e for internal/wgproxy: kernel WireGuard on both sides of a veth
# pair, the wgproxy relay fronting side A. See lib.sh for the topology.
#
# Usage: run.sh a|b|c|d|all      (root, or a user with passwordless sudo)
#   a  handshake + full-MTU traffic stream through the relay
#   b  remote-initiated handshake before the proxy ever saw WG output from A
#      (proves the WG-side target is SetWGTarget-fed, not packet-learned)
#   c  negative control: unprogrammed mapping => handshake fails; same setup
#      with the mapping programmed => handshake succeeds
#   d  mid-flow disruption (remote peer torn down, ICMP unreachables, link
#      flap, garbage flood) => proxy never rebinds, outer port unchanged,
#      traffic resumes
set -eu

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR
. "$HERE/lib.sh"

# standard_topology [A_KEEPALIVE] [B_KEEPALIVE] [RUNNER_FLAGS...]
standard_topology() {
	a_ka=$1; b_ka=$2; shift 2
	setup_ns
	start_runner "$@"
	make_wg in_a "$WG_A" "$APRIV" "$PORT_A" "$BPUB" "$OVL_B/32" "$OVL_A" \
		"127.0.0.1:$INNER_PORT" "$a_ka"
	make_wg in_b "$WG_B" "$BPRIV" "$PORT_B" "$APUB" "$OVL_A/32" "$OVL_B" \
		"$ADDR_A:$OUTER_PORT" "$b_ka"
}

case_a() {
	log "case a: handshake + full-MTU traffic through the relay"
	standard_topology 2 '' -remote "$ADDR_B:$PORT_B"
	wait_handshake in_a "$WG_A" 15 || fail "handshake never completed"
	log "handshake ok"
	full_mtu_traffic in_a "$OVL_B"
	full_mtu_traffic in_b "$OVL_A"
	iperf_traffic
	log "case a PASS"
}

case_b() {
	log "case b: remote-initiated handshake first"
	# A has no keepalive and no traffic, so the proxy has never seen a WG
	# packet from side A when B's initiation arrives; relaying it inward can
	# only work because SetWGTarget was fed from configuration.
	standard_topology '' 2 -remote "$ADDR_B:$PORT_B"
	wait_handshake in_b "$WG_B" 15 || fail "remote-initiated handshake never completed"
	[ "$(handshake_ts in_a "$WG_A")" -gt 0 ] || fail "side A never saw the handshake"
	log "handshake ok (initiated by B)"
	full_mtu_traffic in_b "$OVL_A"
	log "case b PASS"
}

case_c() {
	log "case c arm 1: peer mapping unprogrammed => handshake must fail"
	standard_topology '' 2
	if wait_handshake in_b "$WG_B" 10; then
		fail "handshake succeeded although the peer mapping was never programmed"
	fi
	[ "$(handshake_ts in_a "$WG_A")" = 0 ] || fail "side A saw a handshake in the negative arm"
	log "arm 1 ok: no handshake without a programmed mapping"
	teardown_topology

	log "case c arm 2: identical setup with the mapping programmed => handshake succeeds"
	standard_topology '' 2 -remote "$ADDR_B:$PORT_B"
	wait_handshake in_b "$WG_B" 15 ||
		fail "handshake failed even with the mapping programmed; arm 1 proves nothing"
	log "arm 2 ok: mapping is the only difference and it flips the outcome"
	log "case c PASS"
}

case_d() {
	log "case d: mid-flow disruption; no rebind, port stable, traffic resumes"
	standard_topology 2 2 -remote "$ADDR_B:$PORT_B"
	wait_handshake in_a "$WG_A" 15 || fail "initial handshake never completed"
	full_mtu_traffic in_a "$OVL_B"
	assert_outer_socket
	port_before=$OUTER_PORT

	log "disruption: tearing down $WG_B (A's keepalives now draw ICMP port-unreachable)"
	in_b ip link del "$WG_B"
	sleep 6
	log "disruption: garbage flood from unmapped source ports"
	in_b bash -c "for _ in \$(seq 1 100); do echo garbage > /dev/udp/$ADDR_A/$OUTER_PORT; done"
	log "disruption: flapping $VETH_B"
	in_b ip link set "$VETH_B" down
	sleep 1
	in_b ip link set "$VETH_B" up

	make_wg in_b "$WG_B" "$BPRIV" "$PORT_B" "$APUB" "$OVL_A/32" "$OVL_B" \
		"$ADDR_A:$OUTER_PORT" 2
	wait_handshake in_b "$WG_B" 20 || fail "handshake never re-established after disruption"
	full_mtu_traffic in_a "$OVL_B"
	full_mtu_traffic in_b "$OVL_A"

	[ "$OUTER_PORT" = "$port_before" ] || fail "outer port variable mutated"
	assert_port_unchanged
	binds=$(grep -c 'outer socket bound' "$WORK/runner.err")
	[ "$binds" = 1 ] || fail "outer socket bound $binds times; must be exactly once"
	log "case d PASS (port $OUTER_PORT stable, single bind, traffic resumed)"
}

run_case() {
	"case_$1"
	teardown_topology
}

sel=${1:?usage: run.sh a|b|c|d|all}
trap cleanup EXIT INT TERM
build_runner
gen_keys
case $sel in
a | b | c | d) run_case "$sel" ;;
all) for c in a b c d; do run_case "$c"; done ;;
*) fail "unknown case '$sel'" ;;
esac
log "done"
