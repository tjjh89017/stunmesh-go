#!/bin/sh
# Shared setup for the netns wgproxy e2e (test/e2e/proxy). Sourced by run.sh.
#
# Topology: two network namespaces joined by a veth pair, kernel WireGuard on
# both sides, the wgproxy relay (runner binary) fronting side A. Side A's WG
# peer endpoint points at the proxy's inner loopback listener; side B dials
# the proxy's outer port. No internet, no STUN, no store — deterministic.
#
#   ns A: wgpa (127.0.0.1 <-> runner inner) --- runner outer @ 10.99.9.1:P
#                                                     |
#                                            veth 10.99.9.1 <-> 10.99.9.2
#                                                     |
#   ns B: wgpb (endpoint 10.99.9.1:P) ----------------+
#
# Requires root and `ip netns`. Every command runs under $SUDO like the
# existing test/e2e/run.sh, so the scripts also work as a non-root user with
# passwordless sudo.
#
# shellcheck disable=SC2034  # topology constants are consumed by the sourcing script
set -eu
umask 077

NS_A=smproxy-a
NS_B=smproxy-b
VETH_A=smpva
VETH_B=smpvb
ADDR_A=10.99.9.1
ADDR_B=10.99.9.2
WG_A=wgpa
WG_B=wgpb
OVL_A=10.66.9.1
OVL_B=10.66.9.2
PORT_A=51820
PORT_B=51821
# wg default MTU is 1420; 1392 + 8 (ICMP) + 20 (IP) = 1420, so -M do at this
# size proves full-MTU datagrams survive the relay untruncated.
PING_SIZE=1392

ROOT=$(CDPATH='' cd "$(dirname "$0")/../../.." && pwd)
WORK=$(mktemp -d)
[ "$(id -u)" = 0 ] && SUDO='' || SUDO='sudo'
RUNNER_PID=''
OUTER_PORT=''
INNER_PORT=''

log() { echo "[proxy-e2e] $*"; }
fail() { echo "[proxy-e2e] FAIL: $*" >&2; exit 1; }

in_a() { $SUDO ip netns exec "$NS_A" "$@"; }
in_b() { $SUDO ip netns exec "$NS_B" "$@"; }

teardown_topology() {
	stop_runner || true
	$SUDO ip netns del "$NS_A" 2>/dev/null || true
	$SUDO ip netns del "$NS_B" 2>/dev/null || true
}

cleanup() {
	teardown_topology
	rm -rf "$WORK"
}

setup_ns() {
	# A previous crashed run may have left namespaces behind.
	$SUDO ip netns del "$NS_A" 2>/dev/null || true
	$SUDO ip netns del "$NS_B" 2>/dev/null || true
	$SUDO ip netns add "$NS_A"
	$SUDO ip netns add "$NS_B"
	$SUDO ip link add "$VETH_A" type veth peer name "$VETH_B"
	$SUDO ip link set "$VETH_A" netns "$NS_A"
	$SUDO ip link set "$VETH_B" netns "$NS_B"
	in_a ip addr add "$ADDR_A/24" dev "$VETH_A"
	in_b ip addr add "$ADDR_B/24" dev "$VETH_B"
	in_a ip link set lo up
	in_b ip link set lo up
	in_a ip link set "$VETH_A" up
	in_b ip link set "$VETH_B" up
}

gen_keys() {
	APRIV=$WORK/a.key
	BPRIV=$WORK/b.key
	wg genkey > "$APRIV"
	wg genkey > "$BPRIV"
	APUB=$(wg pubkey < "$APRIV")
	BPUB=$(wg pubkey < "$BPRIV")
}

# make_wg <in_a|in_b> IFACE KEYFILE PORT PEER_PUB PEER_ALLOWED OVERLAY [ENDPOINT] [KEEPALIVE]
make_wg() {
	ns=$1; name=$2; keyfile=$3; port=$4; peer=$5; allowed=$6; overlay=$7
	endpoint=${8:-}; keepalive=${9:-}
	"$ns" ip link add "$name" type wireguard
	set -- "$name" private-key "$keyfile" listen-port "$port" \
		peer "$peer" allowed-ips "$allowed"
	[ -n "$endpoint" ] && set -- "$@" endpoint "$endpoint"
	[ -n "$keepalive" ] && set -- "$@" persistent-keepalive "$keepalive"
	"$ns" wg set "$@"
	"$ns" ip addr add "$overlay/24" dev "$name"
	"$ns" ip link set "$name" up
}

