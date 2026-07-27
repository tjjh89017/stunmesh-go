#!/bin/sh
# Scenario phases for the realnet e2e. Sourced by run-peer.sh, which provides
# ns_exec/rec/log, start_daemon/stop_daemon and the WG_*/PEER_* globals.
#
#   split       baseline: split tunnel (peer overlay /32), raw-socket mode.
#               Both roles run it; the anchor stays in it for the whole run.
#   fulltunnel  subject only: a covering 0.0.0.0/1 + 128.0.0.0/1 default route
#               over WireGuard, proving the STUN socket still escapes it.
#
# Every probe records a conclusion via rec; nothing here exits the job. The
# report step decides the verdict from the recorded conclusions of both sides.

# wait_handshake KEY -- poll until the peer's latest-handshake is nonzero.
wait_handshake() {
	_hs_t0=$(date +%s)
	_hs_deadline=$((_hs_t0 + HANDSHAKE_TIMEOUT))
	while [ "$(date +%s)" -lt "$_hs_deadline" ]; do
		_hs=$(ns_exec wg show "$WG_IF" latest-handshakes | awk 'NR==1{print $2}')
		if [ "${_hs:-0}" -gt 0 ] 2>/dev/null; then
			rec "$1" pass
			rec "$1_secs" $(($(date +%s) - _hs_t0))
			return 0
		fi
		sleep 5
	done
	rec "$1" fail
	rec "$1_secs" "-"
	return 1
}

# ping_probe KEY IP -- a few short ping rounds; pass on the first clean one.
ping_probe() {
	_pp_i=0
	while [ $_pp_i -lt 5 ]; do
		if net_ping "$2" >/dev/null 2>&1; then
			rec "$1" pass
			return 0
		fi
		_pp_i=$((_pp_i + 1))
		sleep 3
	done
	rec "$1" fail
	return 1
}

# mtu_ping KEY IP -- full-MTU don't-fragment ping; double NAT plus the extra
# encapsulation is where MTU bugs surface, and small pings can't see them.
mtu_ping() {
	if net_ping_df $((MTU - 28)) "$2" >/dev/null 2>&1; then
		rec "$1" pass
	else
		rec "$1" fail
	fi
}

# store_count -- successful publishes so far, per the daemon log.
store_count() {
	jq -c 'select(.message=="store endpoint")' "$DAEMON_LOG" 2>/dev/null | wc -l | tr -d ' '
}

transfer_rx() {
	ns_exec wg show "$WG_IF" transfer | awk 'NR==1{print $2}'
}

split_tunnel_anchor() {
	log "split tunnel (anchor): waiting for handshake, then holding as the far end"
	wait_handshake split_handshake || return 0
	ping_probe split_ping "$PEER_OVERLAY" || true
	mtu_ping split_ping_mtu "$PEER_OVERLAY"
}

split_tunnel_subject() {
	log "split tunnel (subject): raw-socket mode"
	wait_handshake split_handshake || return 1
	ping_probe split_ping "$PEER_OVERLAY" || true
	mtu_ping split_ping_mtu "$PEER_OVERLAY"

	# Sustained stream sanity, never throughput numbers: bandwidth between two
	# cloud runners is noise, not a regression signal. Purpose is only that a
	# sustained stream does not stall the tunnel, so where iperf3 is not
	# available the transfer counters alone carry the check.
	_rx0=$(transfer_rx)
	if command -v iperf3 >/dev/null 2>&1; then
		if ns_exec iperf3 -c "$PEER_OVERLAY" -t 5 --connect-timeout 5000 >/dev/null 2>&1; then
			rec split_iperf_up pass
		else
			rec split_iperf_up fail
		fi
		if ns_exec iperf3 -c "$PEER_OVERLAY" -t 5 -R --connect-timeout 5000 >/dev/null 2>&1; then
			rec split_iperf_down pass
		else
			rec split_iperf_down fail
		fi
	else
		rec split_iperf_up skipped
		rec split_iperf_down skipped
		net_ping "$PEER_OVERLAY" >/dev/null 2>&1 || true
	fi
	_rx1=$(transfer_rx)
	if [ "${_rx1:-0}" -gt "${_rx0:-0}" ] 2>/dev/null; then
		rec split_transfer_rise pass
	else
		rec split_transfer_rise fail
	fi

	# Informational, never gating: NAT mapping timeout vs persistent-keepalive.
	log "split tunnel (subject): 60s idle, then checking ping resumes"
	sleep 60
	ping_probe split_idle_resume "$PEER_OVERLAY" || true
	return 0
}

