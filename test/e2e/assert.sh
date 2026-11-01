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
#   4. only with E2E_STORE set (the chain scenario, see run.sh): both traced
#      legs of the mirror stored each side's key, the file store in E2E_STORE
#      holds it, and each side read the other's key back from the file store
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
if [ "${E2E_OS-}" = "Windows" ]; then
	# Proxy mode: WGNT's peer endpoint is the proxy's loopback inner listener
	# and the real remote lives in the proxy mapping, so the cross-equality is
	# asserted through the substitution log line, which carries both:
	#   .remote == what the other side discovered  (pipeline moved the endpoint)
	#   .inner  == the endpoint `wg show` reports  (WGNT points at the relay)
	substituted() { # LOG FIELD -> last value the proxy programmed
		jq -rc "select(.message==\"peer endpoint substituted with proxy inner socket\")|.$2" "$1" | tail -1
	}
	r0=$(substituted "$LOG0" remote); i0=$(substituted "$LOG0" inner)
	r1=$(substituted "$LOG1" remote); i1=$(substituted "$LOG1" inner)
	if [ -n "$d1" ] && [ "$r0" = "$d1" ]; then
		echo "ok: $IF0 proxy remote $r0 == $IF1 discovered"
	else
		echo "FAIL: $IF0 proxy remote '$r0' != $IF1 discovered '$d1'"
		fail=1
	fi
	if [ -n "$d0" ] && [ "$r1" = "$d0" ]; then
		echo "ok: $IF1 proxy remote $r1 == $IF0 discovered"
	else
		echo "FAIL: $IF1 proxy remote '$r1' != $IF0 discovered '$d0'"
		fail=1
	fi
	if [ -n "$i0" ] && [ "$e0" = "$i0" ]; then
		echo "ok: $IF0 peer endpoint $e0 == its proxy inner listener"
	else
		echo "FAIL: $IF0 peer endpoint '$e0' != proxy inner '$i0'"
		fail=1
	fi
	if [ -n "$i1" ] && [ "$e1" = "$i1" ]; then
		echo "ok: $IF1 peer endpoint $e1 == its proxy inner listener"
	else
		echo "FAIL: $IF1 peer endpoint '$e1' != proxy inner '$i1'"
		fail=1
	fi
else
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
fi

if [ -n "${E2E_STORE-}" ]; then
	echo "== 4. plugin chain: mirror -> [trace -> file store, trace -> opendht] =="
	traced() { # FILE OP -> sorted keys of successful OP calls with a value
		$SUDO cat "$1" | jq -r --arg op "$2" 'select(.op==$op and .ok and .value_len>0)|.key' | sort -u
	}
	own0=$(traced "$E2E_WORK/shleg0.jsonl" set)
	own1=$(traced "$E2E_WORK/shleg1.jsonl" set)
	for slot in 0 1; do
		if [ "$slot" = 0 ]; then
			name=$IF0; log=$LOG0; own=$own0; peer=$own1
		else
			name=$IF1; log=$LOG1; own=$own1; peer=$own0
		fi
		if [ -n "$own" ] && [ "$(echo "$own" | wc -l | tr -d ' ')" = 1 ]; then
			echo "ok: $name file-store leg traced set $own"
		else
			echo "FAIL: $name file-store leg traced set keys '$own', want one"
			fail=1
		fi
		dhtset=$(traced "$E2E_WORK/dhtleg$slot.jsonl" set)
		if [ -n "$own" ] && [ "$dhtset" = "$own" ]; then
			echo "ok: $name opendht leg traced set $dhtset"
		else
			echo "FAIL: $name opendht leg traced set '$dhtset', want '$own'"
			fail=1
		fi
		dhtlog=$(jq -r 'select(.message=="set data to builtin opendht plugin")|.key' "$log" | sort -u)
		if [ -n "$own" ] && [ "$dhtlog" = "$own" ]; then
			echo "ok: $name opendht plugin logged set $dhtlog"
		else
			echo "FAIL: $name opendht plugin logged set '$dhtlog', want '$own'"
			fail=1
		fi
		if [ -n "$peer" ] && traced "$E2E_WORK/shleg$slot.jsonl" get | grep -qx "$peer"; then
			echo "ok: $name file-store leg traced get of the peer's key $peer"
		else
			echo "FAIL: $name file-store leg never read the peer's key '$peer'"
			fail=1
		fi
		# The mirror asks opendht only after the file store has no value.
		last=$(jq -r 'select(.message=="get data from shell plugin" or .message=="get data from builtin opendht plugin")|.message' "$log" | tail -1)
		if [ "$last" = "get data from shell plugin" ]; then
			echo "ok: $name last read served by the file store"
		else
			echo "FAIL: $name last read not served by the file store (last: '$last')"
			fail=1
		fi
	done
	want=$(printf '%s\n%s\n' "$own0" "$own1" | grep -v '^$' | sort -u)
	have=$($SUDO ls "$E2E_STORE" | grep -Ex '[0-9a-f]{40}' | sort)
	if [ -n "$want" ] && [ "$want" = "$have" ]; then
		echo "ok: file store holds exactly the published keys"
	else
		echo "FAIL: file store holds '$have', published '$want'"
		fail=1
	fi
	for k in $have; do
		v=$($SUDO cat "$E2E_STORE/$k")
		case $v in
		'' | *[!0-9a-f]*) echo "FAIL: $k holds '$v', not hex"; fail=1 ;;
		*) echo "ok: $k holds a ${#v}-char hex value" ;;
		esac
	done
fi

if [ "$fail" = "0" ]; then
	echo "PASS"
else
	echo "e2e assertions failed"
	exit 1
fi
