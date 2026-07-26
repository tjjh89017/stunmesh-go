#!/bin/sh
#
# OpenDHT Storage Plugin for stunmesh-go (shell protocol)
#
# Same DHT and wire format as contrib/opendht, but implemented for systems
# without jq: busybox sh, sed, base64 and curl or wget are enough, which is
# what an OpenWrt-class device ships by default.
#
# Configuration in config.yaml:
#   plugins:
#     opendht:
#       type: shell
#       command: /usr/local/bin/stunmesh-opendht-shell
#       args: ["-endpoint", "https://dhtproxy2.jami.net",
#              "-endpoint", "https://dhtproxy3.jami.net"]
#       dedup: false
#
# IMPORTANT: dedup must stay false. OpenDHT values expire after 10 minutes;
# skipping an unchanged publish lets the value expire and silently breaks the
# mesh. See contrib/opendht/README.md.

set -eu

ENDPOINTS=""
MAGIC="stunmesh-v1"
TIMEOUT=15

usage() {
	cat >&2 <<'EOF'
Usage: stunmesh-opendht-shell -endpoint URL [-endpoint URL]...
                              [-magic STRING] [-timeout SECONDS]

At least one -endpoint is required and each needs an http:// or https://
scheme. Repeat it to add fallbacks: they are tried in the order given, and
only a failed request moves on to the next.

Reads shell-protocol STUNMESH_* variables on stdin; for get the value is
written to stdout, for set success is the exit status. Not intended to be
run interactively.
EOF
	exit 2
}

die() {
	printf '%s\n' "$1" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	-endpoint)
		[ $# -ge 2 ] || usage
		# Repeatable. URLs contain no spaces, so a space-separated list
		# survives the word splitting the loops below rely on.
		ENDPOINTS="$ENDPOINTS $2"
		shift 2
		;;
	-magic)
		[ $# -ge 2 ] || usage
		MAGIC="$2"
		shift 2
		;;
	-timeout)
		[ $# -ge 2 ] || usage
		TIMEOUT="$2"
		shift 2
		;;
	-h | -help | --help)
		usage
		;;
	*)
		printf 'unknown option: %s\n' "$1" >&2
		usage
		;;
	esac
done

# The envelope is built with printf, not a JSON encoder, so the magic must not
# need escaping.
case "$MAGIC" in
*[!A-Za-z0-9._-]* | "")
	die "-magic must be non-empty and contain only A-Za-z0-9._-"
	;;
esac

[ -n "$ENDPOINTS" ] || die "no -endpoint given; see contrib/opendht/README.md for suggested proxies"

# Validate every endpoint up front, so a typo in the third one is not
# discovered only when the first two happen to be down. Trailing slashes are
# trimmed because the request URLs append "/key/...".
checked=""
for endpoint in $ENDPOINTS; do
	case "$endpoint" in
	http://* | https://*) ;;
	*)
		die "-endpoint '$endpoint' must start with http:// or https://"
		;;
	esac
	checked="$checked ${endpoint%/}"
done
ENDPOINTS="$checked"

command -v base64 >/dev/null 2>&1 || die "base64 not found in PATH"

# curl when available, else wget: GNU wget, busybox wget and OpenWrt's
# uclient-fetch all accept -q, -T, -O - and --post-data.
if command -v curl >/dev/null 2>&1; then
	HTTP=curl
elif command -v wget >/dev/null 2>&1; then
	HTTP=wget
else
	die "neither curl nor wget found in PATH"
fi

http_get() { # URL
	if [ "$HTTP" = curl ]; then
		curl -sS -f --max-time "$TIMEOUT" "$1"
	else
		wget -q -T "$TIMEOUT" -O - "$1"
	fi
}

http_post() { # URL BODY
	if [ "$HTTP" = curl ]; then
		curl -sS -f --max-time "$TIMEOUT" -X POST \
			-H 'Content-Type: application/json' -d "$2" "$1"
	else
		# No portable way to set Content-Type here; the proxy accepts the
		# JSON body without it.
		wget -q -T "$TIMEOUT" -O - --post-data="$2" "$1"
	fi
}

