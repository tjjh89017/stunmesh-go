#!/bin/sh
# Windows WireGuardNT tunnel start with diagnostics and one retry. Sourced by
# run.sh and realnet/run-peer.sh; the caller must set $WORK.

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

# win_tunnel_diag NAME: dumps everything useful for telling a runner-side
# WireGuardNT failure apart from a slow driver load. Every command is
# tolerant (|| true) so a missing tool never masks the real failure.
win_tunnel_diag() {
	name=$1
	echo "[e2e] diag: installtunnelservice output for $name:" >&2
	cat "$WORK/install-$name.log" >&2 2>/dev/null || true
	echo "[e2e] diag: sc query WireGuardTunnel\$$name:" >&2
	sc query "WireGuardTunnel\$$name" >&2 || true
	echo "[e2e] diag: sc query WireGuardManager:" >&2
	sc query WireGuardManager >&2 || true
	echo "[e2e] diag: recent WireGuard Application event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='Application'; ProviderName='WireGuard*'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
	echo "[e2e] diag: recent Service Control Manager System event log entries:" >&2
	powershell -NoProfile -Command \
		"Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'} -MaxEvents 20 -ErrorAction SilentlyContinue | Format-List TimeCreated,Id,LevelDisplayName,Message" \
		>&2 || true
}

# win_start_tunnel NAME CONF: install and poll; on failure dump diagnostics,
# uninstall, wait, and retry once. Returns non-zero after the second failure.
win_start_tunnel() {
	name=$1; conf=$2
	win_install_tunnel "$name" "$conf" && return 0
	win_tunnel_diag "$name"
	echo "[e2e] retrying: uninstalling and reinstalling $name"
	MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
		wireguard.exe /uninstalltunnelservice "$name" >/dev/null 2>&1 || true
	sleep 5
	win_install_tunnel "$name" "$conf" && return 0
	win_tunnel_diag "$name"
	return 1
}
