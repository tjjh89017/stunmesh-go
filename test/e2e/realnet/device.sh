#!/bin/sh
# Per-OS plumbing for the realnet e2e: the WireGuard device, the network
# context commands run in, and the ping spellings that differ everywhere.
# Sourced by run-peer.sh.
#
# Only device creation and a few flag spellings are platform-specific; `wg
# set` and `wg show` are identical on all of them. The one structural
# difference is the crash bunker: on Linux everything runs in a namespace
# NAT'd to the real NIC, which is what makes a covering default route safe to
# install on a CI runner. No other platform here has an equivalent, so their
# commands run directly on the host and the full-tunnel scenario is skipped
# rather than blackholing the runner agent.

detect_os() {
	OS=$(uname -s)
	# Git Bash reports MINGW64_NT-<ver>/MSYS_NT-<ver>; fold to one name.
	case "$OS" in MINGW* | MSYS* | CYGWIN*) OS=Windows ;; esac
	# Hosted Linux/macOS runners are non-root with passwordless sudo; the
	# FreeBSD VM runs as root and may lack sudo; the Windows runner shell is
	# already elevated, which stunmesh requires there.
	if [ "$OS" = Windows ] || [ "$(id -u)" = 0 ]; then SUDO=''; else SUDO='sudo'; fi
	export SUDO
	case "$OS" in Linux) HAS_BUNKER=1 ;; *) HAS_BUNKER=0 ;; esac
}

# ns_exec CMD... -- run in the network context under test.
ns_exec() {
	if [ "$HAS_BUNKER" = 1 ]; then
		$SUDO ip netns exec "$NS" "$@"
	elif [ "$OS" = Windows ]; then
		"$@"
	else
		$SUDO "$@"
	fi
}

# device_up KEYFILE PEER_PUB MY_OVERLAY -- create the device in its
# split-tunnel baseline shape and set WG_IF to the resolved name.
device_up() {
	_keyfile=$1; _peer=$2; _addr=$3

	case "$OS" in
	Linux)
		WG_IF=wg0
		ns_exec ip link add "$WG_IF" type wireguard
		;;
	FreeBSD)
		WG_IF=$($SUDO ifconfig wg create)
		;;
	Darwin)
		_namefile=$WORK/utun.name
		_wgg=$(command -v wireguard-go) || { echo "wireguard-go not in PATH" >&2; exit 1; }
		# Foreground under sudo but backgrounded by the shell, so root keeps
		# the utun while PATH still resolves the brew binary. It writes the
		# chosen utunN to namefile once up.
		$SUDO env WG_TUN_NAME_FILE="$_namefile" WG_PROCESS_FOREGROUND=1 \
			"$_wgg" utun >"$WORK/wireguard-go.log" 2>&1 &
		for _ in $(seq 1 60); do [ -s "$_namefile" ] && break; sleep 1; done
		$SUDO test -s "$_namefile" || {
			echo "wireguard-go never named a utun; its log:" >&2
			$SUDO cat "$WORK/wireguard-go.log" >&2 || true
			exit 1
		}
		WG_IF=$($SUDO cat "$_namefile")
		WGGO_PID=$(pgrep -f "$_wgg utun" || true)
		;;
	Windows)
		# WireGuardNT: the tunnel service owns the adapter and applies the
		# whole config from the .conf (name = file basename), so key, port,
		# address and peer are set here rather than via `wg set`. stunmesh's
		# UDP proxy is the outward socket; the adapter only talks loopback.
		WG_IF=stunmesh0
		_conf=$WORK/$WG_IF.conf
		{
			echo "[Interface]"
			echo "PrivateKey = $(cat "$_keyfile")"
			echo "ListenPort = $WG_PORT"
			echo "Address = $_addr/24"
			echo "MTU = $MTU"
			echo ""
			echo "[Peer]"
			echo "PublicKey = $_peer"
			echo "AllowedIPs = $PEER_OVERLAY/32"
			echo "PersistentKeepalive = $KEEPALIVE"
		} > "$_conf"
		# MSYS_NO_PATHCONV stops Git Bash rewriting /installtunnelservice as
		# a POSIX path; the conf path is pre-converted with cygpath instead.
		MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
			wireguard.exe /installtunnelservice "$(cygpath -w "$_conf")"
		for _ in $(seq 1 60); do
			wg show "$WG_IF" >/dev/null 2>&1 && break
			sleep 1
		done
		wg show "$WG_IF" >/dev/null 2>&1 || { echo "tunnel $WG_IF never came up" >&2; exit 1; }
		return 0
		;;
	*) echo "unsupported OS: $OS" >&2; exit 1 ;;
	esac

	# $WORK belongs to the invoking user, so feeding the key on stdin keeps
	# it out of the process table and off a root-readable path.
	# shellcheck disable=SC2024
	ns_exec wg set "$WG_IF" private-key /dev/stdin \
		listen-port "$WG_PORT" \
		peer "$_peer" allowed-ips "$PEER_OVERLAY/32" \
		persistent-keepalive "$KEEPALIVE" < "$_keyfile"

	case "$OS" in
	Linux)
		ns_exec ip addr add "$_addr/24" dev "$WG_IF"
		ns_exec ip link set "$WG_IF" mtu "$MTU" up
		;;
	FreeBSD)
		ns_exec ifconfig "$WG_IF" inet "$_addr/24" mtu "$MTU" up
		;;
	Darwin)
		# utun is point-to-point, so the peer overlay address is the far end
		# of the link rather than a subnet member.
		ns_exec ifconfig "$WG_IF" inet "$_addr" "$PEER_OVERLAY" \
			netmask 255.255.255.255 mtu "$MTU" up
		;;
	esac
}

device_down() {
	[ -n "${WG_IF:-}" ] || return 0
	case "$OS" in
	Linux) ns_exec ip link del "$WG_IF" 2>/dev/null || true ;;
	FreeBSD) $SUDO ifconfig "$WG_IF" destroy 2>/dev/null || true ;;
	Darwin) [ -n "${WGGO_PID:-}" ] && $SUDO kill "$WGGO_PID" 2>/dev/null || true ;;
	Windows) MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
		wireguard.exe /uninstalltunnelservice "$WG_IF" 2>/dev/null || true ;;
	esac
}

# net_ping IP -- three echo requests. -W is a per-reply timeout in seconds on
# Linux but milliseconds on the BSDs, so use their -t deadline instead.
net_ping() {
	case "$OS" in
	Linux) ns_exec ping -c 3 -W 2 "$1" ;;
	Windows) ping -n 3 -w 2000 "$1" ;;
	*) ns_exec ping -c 3 -t 6 "$1" ;;
	esac
}

# net_ping_df SIZE IP -- don't-fragment ping at a payload size chosen to fill
# the overlay MTU exactly.
net_ping_df() {
	case "$OS" in
	Linux) ns_exec ping -M "do" -c 3 -W 2 -s "$1" "$2" ;;
	Windows) ping -n 3 -w 2000 -f -l "$1" "$2" ;;
	*) ns_exec ping -c 3 -t 6 -D -s "$1" "$2" ;;
	esac
}
