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

echo "== 1. no error-level log lines =="
for pair in "$IF0:$LOG0" "$IF1:$LOG1"; do
	log=${pair#*:}
	n=$(jq -c 'select(.level=="error")' "$log" | wc -l | tr -d ' ')
	if [ "$n" != "0" ]; then
		echo "FAIL: ${pair%%:*} logged $n error(s):"
		jq -c 'select(.level=="error")|{message,error}' "$log"
		fail=1
	else
		echo "ok: ${pair%%:*} clean"
	fi
done

echo "== 2. STUN IP matches api.ipify.org =="
oracle=$(curl -4 -sS -m 15 https://api.ipify.org)
d0=$(discovered "$LOG0"); d1=$(discovered "$LOG1")
for pair in "$IF0:$d0" "$IF1:$d1"; do
	ip=${pair#*:}; ip=${ip%:*}
	if [ "$ip" = "$oracle" ]; then
		echo "ok: ${pair%%:*} discovered $ip == oracle"
	else
		echo "FAIL: ${pair%%:*} discovered '$ip', oracle '$oracle'"
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