# resolve_v4 HOST -- all IPv4 addresses, resolved from inside the namespace so
# the exclusion routes match what curl/stunmesh will connect to.
resolve_v4() {
	ns_exec getent ahostsv4 "$1" 2>/dev/null | awk '{print $1}' | sort -u
}

full_tunnel_subject() {
	rec fulltunnel_ran yes
	log "full tunnel (subject): installing the covering default route"

	# Resolve everything that must keep working before DNS goes dark under
	# the covering route.
	_stun_ip=$(resolve_v4 "$STUN_HOST" | head -1)
	_excl_ips=""
	for _h in $DHT_HOSTS; do
		_excl_ips="$_excl_ips $(resolve_v4 "$_h")"
	done
	_excl_ips="$_excl_ips 8.8.8.8 1.1.1.1"

	stop_daemon
	ns_exec wg set "$WG_IF" fwmark "$FWMARK"
	ns_exec wg set "$WG_IF" peer "$PEER_PUBLIC_KEY" \
		allowed-ips 0.0.0.0/1,128.0.0.0/1 persistent-keepalive "$KEEPALIVE"
	ns_exec ip route add 0.0.0.0/1 dev "$WG_IF"
	ns_exec ip route add 128.0.0.0/1 dev "$WG_IF"
	# The escape under test: WireGuard's outer packets carry the device
	# fwmark and stunmesh mirrors it onto the STUN socket (SO_MARK); this
	# rule is what acts on the mark, exactly as wg-quick's policy routing
	# would in production.
	ns_exec ip route add default via "$HOST_IP" dev "$VETH_NS" table "$ESCAPE_TABLE"
	ns_exec ip rule add fwmark "$FWMARK" lookup "$ESCAPE_TABLE" pref 100
	# NOT the escape under test: in a production full tunnel the rendezvous
	# traffic rides the tunnel through an exit node, but the far end here
	# never forwards, so the storage backend and DNS get explicit exclusion
	# routes to keep the refresh loop's storage side alive.
	for _ip in $_excl_ips; do
		ns_exec ip route add "$_ip/32" via "$HOST_IP" dev "$VETH_NS" 2>/dev/null || true
	done
	start_daemon
	log "full tunnel (subject): routes installed, daemon restarted"

	# Escape mechanism spot-check, plus the cheap route assert that gives a
	# better failure message when the canary fetch below fails.
	if [ -n "$_stun_ip" ] &&
		ns_exec ip route get "$_stun_ip" mark "$FWMARK" 2>/dev/null | grep -q "$VETH_NS"; then
		rec fulltunnel_escape_route pass
	else
		rec fulltunnel_escape_route fail
	fi
	if ns_exec ip route get "$CANARY_IP" 2>/dev/null | grep -q "$WG_IF"; then
		rec fulltunnel_route_canary pass
	else
		rec fulltunnel_route_canary fail
	fi

	# Canary fetch: the destination is off-overlay, so only the covering
	# route can carry it, and it terminates on the far end's dummy interface,
	# so no exit-node forwarding is needed. The report compares the sha256
	# against what the far end's server printed, which is what makes this
	# prove the traffic really traversed the tunnel intact rather than that
	# the route table merely looks right.
	_nonce="$(date +%s)-$$"
	if ns_exec curl -4 -sS -m 20 -o "$WORK/canary.blob" "http://$CANARY_IP:$CANARY_PORT/$_nonce" 2>>"$WORK/curl.log"; then
		rec fulltunnel_canary pass
		rec fulltunnel_canary_sha256 "$(sha256sum "$WORK/canary.blob" | awk '{print $1}')"
	else
		rec fulltunnel_canary fail
		rec fulltunnel_canary_sha256 "-"
	fi

	# Escape survival: across two or more refresh cycles under the covering
	# route the publish side keeps reaching its storage and the tunnel keeps
	# carrying traffic.
	_sc0=$(store_count)
	_rx0=$(transfer_rx)
	sleep 45
	_sc1=$(store_count)
	_rx1=$(transfer_rx)
	_surv=pass
	[ "${_sc1:-0}" -gt "${_sc0:-0}" ] 2>/dev/null || _surv=fail
	[ "${_rx1:-0}" -gt "${_rx0:-0}" ] 2>/dev/null || _surv=fail
	net_ping "$PEER_OVERLAY" >/dev/null 2>&1 || _surv=fail
	rec fulltunnel_survival "$_surv"

	# The endpoint must not flap to loopback under the covering route.
	if ns_exec wg show "$WG_IF" endpoints | grep -q '127\.0\.0\.1'; then
		rec fulltunnel_endpoint_flap fail
	else
		rec fulltunnel_endpoint_flap pass
	fi
}
