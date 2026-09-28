#!/bin/sh
# One side of the two-VM real-network e2e.
#
#   anchor   the fixed far end, always Linux and always the same split-tunnel
#            raw-socket shape, plus the canary and iperf3 servers; holds until
#            ANCHOR_HOLD_SECS so a slow subject still finds it there.
#   subject  the side under test: the split-tunnel baseline on every platform,
#            then the full-tunnel escape scenario where a crash bunker makes
#            that safe (Linux only -- see device.sh).
#
# There is no cross-job rendezvous: endpoint exchange is stunmesh's own data
# plane (the storage plugin), and the daemon's refresh loop is the barrier.
# Every check records a conclusion into $WORK/results.env; assert.sh turns
# those plus the daemon log into results.json. The gating verdict belongs to
# report.sh, which sees both sides. This script fails on infra/script errors,
# and on a missing pair acknowledgement: the subject POSTs one through the
# tunnel to the anchor's overlay canary, and both sides exit nonzero after
# assert.sh when it did not arrive, so both cells of a pair fail together
# (whatever the cause) and "Re-run failed jobs" reruns the pair.
#
# Usage: run-peer.sh /path/to/stunmesh
# Env:
#   ROLE               anchor | subject (required)
#   WG_PRIVATE_KEY     this side's private key (required)
#   PEER_PUBLIC_KEY    other side's public key (required)
#   RESULT_DIR         logs/results directory (default: mktemp -d, kept)
#   ENDPOINTS          opendht proxy URLs (default: dhtproxy2/3.jami.net)
#   CANARY_BIN         prebuilt canary server (anchor; default: go build)
#   ANCHOR_HOLD_SECS   anchor lifetime from script start (default 600)
#   HANDSHAKE_TIMEOUT  seconds to wait for the first handshake (default 300)
#   REALNET_PAIR       pair name, carried in the acknowledgement (default local)
#   GITHUB_RUN_ATTEMPT run attempt, recorded and carried in the acknowledgement
set -eu
umask 077

BIN=${1:?usage: run-peer.sh /path/to/stunmesh}
ROLE=${ROLE:?ROLE=anchor|subject is required}
WG_PRIVATE_KEY=${WG_PRIVATE_KEY:?}
PEER_PUBLIC_KEY=${PEER_PUBLIC_KEY:?}

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
WORK=${RESULT_DIR:-$(mktemp -d)}
mkdir -p "$WORK/pcap"
RESULTS=$WORK/results.env
DAEMON_LOG=$WORK/daemon.log
: > "$RESULTS"
START_TS=$(date +%s)

ENDPOINTS=${ENDPOINTS:-'https://dhtproxy2.jami.net https://dhtproxy3.jami.net'}
# Consumed by the sourced phases.sh.
# shellcheck disable=SC2034
DHT_HOSTS=$(for u in $ENDPOINTS; do h=${u#*://}; echo "${h%%/*}"; done)
STUN_HOST=stun.l.google.com
STUN_PORT=19302

# Consumed by the sourced device.sh and phases.sh.
# shellcheck disable=SC2034
WG_PORT=51820
# shellcheck disable=SC2034
MTU=1280
# shellcheck disable=SC2034
KEEPALIVE=25
# shellcheck disable=SC2034
FWMARK=51820
# shellcheck disable=SC2034
ESCAPE_TABLE=100
CANARY_IP=192.0.2.1
CANARY_PORT=8080
ANCHOR_HOLD_SECS=${ANCHOR_HOLD_SECS:-600}
HANDSHAKE_TIMEOUT=${HANDSHAKE_TIMEOUT:-300}
RUN_ATTEMPT=${GITHUB_RUN_ATTEMPT:-local}
PAIR_ACK=fail

case "$ROLE" in
anchor) MY_OVERLAY=10.99.0.1 PEER_OVERLAY=10.99.0.2 ;;
subject) MY_OVERLAY=10.99.0.2 PEER_OVERLAY=10.99.0.1 ;;
*) echo "unknown ROLE '$ROLE'" >&2; exit 1 ;;
esac

log() { echo "[realnet:$ROLE] $*"; }
rec() { echo "$1=$2" >> "$RESULTS"; log "result: $1=$2"; }

. "$HERE/device.sh"
. "$HERE/netns.sh"
. "$HERE/phases.sh"

detect_os
# The anchor is deliberately the same boring Linux far end for every subject,
# so a failure is attributable to the subject side by construction.
if [ "$ROLE" = anchor ] && [ "$OS" != Linux ]; then
	echo "the anchor role is Linux-only by design, got $OS" >&2
	exit 1
fi

