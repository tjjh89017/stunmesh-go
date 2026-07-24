#!/bin/sh
#
# OpenDHT Storage Plugin for stunmesh-go (exec protocol)
#
# Stores peer endpoint data in the OpenDHT distributed hash table via an
# OpenDHT proxy server's REST API. No account, no API token, no quota.
#
# Configuration in config.yaml:
#   plugins:
#     opendht:
#       type: exec
#       command: /usr/local/bin/stunmesh-opendht
#       args: ["-endpoint", "https://dhtproxy2.jami.net",
#              "-endpoint", "https://dhtproxy3.jami.net"]
#       dedup: false
#
# IMPORTANT: dedup must stay false. OpenDHT values expire after 10 minutes
# (DEFAULT_VALUE_EXPIRATION); skipping an unchanged publish lets the value
# expire and silently breaks the mesh. See README.md.
#
# Requires: curl, jq

set -eu

ENDPOINTS=""
MAGIC="stunmesh-v1"
TIMEOUT=15

usage() {
	cat >&2 <<'EOF'
Usage: stunmesh-opendht -endpoint URL [-endpoint URL]... [-magic STRING]
                        [-timeout SECONDS]

At least one -endpoint is required and each needs an http:// or https://
scheme. Repeat it to add fallbacks: they are tried in the order given, and
only a failed request moves on to the next. README.md suggests which
proxies to use.

Reads an exec-protocol JSON request on stdin and writes a JSON response
on stdout. Not intended to be run interactively.
EOF
	exit 2
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

# jq builds every response, so its absence has to be reported without it.
if ! command -v jq >/dev/null 2>&1; then
	printf '{"success":false,"error":"jq not found in PATH"}\n'
	exit 0
fi

respond_ok() {
	jq -cn --arg v "${1-}" '{success: true, value: $v}'
	exit 0
}

respond_error() {
	jq -cn --arg e "$1" '{success: false, error: $e}'
	exit 0
}

command -v curl >/dev/null 2>&1 || respond_error "curl not found in PATH"

# No default: which proxy to trust is a decision for whoever runs the mesh,
# not something to inherit silently. README.md suggests some.
[ -n "$ENDPOINTS" ] || respond_error "no -endpoint given; see the plugin README for suggested proxies"

# Validate every endpoint up front, so a typo in the third one is not
# discovered only when the first two happen to be down. curl would read a
# scheme-less "dhtproxy.jami.net" as http, silently downgrading from the https
# the default uses, so ask for the scheme instead. Trailing slashes are
# trimmed because the request URLs append "/key/...".
checked=""
for endpoint in $ENDPOINTS; do
	case "$endpoint" in
	http://* | https://*) ;;
	*)
		respond_error "-endpoint '$endpoint' must start with http:// or https://"
		;;
	esac
	checked="$checked ${endpoint%/}"
done
ENDPOINTS="$checked"

request=$(cat)

action=$(printf '%s' "$request" | jq -r '.action // empty' 2>/dev/null) ||
	respond_error "malformed JSON request"
key=$(printf '%s' "$request" | jq -r '.key // empty' 2>/dev/null) ||
	respond_error "malformed JSON request"
value=$(printf '%s' "$request" | jq -r '.value // empty' 2>/dev/null) ||
	respond_error "malformed JSON request"

[ -n "$action" ] || respond_error "missing action"
[ -n "$key" ] || respond_error "missing key"

# OpenDHT addresses values by InfoHash: 160 bits, i.e. 40 hex characters.
# stunmesh keys are SHA1 hex, so they map over directly -- but reject
# anything else rather than let the proxy interpret a bad path segment.
case "$key" in
*[!0-9a-fA-F]* | "")
	respond_error "key must be hex"
	;;
esac
[ ${#key} -eq 40 ] || respond_error "key must be 40 hex characters, got ${#key}"

case "$action" in
get)
	# Only a failed request moves on to the next endpoint. A request that
	# succeeds but carries no value is an answer, not a failure, and every
	# endpoint fronts the same DHT, so asking another would give the same one.
	# A successful request may return an empty body -- a key with no value --
	# so completion is tracked separately from what came back.
	body=""
	answered=""
	errors=""
	for endpoint in $ENDPOINTS; do
		if body=$(curl -sS -f --max-time "$TIMEOUT" "$endpoint/key/$key" 2>&1); then
			answered=1
			break
		fi
		errors="$errors; $endpoint: $body"
		body=""
	done

	[ -n "$answered" ] || respond_error "get request failed:${errors#;}"

	# The proxy answers with newline-delimited JSON: one value object per
	# line, since a key holds a set of values rather than a single slot.
	# Anyone can publish under a known key, so decode each candidate, keep
	# the ones carrying our magic, and take the most recent. fromjson?
	# discards entries that are not our envelope at all.
	found=$(printf '%s' "$body" | jq -rs --arg m "$MAGIC" '
		map(.data | @base64d | fromjson? | select(.magic == $m))
		| sort_by(.ts)
		| last
		| .data // empty
	' 2>/dev/null) || respond_error "failed to parse proxy response"

	[ -n "$found" ] || respond_error "no value found for key"

	respond_ok "$found"
	;;
set)
	[ -n "$value" ] || respond_error "missing value"

	payload=$(jq -cn \
		--arg m "$MAGIC" \
		--argjson ts "$(date +%s)" \
		--arg d "$value" \
		'{data: ({magic: $m, ts: $ts, data: $d} | tojson | @base64)}') ||
		respond_error "failed to build request payload"

	stored=""
	errors=""
	for endpoint in $ENDPOINTS; do
		if out=$(curl -sS -f --max-time "$TIMEOUT" \
			-X POST \
			-H 'Content-Type: application/json' \
			-d "$payload" \
			"$endpoint/key/$key" 2>&1); then
			stored=1
			break
		fi
		errors="$errors; $endpoint: $out"
	done

	[ -n "$stored" ] || respond_error "set request failed:${errors#;}"

	respond_ok ""
	;;
*)
	respond_error "unknown action: $action"
	;;
esac
