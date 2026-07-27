#!/bin/sh
# Crash-bunker netns for the realnet e2e (Linux only). Sourced by run-peer.sh.
#
# Everything under test (wg device, stunmesh, covering full-tunnel routes)
# runs inside a namespace that is MASQUERADEd to the host's real NIC, so the
# WAN side is the real internet (double NAT: netns->host, host->Azure SNAT)
# while the runner agent in the root namespace can never be blackholed by the
# covering route. This is a crash bunker, not a synthetic network.
#
# Requires: $SUDO and ns_exec from device.sh. Provides: NS, VETH_NS,
# HOST_IP, netns_up, netns_down.

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
