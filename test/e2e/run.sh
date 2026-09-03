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
# Git Bash reports MINGW64_NT-<ver>/MSYS_NT-<ver>; fold to one name.
case "$OS" in MINGW* | MSYS* | CYGWIN*) OS=Windows ;; esac
# GitHub's Linux/macOS runners are non-root with passwordless sudo; the FreeBSD
# VM runs as root and may lack sudo. Windows has no sudo at all -- the runner
# shell is already elevated, which stunmesh requires there. Resolve once and
# share with assert.sh.
if [ "$OS" = Windows ] || [ "$(id -u)" = 0 ]; then SUDO=''; else SUDO='sudo'; fi
export SUDO
export E2E_OS="$OS"
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
		Windows) MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
			wireguard.exe /uninstalltunnelservice "$name" 2>/dev/null || true ;;
		esac
	done
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# win_install_tunnel NAME CONF: runs installtunnelservice (output captured to
# $WORK for win_tunnel_diag) and polls `wg show` for up to 60s. Returns
# non-zero on install failure or timeout; never lets set -e abort mid-poll.
win_install_tunnel() {
	name=$1; conf=$2
	# MSYS_NO_PATHCONV stops Git Bash rewriting /installtunnelservice as
	# a POSIX path; the conf path is pre-converted with cygpath instead.
	if ! MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
		wireguard.exe /installtunnelservice "$(cygpath -w "$conf")" \
		>"$WORK/install-$name.log" 2>&1; then
		return 1
	fi
	for _ in $(seq 1 60); do
		wg show "$name" >/dev/null 2>&1 && return 0
		sleep 1
	done
	return 1
}

# win_tunnel_diag NAME: dumps everything useful for telling a runner-side
# WireGuardNT failure apart from a slow driver load. Every command is
# tolerant (|| true) so a missing tool never masks the real failure.
win_tunnel_diag() {
	name=$1
	echo "[e2e] diag: installtunnelservice output for $name:" >&2
	cat "$WORK/install-$name.log" >&2 2>/dev/null || true
	echo "[e2e] diag: sc query WireGuardTunnel\$$name:" >&2
	sc query "WireGuardTunnel\$$name" >&2 || true
	echo "[e2e] diag: sc query WireGuardManager:" >&2
	sc query WireGuardManager >&2 || true
	echo "[e2e] diag: recent WireGuard Application event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='Application'; ProviderName='WireGuard*'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
	echo "[e2e] diag: recent Service Control Manager System event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
}

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
		for _ in $(seq 1 60); do [ -s "$namefile" ] && break; sleep 1; done
		if ! $SUDO test -s "$namefile"; then
			echo "wireguard-go never named a utun; its log:" >&2
			$SUDO cat "$WORK/wggo$slot.log" >&2 || true
			exit 1
		fi
		name=$($SUDO cat "$namefile")
		eval "PID$slot=\$(pgrep -f \"$wgg utun\")"
		;;
	Windows)
		# WireGuardNT: the tunnel service owns the adapter and applies the
		# whole config from the .conf (name = file basename), so key, port
		# and peer are set here rather than via `wg set` below. stunmesh's
		# UDP proxy is the outward socket; the adapter only talks loopback.
		name=stunmesh$slot
		conf=$WORK/$name.conf
		{
			echo "[Interface]"
			echo "PrivateKey = $(cat "$keyfile")"
			echo "ListenPort = $port"
			echo ""
			echo "[Peer]"
			echo "PublicKey = $peer"
			echo "AllowedIPs = $allowed"
		} > "$conf"
		if ! win_install_tunnel "$name" "$conf"; then
			win_tunnel_diag "$name"
			log "retrying: uninstalling and reinstalling $name"
			MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
				wireguard.exe /uninstalltunnelservice "$name" >/dev/null 2>&1 || true
			sleep 5
			if ! win_install_tunnel "$name" "$conf"; then
				win_tunnel_diag "$name"
				echo "tunnel $name never came up" >&2
				exit 1
			fi
		fi
		;;
	*) echo "unsupported OS: $OS" >&2; exit 1 ;;
	esac
	if [ "$OS" != Windows ]; then
		$SUDO wg set "$name" private-key "$keyfile" listen-port "$port" \
			peer "$peer" allowed-ips "$allowed"
	fi
	eval "IF$slot=\$name"
}

write_config() { # IF PEER_PUB > FILE
	name=$1; peer=$2; out=$3
	# Windows asserts on the proxy's debug-level endpoint-substitution log
	# line (see assert.sh check 3), so it needs the lower level.
	lvl=info
	[ "$OS" = Windows ] && lvl=debug
	{
		echo "log: {level: $lvl, format: json}"
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

# The two processes never wait for each other, so when one side's STUN
# discovery is delayed past the other's last establish round (seen on the
# macOS runners), a single pass can end with a one-sided endpoint. Re-running
# both oneshots lets the fast side pick up the slow side's published data;
# a real product bug keeps failing across every attempt. Logs are truncated
# each pass so assert.sh only ever sees the final round.
run_oneshots() {
	# $WORK is created by mktemp as the current user, so redirecting the root
	# process's output into it as the current user is intended and correct.
	# shellcheck disable=SC2024
	$SUDO "$BIN" --oneshot -c "$WORK/cfg0.yaml" > "$WORK/if0.log" 2>&1 &
	j0=$!
	# shellcheck disable=SC2024
	$SUDO "$BIN" --oneshot -c "$WORK/cfg1.yaml" > "$WORK/if1.log" 2>&1 &
	j1=$!
	wait "$j0"; wait "$j1"
}

attempt=1; max_attempts=3
log "running --oneshot on both"
run_oneshots
log "both finished; asserting"
until sh "$HERE/assert.sh" "$IF0" "$WORK/if0.log" "$IF1" "$WORK/if1.log"; do
	attempt=$((attempt + 1))
	if [ "$attempt" -gt "$max_attempts" ]; then
		log "assertions still failing after $max_attempts attempts"
		exit 1
	fi
	log "assertions failed; re-running --oneshot (attempt $attempt/$max_attempts)"
	run_oneshots
	log "both finished; asserting"
done
