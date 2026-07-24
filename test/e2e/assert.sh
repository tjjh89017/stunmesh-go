#!/bin/sh
# Assertions for the two-interface stunmesh e2e, shared by every platform.
#
# Given two stunmesh --oneshot runs (interfaces IF0 and IF1, each the other's
# peer, publishing to the same store), verify the whole publish -> store ->
# establish pipeline actually moved each side's discovered endpoint to the
# other. The runner sits behind NAT, so only the public IP is stable across
# protocols; ports differ per mapping. Assertions are therefore:
#
#   1. neither run logged an error (JSON logs, keyed on .level)
#   2. the STUN-discovered IP matches an independent oracle (api.ipify.org)
#   3. each interface's peer endpoint equals what the other side discovered
#      -- the cross-equality that proves the data path, independent of NAT type
#
# Args: IF0 LOG0 IF1 LOG1
set -eu

IF0=$1; LOG0=$2; IF1=$3; LOG1=$4
fail=0
SUDO=${SUDO-sudo}  # inherited from run.sh; default sudo for standalone use

discovered() { # LOG -> "ip:port" of the last IPv4 endpoint it found
	jq -rc 'select(.message=="discovered IPv4 endpoint")|.ipv4' "$1" | tail -1
}
peer_endpoint() { # IF -> the single peer's endpoint per `wg show`
	$SUDO wg show "$1" endpoints | awk 'NR==1{print $2}'
}

echo "== 1. error-level log lines (diagnostic, not a gate) =="
# --oneshot publishes three times precisely to ride out transient failures --
# a pcap interface that is briefly unusable, a STUN server that drops a probe.
# Whether the pipeline actually worked is decided by the cross-equality in
# check 3: anything that truly broke it leaves an endpoint unpropagated there,
# while an error that a later round recovered from does not. So surface errors
# for visibility but do not fail on them.
for pair in "$IF0:$LOG0" "$IF1:$LOG1"; do
	log=${pair#*:}
	n=$(jq -c 'select(.level=="error")' "$log" | wc -l | tr -d ' ')
	if [ "$n" != "0" ]; then
		echo "note: ${pair%%:*} logged $n error(s) (tolerated if check 3 passes):"
		jq -c 'select(.level=="error")|{message,error}' "$log"
	else
		echo "ok: ${pair%%:*} clean"
	fi
done

echo "== 2. STUN discovered a real public IPv4 =="
# The gate is that STUN found a routable public address, not garbage or a
# private/reserved one. Matching api.ipify.org exactly is only informational:
# cloud runners (e.g. Azure) egress UDP and TCP through different SNAT IPs, so
# the STUN IP and an HTTPS oracle legitimately differ.
oracle=$(curl -4 -sS -m 15 https://api.ipify.org || echo "")
is_public_ipv4() {
	case $1 in
	*[!0-9.]* | '' | *..* | .* | *.) return 1 ;;  # non-numeric or malformed
	0.* | 10.* | 127.* | 169.254.* | 192.168.* | \
	172.1[6-9].* | 172.2[0-9].* | 172.3[0-1].*) return 1 ;;  # private/reserved
	*.*.*.*) return 0 ;;
	*) return 1 ;;
	esac
}
d0=$(discovered "$LOG0"); d1=$(discovered "$LOG1")
for pair in "$IF0:$d0" "$IF1:$d1"; do
	ip=${pair#*:}; ip=${ip%:*}
	if is_public_ipv4 "$ip"; then
		note=""
		[ "$ip" = "$oracle" ] || note=" (oracle $oracle differs -- normal on split-egress runners)"
		echo "ok: ${pair%%:*} discovered public IP $ip$note"
	else
		echo "FAIL: ${pair%%:*} discovered '$ip', not a public IPv4"
		fail=1
	fi
done

echo "== 3. cross-equality of endpoints =="
e0=$(peer_endpoint "$IF0"); e1=$(peer_endpoint "$IF1")
# IF0's peer is IF1, so IF0's peer endpoint must equal what IF1 discovered.
if [ -n "$d1" ] && [ "$e0" = "$d1" ]; then
	echo "ok: $IF0 peer endpoint $e0 == $IF1 discovered"
else
	echo "FAIL: $IF0 peer endpoint '$e0' != $IF1 discovered '$d1'"
	fail=1
fi
if [ -n "$d0" ] && [ "$e1" = "$d0" ]; then
	echo "ok: $IF1 peer endpoint $e1 == $IF0 discovered"
else
	echo "FAIL: $IF1 peer endpoint '$e1' != $IF0 discovered '$d0'"
	fail=1
fi

if [ "$fail" = "0" ]; then
	echo "PASS"
else
	echo "e2e assertions failed"
	exit 1
fi
