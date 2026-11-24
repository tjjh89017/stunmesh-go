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
# report step decides the verdict from the recorded conclusions of both sides;
# only the pair acknowledgement also sets the peer jobs' exit code.
#
# Symmetric NAT skip: a probe that reports mapping=dependent on either role
# makes the pair skipped. The role stores that fact under a signal key shared
# by both jobs of the pair (run id, attempt, pair), through the same OpenDHT
# proxies the daemon uses. A role that knows the pair is skipped stops its
# handshake wait, and a missing acknowledgement then fails neither cell. The
# skip is annotated with one ::warning:: per job, which changes no exit status.
# Before its final snapshot the role still waits for the peer's endpoint
# (wait_peer_endpoint), since the endpoint round-trip check is never skipped.

# wait_handshake KEY -- poll until the peer's latest-handshake is nonzero.
wait_handshake() {
	_hs_t0=$(date +%s)
	_hs_deadline=$((_hs_t0 + HANDSHAKE_TIMEOUT))
	_hs_tick=0
	while [ "$(date +%s)" -lt "$_hs_deadline" ]; do
		if [ $((_hs_tick % 6)) -eq 0 ]; then
			if nat_skip; then
				log "pair skipped ($NAT_SKIP_REASON); stopping the handshake wait"
				rec "$1" skipped
				rec "$1_secs" "-"
				return 1
			fi
		fi
		_hs=$(ns_exec wg show "$WG_IF" latest-handshakes | awk 'NR==1{print $2}')
		if [ "${_hs:-0}" -gt 0 ] 2>/dev/null; then
			rec "$1" pass
			rec "$1_secs" $(($(date +%s) - _hs_t0))
			return 0
		fi
		# The anchor must not sit out its whole window against a subject
		# that already gave up and left; same probe as the hold loop, at
		# the same 30s budget (every 6th 5s tick).
		_hs_tick=$((_hs_tick + 1))
		if [ $((_hs_tick % 6)) -eq 0 ] && [ -n "${SUBJECT_DONE_CMD:-}" ] &&
			sh -c "$SUBJECT_DONE_CMD" >/dev/null 2>&1; then
			log "peer's run already completed; abandoning the handshake wait"
			break
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

# nat_probe PHASE -- informational: classify this runner's NAT mapping with
# natprobe and record natprobe_PHASE_* plus egress_ip_PHASE. A later phase
# reuses the first probe's local port, so it can tell whether the mapping
# changed. Every key falls back to unknown; nothing here can fail the job.
nat_probe() {
	_np_out=$WORK/natprobe-$1.log
	if [ -n "${NATPROBE_BIN:-}" ] && [ -x "$NATPROBE_BIN" ]; then
		if [ -n "${NP_PORT:-}" ]; then
			ns_exec "$NATPROBE_BIN" -port "$NP_PORT" -previous "$NP_MAPPED" >"$_np_out" 2>&1 || true
		else
			ns_exec "$NATPROBE_BIN" >"$_np_out" 2>&1 || true
		fi
	else
		echo "natprobe binary unavailable" >"$_np_out"
	fi
	for _np_k in mapping mapped local_port same_port changed; do
		_np_v=$(sed -n "s/^$_np_k=//p" "$_np_out" | tail -n 1)
		rec "natprobe_$1_$_np_k" "${_np_v:-unknown}"
	done
	_np_v=$(sed -n 's/^public_ip=//p' "$_np_out" | tail -n 1)
	rec "egress_ip_$1" "${_np_v:-unknown}"
	if grep -qx 'mapping=dependent' "$_np_out" && [ "${OWN_SYMMETRIC:-0}" != 1 ]; then
		OWN_SYMMETRIC=1
		OWN_EGRESS=${_np_v:-unknown}
		nat_signal put "role=$ROLE nat=dependent egress=$OWN_EGRESS" >/dev/null || true
	fi
	if [ -z "${NP_PORT:-}" ]; then
		NP_PORT=$(sed -n 's/^local_port=//p' "$_np_out" | tail -n 1)
		[ "$NP_PORT" = unknown ] && NP_PORT=''
		NP_MAPPED=$(sed -n 's/^mapped=//p' "$_np_out" | tail -n 1)
	fi
	return 0
}

# nat_signal put|has VALUE -- store VALUE under this pair's signal key, or
# print a value the other job stored that starts with VALUE, failing when
# there is none. CI-only: without a run id there is no key both jobs share,
# so it always fails.
nat_signal() {
	[ -n "${GITHUB_RUN_ID:-}" ] && [ -n "${NATPROBE_BIN:-}" ] && [ -x "$NATPROBE_BIN" ] || return 1
	"$NATPROBE_BIN" -signal-key "stunmesh-realnet-nat|${GITHUB_REPOSITORY:-}|$GITHUB_RUN_ID|$RUN_ATTEMPT|${REALNET_PAIR:-local}" \
		-signal-endpoints "$ENDPOINTS" "-signal-$1" "$2" 2>>"$WORK/natsignal.log"
}

# nat_skip -- succeed when either role's NAT is known to be symmetric, and set
# NAT_SKIP_REASON (with that role's egress IP when known). The peer's fact is
# sticky once seen: its stored value expires with the DHT entry.
nat_skip() {
	if [ "${OWN_SYMMETRIC:-0}" = 1 ]; then
		NAT_SKIP_REASON=$(nat_reason "$ROLE" "${OWN_EGRESS:-}")
		return 0
	fi
	_peer_role=anchor; [ "$ROLE" = anchor ] && _peer_role=subject
	if [ "${PEER_SYMMETRIC:-0}" != 1 ] &&
		_ns_v=$(nat_signal has "role=$_peer_role nat=dependent"); then
		PEER_SYMMETRIC=1
		PEER_EGRESS=$(echo "$_ns_v" | sed -n 's/.* egress=\([^ ]*\).*/\1/p' | head -n 1)
	fi
	if [ "${PEER_SYMMETRIC:-0}" = 1 ]; then
		NAT_SKIP_REASON=$(nat_reason "$_peer_role" "${PEER_EGRESS:-}")
		return 0
	fi
	return 1
}

# nat_reason ROLE EGRESS -- the skip reason text shared with report.sh.
nat_reason() {
	case "$2" in
	'' | unknown | '(missing)') echo "$1 NAT is symmetric" ;;
	*) echo "$1 NAT is symmetric (egress $2)" ;;
	esac
}