# Shell protocol input: STUNMESH_ACTION/KEY/VALUE, one per line. Parsed with
# case instead of eval, so a hostile line cannot execute anything. The `||`
# keeps the last line even when it lacks a trailing newline.
ACTION=""
KEY=""
VALUE=""
while IFS= read -r line || [ -n "$line" ]; do
	case "$line" in
	STUNMESH_ACTION=*) ACTION=${line#STUNMESH_ACTION=} ;;
	STUNMESH_KEY=*) KEY=${line#STUNMESH_KEY=} ;;
	STUNMESH_VALUE=*) VALUE=${line#STUNMESH_VALUE=} ;;
	esac
done

[ -n "$ACTION" ] || die "missing STUNMESH_ACTION"
[ -n "$KEY" ] || die "missing STUNMESH_KEY"

# An OpenDHT key is an InfoHash: 160 bits, i.e. 40 hex characters. stunmesh
# keys are SHA1 hex, so they map over directly -- but reject anything else
# rather than let the proxy interpret a bad path segment.
case "$KEY" in
*[!0-9a-fA-F]* | "")
	die "key must be hex"
	;;
esac
[ ${#KEY} -eq 40 ] || die "key must be 40 hex characters, got ${#KEY}"

# json_field BODY NAME -> the first string value of "NAME". Safe because every
# field this plugin reads is base64, hex or a magic validated above -- none of
# them can contain a quote or an escape.
json_field() {
	printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
}

json_number() {
	printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p" | head -n 1
}

case "$ACTION" in
get)
	# Only a failed request moves on to the next endpoint. A request that
	# succeeds but carries no value is an answer, not a failure, and every
	# endpoint fronts the same DHT, so asking another would give the same one.
	body=""
	answered=""
	errors=""
	for endpoint in $ENDPOINTS; do
		if body=$(http_get "$endpoint/key/$KEY" 2>&1); then
			answered=1
			break
		fi
		errors="$errors; $endpoint: $body"
		body=""
	done

	[ -n "$answered" ] || die "get request failed:${errors#;}"

	# The proxy answers with newline-delimited JSON: one value object per
	# line, since a key holds a set of values rather than a single slot.
	# Anyone can publish under a known key, so decode each candidate, keep
	# the ones carrying our magic, and take the most recent by ts. Lines
	# that fail any step of the decode are ignored.
	best_ts=-1
	best=""
	oldIFS=$IFS
	IFS='
'
	for line in $body; do
		IFS=$oldIFS
		b64=$(json_field "$line" data)
		[ -n "$b64" ] || continue
		env=$(printf '%s' "$b64" | base64 -d 2>/dev/null) || continue
		[ "$(json_field "$env" magic)" = "$MAGIC" ] || continue
		ts=$(json_number "$env" ts)
		[ -n "$ts" ] || continue
		data=$(json_field "$env" data)
		[ -n "$data" ] || continue
		if [ "$ts" -gt "$best_ts" ]; then
			best_ts=$ts
			best=$data
		fi
	done
	IFS=$oldIFS

	[ -n "$best" ] || die "no value found for key"

	printf '%s\n' "$best"
	;;
set)
	[ -n "$VALUE" ] || die "missing STUNMESH_VALUE"
	case "$VALUE" in
	*[!0-9a-fA-F]*)
		die "value must be hex"
		;;
	esac

	# Same envelope as the builtin and exec opendht plugins, so shell nodes
	# interoperate with them. base64 output is unwrapped because busybox
	# base64 folds at 76 columns.
	envelope=$(printf '{"magic":"%s","ts":%s,"data":"%s"}' \
		"$MAGIC" "$(date +%s)" "$VALUE")
	b64=$(printf '%s' "$envelope" | base64 | tr -d '\n')
	payload=$(printf '{"data":"%s"}' "$b64")

	stored=""
	errors=""
	for endpoint in $ENDPOINTS; do
		if out=$(http_post "$endpoint/key/$KEY" "$payload" 2>&1); then
			stored=1
			break
		fi
		errors="$errors; $endpoint: $out"
	done

	[ -n "$stored" ] || die "set request failed:${errors#;}"
	;;
*)
	die "unknown action: $ACTION"
	;;
esac
