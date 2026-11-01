#!/bin/sh
# Windows WireGuardNT tunnel start with diagnostics and one retry. Sourced by
# run.sh and realnet/run-peer.sh; the caller must set $WORK.

# win_free_udp_port PREFERRED [AVOID...]: prints PREFERRED, or else the first
# port above it (wrapping within 1024-65535) that is outside every excluded
# UDP range, not bound, and not in AVOID. Hosted runners reserve Hyper-V UDP
# ranges that differ per VM, and WireGuardNT cannot bind a ListenPort inside
# one (the service exits with Win32 error 32).
win_free_udp_port() {
	_pref=$1; shift
	_port=$(
		{
			for _fam in ipv4 ipv6; do
				netsh int "$_fam" show excludedportrange protocol=udp 2>/dev/null
			done | tr -d '\r' |
				awk '$1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ { print "R", $1, $2 }'
			netstat -ano 2>/dev/null | tr -d '\r' |
				awk '$1 == "UDP" { n = split($2, a, ":"); print "B", a[n] }'
			for _a in "$@"; do echo "B $_a"; done
		} | win_pick_port "$_pref"
	) || { echo "[e2e] no free UDP port found from $_pref" >&2; return 1; }
	[ "$_port" != "$_pref" ] &&
		echo "[e2e] UDP port $_pref is excluded or bound; using $_port" >&2
	echo "$_port"
}

# win_pick_port PREFERRED: the search behind win_free_udp_port. Reads
# "R START END" (excluded range) and "B PORT" (taken) lines on stdin.
win_pick_port() {
	awk -v pref="$1" '
	$1 == "R" { n++; rs[n] = $2 + 0; re[n] = $3 + 0 }
	$1 == "B" { taken[$2 + 0] = 1 }
	END {
		p = pref + 0
		for (i = 0; i < 64512; i++) {
			ok = !(p in taken)
			for (j = 1; ok && j <= n; j++) if (p >= rs[j] && p <= re[j]) ok = 0
			if (ok) { print p; exit 0 }
			p++
			if (p > 65535) p = 1024
		}
		exit 1
	}'
}

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

# win_tunnel_diag NAME PORT: dumps everything useful for telling a runner-side
# WireGuardNT failure apart from a slow driver load. Every command is
# tolerant (|| true) so a missing tool never masks the real failure.
win_tunnel_diag() {
	name=$1; port=$2
	echo "[e2e] diag: installtunnelservice output for $name:" >&2
	cat "$WORK/install-$name.log" >&2 2>/dev/null || true
	echo "[e2e] diag: sc query WireGuardTunnel\$$name:" >&2
	sc query "WireGuardTunnel\$$name" >&2 || true
	echo "[e2e] diag: sc query WireGuardManager:" >&2
	sc query WireGuardManager >&2 || true
	echo "[e2e] diag: wireguard.exe /dumplog lines for $name:" >&2
	{ MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' wireguard.exe /dumplog 2>&1 |
		tr -d '\r' | grep -F "$name" | tail -n 30 >&2; } || true
	echo "[e2e] diag: UDP endpoints on port $port:" >&2
	powershell -NoProfile -Command \
		"Get-NetUDPEndpoint -LocalPort $port -ErrorAction SilentlyContinue | Select-Object LocalAddress,LocalPort,OwningProcess,@{n='Process';e={(Get-Process -Id \$_.OwningProcess -ErrorAction SilentlyContinue).ProcessName}} | Format-Table -AutoSize" \
		>&2 || true
	for fam in ipv4 ipv6; do
		echo "[e2e] diag: netsh $fam excluded UDP port ranges:" >&2
		netsh int "$fam" show excludedportrange protocol=udp >&2 || true
	done
	echo "[e2e] diag: recent WireGuard Application event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='Application'; ProviderName='WireGuard*'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
	echo "[e2e] diag: recent Service Control Manager System event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
}

# win_start_tunnel NAME CONF PORT: install and poll; on failure dump
# diagnostics, uninstall, wait, and retry once. Returns non-zero after the
# second failure.
win_start_tunnel() {
	name=$1; conf=$2; port=$3
	win_install_tunnel "$name" "$conf" && return 0
	win_tunnel_diag "$name" "$port"
	echo "[e2e] retrying: uninstalling and reinstalling $name"
	MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
		wireguard.exe /uninstalltunnelservice "$name" >/dev/null 2>&1 || true
	sleep 5
	win_install_tunnel "$name" "$conf" && return 0
	win_tunnel_diag "$name" "$port"
	return 1
}
