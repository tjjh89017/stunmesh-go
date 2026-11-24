#!/bin/sh
# Crash-bunker netns for the realnet e2e (Linux only). Sourced by run-peer.sh.
#
# Everything under test (wg device, stunmesh, covering full-tunnel routes)
# runs inside a namespace that is MASQUERADEd to the host's real NIC, so the
# WAN side is the real internet (double NAT: netns->host, host->Azure SNAT)
# while the runner agent in the root namespace can never be blackholed by the
# covering route. This is a crash bunker, not a synthetic network.
#
# Requires: $SUDO and ns_exec from device.sh, and WORK, STUN_PORT, rec and log
# from run-peer.sh. Provides: NS, VETH_NS, HOST_IP, netns_up, netns_down,
# hostcap_start, hostcap_stop, hostcap_summary.

NS=${NS:-stunmesh}
VETH_HOST=${VETH_HOST:-veth-sm0}
VETH_NS=${VETH_NS:-veth-sm1}
HOST_NET=${HOST_NET:-10.200.0.0/24}
HOST_IP=${HOST_IP:-10.200.0.1}
NS_IP=${NS_IP:-10.200.0.2}

netns_up() {
	$SUDO ip netns add "$NS"
	# ip netns exec bind-mounts this over /etc/resolv.conf; the host stub
	# resolver (127.0.0.53) is unreachable from inside the namespace.
	$SUDO mkdir -p "/etc/netns/$NS"
	printf 'nameserver 8.8.8.8\nnameserver 1.1.1.1\n' |
		$SUDO tee "/etc/netns/$NS/resolv.conf" >/dev/null

	$SUDO ip link add "$VETH_HOST" type veth peer name "$VETH_NS"
	$SUDO ip link set "$VETH_NS" netns "$NS"
	$SUDO ip addr add "$HOST_IP/24" dev "$VETH_HOST"
	$SUDO ip link set "$VETH_HOST" up
	ns_exec ip link set lo up
	ns_exec ip addr add "$NS_IP/24" dev "$VETH_NS"
	ns_exec ip link set "$VETH_NS" up
	ns_exec ip route add default via "$HOST_IP"

	$SUDO sysctl -qw net.ipv4.ip_forward=1
	$SUDO iptables -t nat -A POSTROUTING -s "$HOST_NET" ! -o "$VETH_HOST" -j MASQUERADE
	# GitHub runners ship docker, which sets the FORWARD policy to DROP.
	$SUDO iptables -A FORWARD -i "$VETH_HOST" -j ACCEPT
	$SUDO iptables -A FORWARD -o "$VETH_HOST" -j ACCEPT
}

netns_down() {
	# Reap whatever is still parked in the namespace (canary, iperf3): the
	# namespace object would survive them, but a manual run should not leave
	# strays behind.
	for _pid in $($SUDO ip netns pids "$NS" 2>/dev/null); do
		$SUDO kill "$_pid" 2>/dev/null || true
	done
	$SUDO iptables -t nat -D POSTROUTING -s "$HOST_NET" ! -o "$VETH_HOST" -j MASQUERADE 2>/dev/null || true
	$SUDO iptables -D FORWARD -i "$VETH_HOST" -j ACCEPT 2>/dev/null || true
	$SUDO iptables -D FORWARD -o "$VETH_HOST" -j ACCEPT 2>/dev/null || true
	$SUDO ip link del "$VETH_HOST" 2>/dev/null || true
	$SUDO ip netns del "$NS" 2>/dev/null || true
	$SUDO rm -rf "/etc/netns/$NS" 2>/dev/null || true
}

# Host-side capture, outside the namespace, on the host's default-route
# interface. Outbound packets appear there after the host's MASQUERADE (host
# address, translated port) and inbound ones before it is undone, so it shows
# what reaches this machine from the peer and from which source port. It is
# evidence only: a capture that cannot start records unknown, never a failure.
# WireGuard data packets larger than a keepalive are dropped so the iperf3
# bulk stays out; handshakes, keepalives and STUN stay in. Records are at
# most 112 bytes and the ring is 2x5MB.
HOSTCAP_PID=''
HOSTCAP_IF=''
HOSTCAP_ERR=''
HOSTCAP_FILE=host-outer.pcap
HOSTCAP_FILTER='ip and udp and not port 53 and not port 5353 and not port 123 and not port 443 and not portrange 67-68 and not (udp[8:4] = 0x04000000 and udp[4:2] > 64)'