# peer_endpoint -- the peer's endpoint on the device, empty while none is set.
peer_endpoint() {
	ns_exec wg show "$WG_IF" endpoints | awk 'NR==1 && $2 != "(none)" {print $2}'
}

# wait_peer_endpoint -- poll until the daemon has applied the peer's endpoint.
# A skipped pair has no handshake wait to keep the daemon up that long, and
# the final snapshot feeds the hard endpoint round-trip check. The deadline
# is the one wait_handshake would have had, counted from DAEMON_TS, so a
# skipped role never runs longer than an unskipped one.
wait_peer_endpoint() {
	_ep_deadline=$((DAEMON_TS + HANDSHAKE_TIMEOUT))
	while [ -z "$(peer_endpoint)" ]; do
		if [ "$(date +%s)" -ge "$_ep_deadline" ]; then
			log "no peer endpoint within ${HANDSHAKE_TIMEOUT}s of the daemon start"
			rec skip_endpoint_wait fail
			return 1
		fi
		sleep 5
	done
	rec skip_endpoint_wait "pass after $(($(date +%s) - DAEMON_TS))s"
	return 0
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

# send_ack -- the subject's pair acknowledgement: one POST through the tunnel
# to the anchor's overlay canary. Sets PAIR_ACK; run-peer.sh turns a fail into
# a nonzero exit on both sides, so a failed pair reruns as a pair.
send_ack() {
	_ack_body="pair=${REALNET_PAIR:-local} attempt=$RUN_ATTEMPT"
	PAIR_ACK=fail
	_ack_i=0
	while [ $_ack_i -lt 3 ]; do
		if ns_exec curl -4 -sS -f -m 5 -X POST --data-binary "$_ack_body" \
			"http://$PEER_OVERLAY:$CANARY_PORT/ack" >/dev/null 2>>"$WORK/curl.log"; then
			PAIR_ACK=pass
			break
		fi
		_ack_i=$((_ack_i + 1))
		sleep 2
	done
	rec pair_ack "$PAIR_ACK"
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
	# The storage backend is deliberately NOT excluded: its socket carries
	# the device fwmark and leaves through the escape rule installed below,
	# which is what makes the refresh loop survive a covering route. An
	# exclusion route here would hide a regression in that escape. DNS
	# lookups go through the same escaped socket via the plugin dialer's
	# PreferGo resolver (#248), so they need no exclusion either.

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
	# The runner shell opens the blob: a root-created file under umask 077 is unreadable to sha256sum.
	if ns_exec curl -4 -sS -m 20 "http://$CANARY_IP:$CANARY_PORT/$_nonce" >"$WORK/canary.blob" 2>>"$WORK/curl.log"; then
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