build_runner() {
	RUNNER_BIN=$WORK/wgproxy-runner
	(cd "$ROOT" && go build -o "$RUNNER_BIN" ./test/e2e/proxy/runner)
}

# start_runner [extra runner flags...] — always in ns A, peer B, wg-port A.
start_runner() {
	RUNNER_LOG=$WORK/runner.log
	in_a "$RUNNER_BIN" -peer "$BPUB" -wg-port "$PORT_A" "$@" \
		> "$RUNNER_LOG" 2> "$WORK/runner.err" &
	for _ in $(seq 1 50); do
		OUTER_PORT=$(sed -n 's/^OUTER_PORT=//p' "$RUNNER_LOG" | head -1)
		INNER_PORT=$(sed -n 's/^INNER_PORT=//p' "$RUNNER_LOG" | head -1)
		[ -n "$OUTER_PORT" ] && [ -n "$INNER_PORT" ] && break
		sleep 0.1
	done
	[ -n "$OUTER_PORT" ] && [ -n "$INNER_PORT" ] || {
		cat "$WORK/runner.err" >&2 || true
		fail "runner never reported its ports"
	}
	# pgrep -x excludes the sudo/ip wrapper processes whose argv also mentions
	# the binary path.
	RUNNER_PID=$($SUDO pgrep -x wgproxy-runner)
	log "runner up: pid=$RUNNER_PID outer=$OUTER_PORT inner=$INNER_PORT"
}

stop_runner() {
	[ -n "$RUNNER_PID" ] || return 0
	$SUDO kill "$RUNNER_PID" 2>/dev/null || true
	for _ in $(seq 1 20); do
		$SUDO pgrep -x wgproxy-runner >/dev/null 2>&1 || break
		sleep 0.1
	done
	RUNNER_PID=''
}

# wait_handshake <in_a|in_b> IFACE TIMEOUT_SECONDS -> 0 iff a handshake completed
wait_handshake() {
	ns=$1; name=$2; timeout=$3
	i=0
	while [ "$i" -lt "$timeout" ]; do
		ts=$("$ns" wg show "$name" latest-handshakes | awk '{print $2}')
		[ "${ts:-0}" -gt 0 ] && return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}

handshake_ts() { # <in_a|in_b> IFACE
	"$1" wg show "$2" latest-handshakes | awk '{print $2}'
}

# assert_outer_socket — the runner pid still holds a UDP socket on OUTER_PORT
# in ns A (port lifetime invariant: bound once, never rebound or recreated).
assert_outer_socket() {
	out=$(in_a ss -ulnpH "sport = :$OUTER_PORT")
	echo "$out" | grep -q "pid=$RUNNER_PID," ||
		fail "outer port $OUTER_PORT not held by runner pid $RUNNER_PID: $out"
}

# assert_port_unchanged — SIGHUP makes the runner re-print OUTER_PORT; every
# line printed so far must be the original port.
assert_port_unchanged() {
	$SUDO kill -HUP "$RUNNER_PID"
	sleep 0.3
	ports=$(sed -n 's/^OUTER_PORT=//p' "$RUNNER_LOG" | sort -u)
	[ "$ports" = "$OUTER_PORT" ] || fail "outer port changed: $ports"
	assert_outer_socket
}

# full_mtu_traffic <in_a|in_b> DST — 200 full-MTU pings through the tunnel,
# zero loss required. Truncation anywhere in the relay breaks these.
full_mtu_traffic() {
	ns=$1; dst=$2
	"$ns" ping -q -c 200 -i 0.005 -W 2 -M "do" -s "$PING_SIZE" "$dst" \
		| grep -q ' 0% packet loss' || fail "full-MTU traffic to $dst lost packets"
	log "full-MTU traffic to $dst: 200/200 delivered"
}

iperf_traffic() {
	command -v iperf3 >/dev/null 2>&1 || { log "iperf3 not installed, skipping stream test"; return 0; }
	in_b iperf3 -s -1 -D
	sleep 1
	in_a iperf3 -c "$OVL_B" -t 5 || fail "iperf3 stream through the relay failed"
	log "iperf3 stream through the relay ok"
}