hostcap_start() {
	HOSTCAP_IF=$(ip -4 route show default 2>/dev/null |
		awk '{for (i = 1; i < NF; i++) if ($i == "dev") { print $(i + 1); exit }}')
	if [ -z "$HOSTCAP_IF" ]; then
		HOSTCAP_ERR='no default-route interface'
	elif ! command -v tcpdump >/dev/null 2>&1; then
		HOSTCAP_ERR='tcpdump missing'
	else
		# -Z root: tcpdump otherwise drops to its own user before it opens an
		# output file, and that user cannot write the runner-owned 0700 dir.
		# shellcheck disable=SC2024
		$SUDO tcpdump -i "$HOSTCAP_IF" -nn -s 96 -U -C 5 -W 2 -Z root \
			-w "$WORK/pcap/$HOSTCAP_FILE" "$HOSTCAP_FILTER" >"$WORK/tcpdump-host.log" 2>&1 &
		HOSTCAP_PID=$!
		for _ in 1 2 3 4 5; do
			grep -q 'listening on' "$WORK/tcpdump-host.log" 2>/dev/null && return 0
			sleep 1
		done
		HOSTCAP_ERR="tcpdump did not start: $(tail -n 1 "$WORK/tcpdump-host.log" 2>/dev/null)"
		hostcap_stop
	fi
	log "warning: no host-side capture ($HOSTCAP_ERR)"
	return 0
}

hostcap_stop() {
	[ -n "$HOSTCAP_PID" ] || return 0
	$SUDO kill "$HOSTCAP_PID" 2>/dev/null || true
	wait "$HOSTCAP_PID" 2>/dev/null || true
	HOSTCAP_PID=''
}

# hostcap_read FILTER -- the matching packets of every ring file, one line
# each. The files are root-owned and tcpdump is AppArmor-confined on some
# runners, so the bytes arrive through a pipe.
hostcap_read() {
	for _hc_f in "$WORK/pcap/$HOSTCAP_FILE"[0-9]; do
		[ -e "$_hc_f" ] || continue
		$SUDO cat "$_hc_f" | tcpdump -nn -r - "$1" 2>/dev/null || true
	done
}

# hostcap_ports sport|pair -- "port:count" (or "sport>dport:count") from
# hostcap_read lines, most frequent first, at most 8 entries.
hostcap_ports() {
	awk -v m="$1" '{
		for (i = 1; i < NF; i++) if ($i == "IP") {
			s = $(i + 1); d = $(i + 3); sub(/:$/, "", d)
			sub(/.*\./, "", s); sub(/.*\./, "", d)
			print (m == "pair" ? s ">" d : s); break
		}
	}' | sort | uniq -c | sort -k1,1nr -k2 |
		awk 'NR <= 8 {printf "%s%s:%s", (NR > 1 ? "," : ""), $2, $1}
			END {if (NR > 8) printf ",..."; if (NR == 0) printf "none"; print ""}'
}

# hostcap_summary PEER_ENDPOINT -- record the hostcap_* lines. An empty
# endpoint leaves the peer lines unknown.
hostcap_summary() {
	if [ -z "$HOSTCAP_ERR" ] && ! ls "$WORK/pcap/$HOSTCAP_FILE"[0-9] >/dev/null 2>&1; then
		HOSTCAP_ERR='no capture file'
	fi
	if [ -n "$HOSTCAP_ERR" ]; then
		rec hostcap "unknown ($HOSTCAP_ERR)"
		for _hc_k in in_peer_ip in_peer_ip_ports in_peer_endpoint out_peer_endpoint \
			out_peer_sports out_stun_sports; do
			rec "hostcap_$_hc_k" unknown
		done
		return 0
	fi
	rec hostcap "$HOSTCAP_IF"
	_hc_ip=${1%:*}
	_hc_port=${1##*:}
	if [ -n "$1" ]; then
		_hc_in=$(hostcap_read "src host $_hc_ip")
		rec hostcap_in_peer_ip "$(printf '%s' "$_hc_in" | grep -c . || true)"
		rec hostcap_in_peer_ip_ports "$(printf '%s\n' "$_hc_in" | hostcap_ports pair)"
		rec hostcap_in_peer_endpoint \
			"$(hostcap_read "src host $_hc_ip and src port $_hc_port" | grep -c . || true)"
		_hc_out=$(hostcap_read "dst host $_hc_ip and dst port $_hc_port")
		rec hostcap_out_peer_endpoint "$(printf '%s' "$_hc_out" | grep -c . || true)"
		rec hostcap_out_peer_sports "$(printf '%s\n' "$_hc_out" | hostcap_ports sport)"
	else
		for _hc_k in in_peer_ip in_peer_ip_ports in_peer_endpoint out_peer_endpoint \
			out_peer_sports; do
			rec "hostcap_$_hc_k" "unknown (no peer endpoint)"
		done
	fi
	rec hostcap_out_stun_sports "$(hostcap_read "dst port $STUN_PORT" | hostcap_ports sport)"
	return 0
}