DPID=''
TCPDUMP_PID=''
cleanup() {
	stop_daemon 2>/dev/null || true
	[ -n "$TCPDUMP_PID" ] && $SUDO kill "$TCPDUMP_PID" 2>/dev/null || true
	sleep 1
	device_down
	[ "$HAS_BUNKER" = 1 ] && netns_down
	# tcpdump and the daemon wrote as root; hand the evidence back to the
	# invoking user so artifact upload can read it.
	[ -n "$SUDO" ] && $SUDO chown -R "$(id -u):$(id -g)" "$WORK" 2>/dev/null || true
	log "logs kept in $WORK"
}
trap cleanup EXIT INT TERM

start_daemon() {
	# Backgrounded as a simple command on purpose: backgrounding the ns_exec
	# *function* would fork a subshell that does not forward SIGTERM, so
	# stop_daemon would kill the subshell and orphan the daemon -- exactly
	# the double-daemon bug this replaces. As a simple command the shell's
	# child is sudo (or the binary itself), and sudo does relay signals.
	# $WORK belongs to the invoking user; redirecting the root process's
	# output into it is intended.
	# shellcheck disable=SC2024
	if [ "$HAS_BUNKER" = 1 ]; then
		$SUDO ip netns exec "$NS" "$BIN" -c "$WORK/config.yaml" >> "$DAEMON_LOG" 2>&1 &
	elif [ "$OS" = Windows ]; then
		"$BIN" -c "$WORK/config.yaml" >> "$DAEMON_LOG" 2>&1 &
	else
		# shellcheck disable=SC2024
		$SUDO "$BIN" -c "$WORK/config.yaml" >> "$DAEMON_LOG" 2>&1 &
	fi
	DPID=$!
}
start_canary() { # LISTEN LOG
	# shellcheck disable=SC2024
	ns_exec "$CANARY_BIN" -listen "$1" >"$2" 2>&1 &
	for _ in $(seq 1 20); do
		grep -q CANARY_READY "$2" 2>/dev/null && return 0
		sleep 1
	done
	echo "canary on $1 never came up" >&2
	exit 1
}
stop_daemon() {
	[ -n "$DPID" ] || return 0
	kill "$DPID" 2>/dev/null || $SUDO kill "$DPID" 2>/dev/null || true
	wait "$DPID" 2>/dev/null || true
	# Belt and suspenders against any orphaned daemon: the config path is
	# unique to this run, so the pattern cannot match anything else.
	if command -v pkill >/dev/null 2>&1; then
		$SUDO pkill -f "$WORK/config.yaml" 2>/dev/null || true
	fi
	DPID=''
}

log "OS=$OS work=$WORK"
rec role "$ROLE"
rec os "$OS"
rec run_attempt "$RUN_ATTEMPT"

if [ "$HAS_BUNKER" = 1 ]; then
	netns_up
	log "netns '$NS' up, MASQUERADE via $VETH_HOST"
else
	log "no crash bunker on $OS; running on the host, full-tunnel scenario will be skipped"
fi

# Explicit MTU so the full-MTU ping asserts a known number, explicit keepalive
# so the NAT mapping lifetime is a pinned variable rather than an accident.
KEYFILE=$WORK/wg.key
printf '%s\n' "$WG_PRIVATE_KEY" > "$KEYFILE"
device_up "$KEYFILE" "$PEER_PUBLIC_KEY" "$MY_OVERLAY"
log "$WG_IF up: $MY_OVERLAY, peer allowed-ips $PEER_OVERLAY/32"

# Debug level so the escape evidence ("marking STUN socket ...") and, in
# proxy mode, the endpoint substitution line are both in the log.
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

# Storage preflight, so the report can tell a backend outage from network
# weather from our bug. Any HTTP response counts as reachable.
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

# Size-bounded evidence: headers only (the payload is encrypted and useless
# anyway), and the decrypted inner iperf stream is excluded -- its verdict
# lives in the iperf log and transfer counters, and letting it flood the
# ring would rotate away the split-phase handshake/STUN packets, the only
# part of the capture with diagnostic value. 2x10MB ring as the hard cap.
# Linux only: the '-i any' pseudo-interface exists nowhere else, and the
# capture is post-mortem evidence, never part of any assert.
if [ "$OS" = Linux ]; then
	# shellcheck disable=SC2024
	ns_exec tcpdump -i any -s 160 -U -C 10 -W 2 -Z root \
		-w "$WORK/pcap/realnet.pcap" 'not tcp port 5201' >"$WORK/tcpdump.log" 2>&1 &
	TCPDUMP_PID=$!
fi

