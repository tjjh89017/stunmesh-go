#!/bin/sh
# Two-interface stunmesh e2e. Creates two WireGuard interfaces that are each
# other's peer, runs `stunmesh --oneshot` on both against a shared opendht
# store, and hands the logs to assert.sh.
#
# Outside Windows the peers publish through a plugin chain, a mirror over two
# traced legs: trace -> shell file store (filestore.sh, read first) and
# trace -> opendht. Each trace writes $WORK/<leg><slot>.jsonl for assert.sh.
# The shell plugin execs /bin/sh, which a native Windows binary does not
# have, so Windows runs the plain scenario with opendht alone.
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
CHAIN=1
[ "$OS" = Windows ] && CHAIN=0
STORE=$WORK/store

# The binary must not execute from the sshfs mount of the FreeBSD VM.
LOCALBIN=$WORK/stunmesh
[ "$OS" = Windows ] && LOCALBIN=$LOCALBIN.exe
cp -p "$BIN" "$LOCALBIN"
BIN=$LOCALBIN

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

# shellcheck source=win_tunnel.sh
. "$HERE/win_tunnel.sh"

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
		if ! win_start_tunnel "$name" "$conf"; then
			echo "tunnel $name never came up" >&2
			exit 1
		fi
		;;
	*) echo "unsupported OS: $OS" >&2; exit 1 ;;
	esac
	if [ "$OS" != Windows ]; then
		# On Ubuntu 26.04 an AppArmor profile confines wg to /etc/wireguard and
		# denies dac_override, so the key must arrive through a pipe root creates.
		# shellcheck disable=SC2016
		$SUDO sh -c 'cat "$1" | wg set "$0" private-key /dev/stdin \
			listen-port "$2" peer "$3" allowed-ips "$4"' \
			"$name" "$keyfile" "$port" "$peer" "$allowed"
	fi
	eval "IF$slot=\$name"
}

write_config() { # IF PEER_PUB FILE SLOT [cycle|undefined]
	name=$1; peer=$2; out=$3; slot=$4; mode=${5:-}
	use=dht
	[ "$CHAIN" = 1 ] && use=chain
	shnext=files; dhtnext=dht
	[ "$mode" = cycle ] && dhtnext=chain
	[ "$mode" = undefined ] && shnext=missing
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
		if [ "$CHAIN" = 1 ]; then
			echo "  chain:"
			echo "    type: builtin"
			echo "    name: mirror"
			echo "    stores: [shleg, dhtleg]"
			echo "  shleg:"
			echo "    type: builtin"
			echo "    name: trace"
			echo "    next: $shnext"
			echo "    file: \"$WORK/shleg$slot.jsonl\""
			echo "  dhtleg:"
			echo "    type: builtin"
			echo "    name: trace"
			echo "    next: $dhtnext"
			echo "    file: \"$WORK/dhtleg$slot.jsonl\""
			echo "  files:"
			echo "    type: shell"
			echo "    command: /bin/sh"
			echo "    args: [\"$HERE/filestore.sh\", \"$STORE\"]"
		fi
		echo "interfaces:"
		echo "  $name:"
		echo "    protocol: ipv4"
		echo "    peers:"
		echo "      peer:"
		echo "        public_key: \"$peer\""
		echo "        plugin: $use"
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

write_config "$IF0" "$Bpub" "$WORK/cfg0.yaml" 0
write_config "$IF1" "$Apub" "$WORK/cfg1.yaml" 1

if [ "$CHAIN" = 1 ]; then
	mkdir "$STORE"
	export E2E_STORE="$STORE"
	export E2E_WORK="$WORK"
	# expect_startup_failure MODE TEXT: the broken chain must stop startup
	# with TEXT on stderr.
	expect_startup_failure() {
		mode=$1; text=$2
		log "negative check: $mode chain must stop startup"
		write_config "$IF0" "$Bpub" "$WORK/$mode.yaml" 9 "$mode"
		# shellcheck disable=SC2024
		if $SUDO "$BIN" --oneshot -c "$WORK/$mode.yaml" > "$WORK/$mode.out" 2> "$WORK/$mode.err"; then
			echo "FAIL: stunmesh started with a $mode chain" >&2
			exit 1
		fi
		if ! grep -q "$text" "$WORK/$mode.err"; then
			echo "FAIL: no '$text' in the stderr of the $mode run:" >&2
			cat "$WORK/$mode.out" "$WORK/$mode.err" >&2
			exit 1
		fi
		log "ok: $(grep -m1 "$text" "$WORK/$mode.err")"
	}
	expect_startup_failure cycle 'plugin cycle'
	expect_startup_failure undefined 'is not defined'
fi

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
	rc0=0; rc1=0
	wait "$j0" || rc0=$?
	wait "$j1" || rc1=$?
	log "oneshot exit: wg0=$rc0 wg1=$rc1"
	if [ "$rc0" != 0 ] || [ "$rc1" != 0 ]; then
		echo "[e2e] daemon failed; logs follow" >&2
		for f in "$WORK/if0.log" "$WORK/if1.log"; do
			echo "----- $f -----" >&2
			cat "$f" >&2 || true
		done
		case "$OS" in
		FreeBSD | Darwin) dmesg | tail -n 20 >&2 || true ;;
		esac
		[ "$rc0" != 0 ] && exit "$rc0"
		exit "$rc1"
	fi
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
