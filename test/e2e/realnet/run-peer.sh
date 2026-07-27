#!/bin/sh
# One side of the two-VM real-network e2e (Linux only). Runs stunmesh as a
# daemon inside a crash-bunker netns (netns.sh) NAT'd to the host's real NIC,
# and drives this role's scenario phases (phases.sh):
#
#   anchor   the boring far end: split tunnel, raw mode, unchanged for the
#            whole run, plus the canary HTTP server and an iperf3 server;
#            holds until ANCHOR_HOLD_SECS so the subject can finish.
#   subject  split tunnel first, then the full-tunnel escape scenario.
#
# There is no cross-job rendezvous: endpoint exchange is stunmesh's own data
# plane (the opendht proxies), and the daemon's refresh loop is the barrier.
# Every check records a conclusion into $WORK/results.env; assert.sh turns
# those plus the daemon log into a JSON job output. This script fails only on
# infra/script errors -- the verdict belongs to report.sh, which sees both
# sides' conclusions.
#
# Usage: run-peer.sh /path/to/stunmesh
# Env:
#   ROLE               anchor | subject (required)
#   WG_PRIVATE_KEY     this side's private key (required)
#   PEER_PUBLIC_KEY    other side's public key (required)
#   RESULT_DIR         logs/results directory (default: mktemp -d, kept)
#   ENDPOINTS          opendht proxy URLs (default: dhtproxy2/3.jami.net)
#   CANARY_BIN         prebuilt canary server (anchor; default: go build)
#   ANCHOR_HOLD_SECS   anchor lifetime from script start (default 480)
#   HANDSHAKE_TIMEOUT  seconds to wait for the first handshake (default 300)
set -eu
umask 077

BIN=${1:?usage: run-peer.sh /path/to/stunmesh}
ROLE=${ROLE:?ROLE=anchor|subject is required}
WG_PRIVATE_KEY=${WG_PRIVATE_KEY:?}
PEER_PUBLIC_KEY=${PEER_PUBLIC_KEY:?}

[ "$(uname -s)" = Linux ] || { echo "realnet e2e supports Linux subjects only for now" >&2; exit 1; }
if [ "$(id -u)" = 0 ]; then SUDO=''; else SUDO='sudo'; fi
export SUDO

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
WORK=${RESULT_DIR:-$(mktemp -d)}
mkdir -p "$WORK/pcap"
RESULTS=$WORK/results.env
DAEMON_LOG=$WORK/daemon.log
: > "$RESULTS"
START_TS=$(date +%s)

ENDPOINTS=${ENDPOINTS:-'https://dhtproxy2.jami.net https://dhtproxy3.jami.net'}
# Several of these are consumed only by the sourced phases.sh.
# shellcheck disable=SC2034
DHT_HOSTS=$(for u in $ENDPOINTS; do h=${u#*://}; echo "${h%%/*}"; done)
STUN_HOST=stun.l.google.com
STUN_PORT=19302

WG_IF=wg0
WG_PORT=51820
MTU=1280
KEEPALIVE=25
# shellcheck disable=SC2034
FWMARK=51820
# shellcheck disable=SC2034
ESCAPE_TABLE=100
CANARY_IP=192.0.2.1
CANARY_PORT=8080
ANCHOR_HOLD_SECS=${ANCHOR_HOLD_SECS:-480}
HANDSHAKE_TIMEOUT=${HANDSHAKE_TIMEOUT:-300}

case "$ROLE" in
anchor) MY_OVERLAY=10.99.0.1 PEER_OVERLAY=10.99.0.2 ;;
subject) MY_OVERLAY=10.99.0.2 PEER_OVERLAY=10.99.0.1 ;;
*) echo "unknown ROLE '$ROLE'" >&2; exit 1 ;;
esac

log() { echo "[realnet:$ROLE] $*"; }
rec() { echo "$1=$2" >> "$RESULTS"; log "result: $1=$2"; }

. "$HERE/netns.sh"
. "$HERE/phases.sh"

DPID=''
TCPDUMP_PID=''
cleanup() {
	stop_daemon 2>/dev/null || true
	[ -n "$TCPDUMP_PID" ] && $SUDO kill "$TCPDUMP_PID" 2>/dev/null || true
	sleep 1
	netns_down
	# tcpdump and the daemon wrote as root; hand the evidence back to the
	# invoking user so artifact upload can read it.
	$SUDO chown -R "$(id -u):$(id -g)" "$WORK" 2>/dev/null || true
	log "logs kept in $WORK"
}
trap cleanup EXIT INT TERM

start_daemon() {
	# $WORK belongs to the invoking user; redirecting the root process's
	# output into it is intended.
	# shellcheck disable=SC2024
	$SUDO ip netns exec "$NS" "$BIN" -c "$WORK/config.yaml" >> "$DAEMON_LOG" 2>&1 &
	DPID=$!
}
stop_daemon() {
	[ -n "$DPID" ] || return 0
	$SUDO kill "$DPID" 2>/dev/null || true
	wait "$DPID" 2>/dev/null || true
	DPID=''
}

log "work=$WORK"
rec role "$ROLE"

netns_up
log "netns '$NS' up, MASQUERADE via $VETH_HOST"