if [ "$ROLE" = anchor ]; then
	# Off-overlay canary: static from job start, reachable only once a
	# subject installs a covering route. Replies source from $CANARY_IP,
	# which only that subject's covering AllowedIPs admit.
	if [ -z "${CANARY_BIN:-}" ]; then
		CANARY_BIN=$WORK/canary
		go build -o "$CANARY_BIN" "$HERE/canary"
	fi
	ns_exec ip link add canary0 type dummy
	ns_exec ip addr add "$CANARY_IP/32" dev canary0
	ns_exec ip link set canary0 up
	start_canary "$CANARY_IP:$CANARY_PORT" "$WORK/canary.log"
	rec canary_sha256 "$(sed -n 's/^CANARY_SHA256=//p' "$WORK/canary.log" | head -1)"
	# The acknowledgement listener. Split-tunnel AllowedIPs admit only the
	# overlay /32, so the subject reaches this one in every pair; both
	# instances live in the netns, whose teardown reaps them.
	start_canary "$MY_OVERLAY:$CANARY_PORT" "$WORK/canary-ack.log"

	ns_exec iperf3 -s -D --logfile "$WORK/iperf3.log"
fi

log "starting stunmesh daemon"
start_daemon

case "$ROLE" in
anchor)
	split_tunnel_anchor || true
	# Hold as the far end until the subject's run is over. SUBJECT_DONE_CMD
	# (CI: subject-done.sh) turns the fixed window into a cap: the anchor
	# releases as soon as the subject completes, and only a dead subject or
	# a manual run sleeps the window out.
	left=$((ANCHOR_HOLD_SECS - ($(date +%s) - START_TS)))
	log "holding as far end for up to ${left}s"
	released=full
	while [ "$left" -gt 0 ]; do
		if [ -n "${SUBJECT_DONE_CMD:-}" ] && sh -c "$SUBJECT_DONE_CMD" >/dev/null 2>&1; then
			log "subject run completed; releasing the hold early"
			released=early
			break
		fi
		# 30s keeps the whole realnet layer far inside the shared 1000/h
		# GITHUB_TOKEN budget even with concurrent runs; a faster poll would
		# only save seconds that the job's own teardown time dwarfs anyway.
		sleep 30
		left=$((ANCHOR_HOLD_SECS - ($(date +%s) - START_TS)))
	done
	rec hold_released "$released"
	# The acknowledgement does not end the hold: the subject sends it before
	# its full-tunnel scenario, which still needs this far end. Only the exact
	# line for this pair and attempt counts.
	if grep -qxF "CANARY_ACK=pair=${REALNET_PAIR:-local} attempt=$RUN_ATTEMPT" "$WORK/canary-ack.log"; then
		PAIR_ACK=pass
	else
		log "no acknowledgement from the subject; received: $(grep '^CANARY_ACK=' "$WORK/canary-ack.log" | tr '\n' ' ')"
	fi
	rec pair_ack "$PAIR_ACK"
	;;
subject)
	# Sent whatever the handshake result, so a subject that is up but cannot
	# reach the anchor still fails the pair on both sides.
	split_ok=1
	split_tunnel_subject || split_ok=0
	send_ack
	# The full-tunnel checks are relative to the split-tunnel baseline: they
	# run only if it handshook, so network weather can never masquerade as a
	# routing or escape regression.
	if [ "$split_ok" = 0 ]; then
		log "no baseline handshake; skipping the full-tunnel scenario"
		rec fulltunnel_ran no
		rec fulltunnel_skipped "no baseline handshake"
	elif [ "$HAS_BUNKER" != 1 ]; then
		# Installing a covering default route with no bunker to contain it
		# would blackhole the runner agent itself.
		log "no crash bunker on $OS; skipping the full-tunnel scenario"
		rec fulltunnel_ran no
		rec fulltunnel_skipped "no crash bunker on $OS"
	else
		full_tunnel_subject || true
	fi
	;;
esac

# Final snapshot while the device still exists. assert.sh turns this into the
# peer_endpoint the report checks, accounting for proxy mode.
ns_exec wg show "$WG_IF" > "$WORK/wg-final.log" 2>&1 || true
ns_exec wg show "$WG_IF" dump >> "$WORK/wg-final.log" 2>&1 || true
wgep=$(ns_exec wg show "$WG_IF" endpoints | awk 'NR==1{print $2}')
rec wg_endpoint "${wgep:-(none)}"

stop_daemon
sh "$HERE/assert.sh" "$WORK"

if [ "$PAIR_ACK" != pass ]; then
	echo "pair acknowledgement missing; failing this side of the pair" >&2
	exit 1
fi
