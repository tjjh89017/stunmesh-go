#!/bin/sh
# stunmesh-go installer for OpenWrt.
#
# Usage (on the router):
#   wget -qO- https://raw.githubusercontent.com/tjjh89017/stunmesh-go/main/scripts/openwrt-install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/tjjh89017/stunmesh-go/main/scripts/openwrt-install.sh | sh
#
# Uninstall:
#   wget -qO- https://raw.githubusercontent.com/tjjh89017/stunmesh-go/main/scripts/openwrt-install.sh | sh -s -- uninstall
#
# Overrides via environment:
#   STUNMESH_VERSION=v1.2.3   install a specific release tag (default: latest)
#   STUNMESH_ARCH=mipsle      skip architecture detection
#   STUNMESH_NO_CA=1          install the plain binary instead of the -ca one
#                             (the -ca variant embeds the Mozilla CA bundle, so
#                             TLS works without the ca-bundle package)
#   STUNMESH_BIN_DIR=/usr/bin install directory for the binary
#   STUNMESH_PURGE=1          uninstall only: also delete /etc/stunmesh
#                             including the live config

set -eu

REPO="tjjh89017/stunmesh-go"
BIN_DIR="${STUNMESH_BIN_DIR:-/usr/bin}"
BIN_PATH="$BIN_DIR/stunmesh-go"
INIT_PATH="/etc/init.d/stunmesh"
CONFIG_DIR="/etc/stunmesh"

log() { echo "stunmesh-install: $*"; }
die() { echo "stunmesh-install: error: $*" >&2; exit 1; }

# curl if available, otherwise wget (uclient-fetch on OpenWrt).
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	die "neither curl nor wget found"
fi

detect_arch() {
	# OPENWRT_ARCH (e.g. mips_24kc, mipsel_24kc, aarch64_cortex-a53,
	# arm_cortex-a7_neon-vfpv4, x86_64) distinguishes MIPS endianness,
	# which uname -m does not.
	openwrt_arch=""
	[ -r /etc/os-release ] && openwrt_arch="$(. /etc/os-release 2>/dev/null; echo "${OPENWRT_ARCH:-}")"
	[ -n "$openwrt_arch" ] || openwrt_arch="$(uname -m)"

	case "$openwrt_arch" in
		x86_64*) echo amd64 ;;
		aarch64*) echo arm64 ;;
		arm*) echo arm ;;
		mips64*) die "unsupported architecture: $openwrt_arch" ;;
		mipsel*) echo mipsle ;;
		mips*)
			# uname -m fallback says just "mips" for both endiannesses;
			# EI_DATA (byte 5) of any ELF binary settles it: 1=LE, 2=BE.
			# OpenWrt busybox ships hexdump but usually not od.
			if command -v hexdump >/dev/null 2>&1; then
				ei_data="$(hexdump -v -s5 -n1 -e '1/1 "%u"' /bin/sh)"
			elif command -v od >/dev/null 2>&1; then
				ei_data="$(od -An -j5 -N1 -tu1 /bin/sh | tr -d ' ')"
			else
				die "cannot probe MIPS endianness (no hexdump or od); set STUNMESH_ARCH=mips or STUNMESH_ARCH=mipsle"
			fi
			if [ "$ei_data" = "1" ]; then
				echo mipsle
			else
				echo mips
			fi
			;;
		i386|i686|x86) die "32-bit x86 is not supported by stunmesh-go releases" ;;
		*) die "unrecognized architecture: $openwrt_arch (set STUNMESH_ARCH to override)" ;;
	esac
}

do_install() {
	ARCH="${STUNMESH_ARCH:-$(detect_arch)}"

	TMP_DIR="$(mktemp -d)"
	trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

	VERSION="${STUNMESH_VERSION:-}"
	if [ -z "$VERSION" ]; then
		fetch "https://api.github.com/repos/$REPO/releases/latest" "$TMP_DIR/release.json" \
			|| die "failed to query the latest release (TLS missing? opkg install ca-bundle libustream-mbedtls, or set STUNMESH_VERSION)"
		VERSION="$(sed -n 's/.*"tag_name"[^"]*"\([^"]*\)".*/\1/p' "$TMP_DIR/release.json" | head -n1)"
		[ -n "$VERSION" ] || die "could not determine the latest release tag"
	fi

	ASSET="stunmesh-linux-$ARCH"
	[ "${STUNMESH_NO_CA:-0}" != "0" ] || ASSET="$ASSET-ca"
	ASSET="$ASSET-$VERSION"
	URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

	log "installing stunmesh-go $VERSION for linux/$ARCH"
	log "downloading $URL"
	fetch "$URL" "$TMP_DIR/stunmesh-go" || die "download failed: $URL"
	[ -s "$TMP_DIR/stunmesh-go" ] || die "downloaded file is empty: $URL"

	chmod 755 "$TMP_DIR/stunmesh-go"
	mv "$TMP_DIR/stunmesh-go" "$BIN_PATH"
	log "installed $BIN_PATH"

	if [ ! -e "$INIT_PATH" ]; then
		cat > "$INIT_PATH" <<EOF
#!/bin/sh /etc/rc.common

START=99
USE_PROCD=1

start_service() {
	procd_open_instance
	procd_set_param command $BIN_PATH
	procd_set_param respawn
	procd_set_param stderr 1
	procd_close_instance
}
EOF
		chmod 755 "$INIT_PATH"
		log "installed procd init script $INIT_PATH"
	else
		log "keeping existing $INIT_PATH"
	fi

	mkdir -p "$CONFIG_DIR"
	cat > "$CONFIG_DIR/config.yaml.example" <<'EOF'
---
refresh_interval: "1m"
log:
  level: "info"
interfaces:
  wg0:
    peers:
      "PEER_B":
        public_key: "<PEER_B_PUBLIC_KEY_BASE64>"
        plugin: dht
stun:
  addresses: ["stun.l.google.com:19302"]
plugins:
  dht:
    type: builtin
    name: opendht
    endpoints:
      - "https://dhtproxy2.jami.net"
      - "https://dhtproxy3.jami.net"
EOF
	log "wrote sample config to $CONFIG_DIR/config.yaml.example"

	log "done. next steps:"
	log "  1. cp $CONFIG_DIR/config.yaml.example $CONFIG_DIR/config.yaml and edit it"
	log "  2. $INIT_PATH enable"
	log "  3. $INIT_PATH start"
}

do_uninstall() {
	if [ -e "$INIT_PATH" ]; then
		"$INIT_PATH" stop 2>/dev/null || true
		"$INIT_PATH" disable 2>/dev/null || true
		rm -f "$INIT_PATH"
		log "stopped service and removed $INIT_PATH"
	fi

	if [ -e "$BIN_PATH" ]; then
		rm -f "$BIN_PATH"
		log "removed $BIN_PATH"
	fi

	rm -f "$CONFIG_DIR/config.yaml.example"

	if [ "${STUNMESH_PURGE:-0}" != "0" ]; then
		rm -rf "$CONFIG_DIR"
		log "removed $CONFIG_DIR"
	elif [ -d "$CONFIG_DIR" ]; then
		rmdir "$CONFIG_DIR" 2>/dev/null \
			|| log "kept $CONFIG_DIR (your config; set STUNMESH_PURGE=1 to delete it too)"
	fi

	log "uninstalled"
}

case "${1:-install}" in
	install) do_install ;;
	uninstall) do_uninstall ;;
	*) die "unknown command: $1 (expected install or uninstall)" ;;
esac