# WireGuard device inside the bunker, in the split-tunnel baseline shape that
# both roles start from (the anchor never leaves it). Explicit MTU so
# the full-MTU ping asserts a known number, explicit keepalive so the NAT
# mapping lifetime is a pinned variable rather than an accident.
KEYFILE=$WORK/wg.key
printf '%s\n' "$WG_PRIVATE_KEY" > "$KEYFILE"
ns_exec ip link add "$WG_IF" type wireguard
# shellcheck disable=SC2024
$SUDO ip netns exec "$NS" wg set "$WG_IF" private-key /dev/stdin \
	listen-port "$WG_PORT" \
	peer "$PEER_PUBLIC_KEY" allowed-ips "$PEER_OVERLAY/32" \
	persistent-keepalive "$KEEPALIVE" < "$KEYFILE"
ns_exec ip addr add "$MY_OVERLAY/24" dev "$WG_IF"
ns_exec ip link set "$WG_IF" mtu "$MTU" up
log "$WG_IF up: $MY_OVERLAY, peer allowed-ips $PEER_OVERLAY/32"

# Debug level so the escape evidence ("marking STUN socket ...") is in the log.
{
	echo "log: {level: debug, format: json}"
	echo "refresh_interval: 10s"
	echo "stun: {addresses: [\"$STUN_HOST:$STUN_PORT\"]}"
	echo "plugins:"
	echo "  dht:"
	echo "    type: builtin"
	echo "    name: opendht"
	echo "    endpoints:"
	for url in $ENDPOINTS; do echo "      - $url"; done
	echo "interfaces:"
	echo "  $WG_IF:"
	echo "    protocol: ipv4"
	echo "    peers:"
	echo "      peer:"
	echo "        public_key: \"$PEER_PUBLIC_KEY\""
	echo "        plugin: dht"
	echo "        protocol: ipv4"
} > "$WORK/config.yaml"

# DHT preflight, so the report can classify failures as DHT weather vs NAT
# weather vs our bug. Any HTTP response counts as reachable.
preflight=''
for url in $ENDPOINTS; do
	h=${url#*://}; h=${h%%/*}
	if ns_exec curl -4 -sS -o /dev/null -m 10 "$url" 2>>"$WORK/curl.log"; then
		preflight="$preflight$h=ok,"
	else
		preflight="$preflight$h=fail,"
	fi
done
rec dht_preflight "${preflight%,}"

# Size-bounded evidence: headers only (payload is encrypted anyway), 2x50MB
# ring as the hard cap for the iperf window.
# shellcheck disable=SC2024
$SUDO ip netns exec "$NS" tcpdump -i any -s 160 -U -C 50 -W 2 -Z root \
	-w "$WORK/pcap/realnet.pcap" >"$WORK/tcpdump.log" 2>&1 &
TCPDUMP_PID=$!

if [ "$ROLE" = anchor ]; then
	# Off-overlay canary: static from job start, reachable only once the
	# subject's covering route exists. Replies source from $CANARY_IP, which
	# only the subject's covering AllowedIPs admit.
	if [ -z "${CANARY_BIN:-}" ]; then
		CANARY_BIN=$WORK/canary
		go build -o "$CANARY_BIN" "$HERE/canary"
	fi
	ns_exec ip link add canary0 type dummy
	ns_exec ip addr add "$CANARY_IP/32" dev canary0
	ns_exec ip link set canary0 up
	# shellcheck disable=SC2024
	$SUDO ip netns exec "$NS" "$CANARY_BIN" -listen "$CANARY_IP:$CANARY_PORT" \
		>"$WORK/canary.log" 2>&1 &
	for _ in $(seq 1 20); do
		grep -q CANARY_READY "$WORK/canary.log" 2>/dev/null && break
		sleep 1
	done
	grep -q CANARY_READY "$WORK/canary.log" || { echo "canary never came up" >&2; exit 1; }
	rec canary_sha256 "$(sed -n 's/^CANARY_SHA256=//p' "$WORK/canary.log" | head -1)"

	ns_exec iperf3 -s -D --logfile "$WORK/iperf3.log"
fi

log "starting stunmesh daemon"
start_daemon

case "$ROLE" in
anchor)
	split_tunnel_anchor || true
	left=$((ANCHOR_HOLD_SECS - ($(date +%s) - START_TS)))
	if [ "$left" -gt 0 ]; then
		log "holding as far end for another ${left}s"
		sleep "$left"
	fi
	;;
subject)
	# The full-tunnel checks are relative to the split-tunnel baseline: they
	# run only if it handshook, so NAT weather can never masquerade as a
	# routing or escape regression.
	if split_tunnel_subject; then
		full_tunnel_subject || true
	else
		log "no baseline handshake; skipping the full-tunnel scenario"
		rec fulltunnel_ran no
	fi
	;;
esac

# Final state snapshots while the namespace still exists; peer_endpoint is the
# report job's half of the publish->store->establish cross-equality.
ns_exec wg show "$WG_IF" > "$WORK/wg-final.log" 2>&1 || true
ns_exec wg show "$WG_IF" dump >> "$WORK/wg-final.log" 2>&1 || true
pe=$(ns_exec wg show "$WG_IF" endpoints | awk 'NR==1{print $2}')
rec peer_endpoint "${pe:-"(none)"}"

stop_daemon
sh "$HERE/assert.sh" "$WORK"
