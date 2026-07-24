#!/bin/sh
# Two-interface stunmesh e2e. Creates two WireGuard interfaces that are each
# other's peer, runs `stunmesh --oneshot` on both against a shared opendht
# store, and hands the logs to assert.sh.
#
# stunmesh attaches to an existing device (it never creates one), so the wg
# interface, keys, listen port and peer must be in place before it runs. Only
# the interface *creation* differs per OS; `wg set`/`wg show` are identical
# everywhere.
#
# Usage: run.sh /path/to/stunmesh
# Env:   ENDPOINTS  newline/space list of opendht proxy URLs
#                   (default: dhtproxy2 then dhtproxy3, dhtproxy3 as failover)
set -eu
umask 077  # keep the generated private keys off world-readable

BIN=${1:?usage: run.sh /path/to/stunmesh}
HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
WORK=$(mktemp -d)
OS=$(uname -s)
# GitHub's Linux/macOS runners are non-root with passwordless sudo; the FreeBSD
# VM runs as root and may lack sudo. Resolve once and share with assert.sh.
[ "$(id -u)" = 0 ] && SUDO='' || SUDO='sudo'
export SUDO
# Space- or newline-separated opendht proxy URLs; the second is failover.
ENDPOINTS=${ENDPOINTS:-'https://dhtproxy2.jami.net https://dhtproxy3.jami.net'}

PORT0=51820; PORT1=51821
ALLOWED0=10.66.0.1/32; ALLOWED1=10.66.0.2/32  # placeholder allowed-ips per peer
IF0=""; IF1=""   # resolved names (utunN on macOS)

log() { echo "[e2e] $*"; }

cleanup() {
	for slot in 0 1; do
		eval "name=\$IF$slot"
		[ -n "$name" ] || continue
		case "$OS" in
		Linux)   $SUDO ip link del "$name" 2>/dev/null || true ;;
		FreeBSD) $SUDO ifconfig "$name" destroy 2>/dev/null || true ;;
		Darwin)  eval "pid=\$PID$slot"; [ -n "${pid:-}" ] && $SUDO kill "$pid" 2>/dev/null || true ;;
		esac
	done
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# create_iface SLOT PRIVKEY_FILE PORT PEER_PUB PEER_ALLOWED
# Sets IF$SLOT (and, on Darwin, PID$SLOT) to the resolved interface. No tunnel
# address is assigned: stunmesh only needs the device's key, listen port and
# peer, and STUN runs on its own raw socket, so the overlay IP is irrelevant.
create_iface() {
	slot=$1; keyfile=$2; port=$3; peer=$4; allowed=$5
	case "$OS" in
	Linux)
		name=wg$slot
		$SUDO ip link add "$name" type wireguard
		$SUDO ip link set "$name" up
		;;
	FreeBSD)
		name=$($SUDO ifconfig wg create)
		$SUDO ifconfig "$name" up
		;;
	Darwin)
		namefile=$WORK/tun$slot.name
		wgg=$(command -v wireguard-go) || { echo "wireguard-go not in PATH" >&2; exit 1; }
		log "starting $wgg for slot $slot"
		# Run in the foreground under sudo but backgrounded by the shell, so
		# root keeps the utun while PATH still resolves the brew binary. It
		# writes the chosen utunN to namefile once up.
		$SUDO env WG_TUN_NAME_FILE="$namefile" WG_PROCESS_FOREGROUND=1 \
			"$wgg" utun >"$WORK/wggo$slot.log" 2>&1 &
		for _ in $(seq 1 30); do [ -s "$namefile" ] && break; sleep 0.5; done
		if ! $SUDO test -s "$namefile"; then
			echo "wireguard-go never named a utun; its log:" >&2
			$SUDO cat "$WORK/wggo$slot.log" >&2 || true
			exit 1
		fi
		name=$($SUDO cat "$namefile")
		eval "PID$slot=\$(pgrep -f \"$wgg utun\")"
		;;
	*) echo "unsupported OS: $OS" >&2; exit 1 ;;
	esac
	$SUDO wg set "$name" private-key "$keyfile" listen-port "$port" \
		peer "$peer" allowed-ips "$allowed"
	eval "IF$slot=\$name"
}

write_config() { # IF PEER_PUB > FILE
	name=$1; peer=$2; out=$3
	{
		echo "log: {level: info, format: json}"
		echo "stun: {addresses: [\"stun.l.google.com:19302\"]}"
		echo "plugins:"
		echo "  dht:"
		echo "    type: builtin"
		echo "    name: opendht"
		echo "    endpoints:"
		for url in $ENDPOINTS; do echo "      - $url"; done
		echo "interfaces:"
		echo "  $name:"
		echo "    protocol: ipv4"
		echo "    peers:"
		echo "      peer:"
		echo "        public_key: \"$peer\""
		echo "        plugin: dht"
		echo "        protocol: ipv4"
	} > "$out"
}

log "OS=$OS  work=$WORK"
Apriv=$WORK/a.key; Bpriv=$WORK/b.key
wg genkey > "$Apriv"; wg genkey > "$Bpriv"
Apub=$(wg pubkey < "$Apriv"); Bpub=$(wg pubkey < "$Bpriv")
log "keys generated; creating interfaces"

create_iface 0 "$Apriv" "$PORT0" "$Bpub" "$ALLOWED1"
create_iface 1 "$Bpriv" "$PORT1" "$Apub" "$ALLOWED0"
log "interfaces up: $IF0 (peer $Bpub), $IF1 (peer $Apub)"

write_config "$IF0" "$Bpub" "$WORK/cfg0.yaml"
write_config "$IF1" "$Apub" "$WORK/cfg1.yaml"

log "running --oneshot on both"
# $WORK is created by mktemp as the current user, so redirecting the root
# process's output into it as the current user is intended and correct.
# shellcheck disable=SC2024
$SUDO "$BIN" --oneshot -c "$WORK/cfg0.yaml" > "$WORK/if0.log" 2>&1 &
j0=$!
# shellcheck disable=SC2024
$SUDO "$BIN" --oneshot -c "$WORK/cfg1.yaml" > "$WORK/if1.log" 2>&1 &
j1=$!
wait "$j0"; wait "$j1"
log "both finished; asserting"

sh "$HERE/assert.sh" "$IF0" "$WORK/if0.log" "$IF1" "$WORK/if1.log"
