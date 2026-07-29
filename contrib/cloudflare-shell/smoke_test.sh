#!/bin/sh
# Script-level smoke test for cloudflare-shell.sh (shell protocol).
#
# cloudflare-shell.sh calls curl against a hardcoded
# https://api.cloudflare.com URL (there is no -endpoint override, unlike the
# opendht plugins), so it cannot be pointed at a local mock server the way
# contrib/opendht/smoke_test.sh is. Instead this test prepends a fake `curl`
# to PATH: a stand-in for the one external dependency the script has, in the
# same spirit as internal/plugin/exec_test.go substituting a fixture .sh for
# a real plugin binary. The real cloudflare-shell.sh runs unmodified; only
# the `curl` it shells out to is swapped.
#
# Requires: bash (script uses [[ ]] arrays are not used, but declares
# `#!/bin/bash`), grep, sed.
set -eu

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
PLUGIN="$HERE/cloudflare-shell.sh"
KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352
fail=0

FAKE_DIR=$(mktemp -d)
STATE="$FAKE_DIR/state"
mkdir -p "$STATE"

# --- fake curl: a minimal in-memory Cloudflare API stand-in ------------
cat >"$FAKE_DIR/curl" <<'SH'
#!/bin/sh
set -eu
method=GET
url=""
data=""
while [ $# -gt 0 ]; do
	case "$1" in
	-X) method="$2"; shift 2 ;;
	-H) shift 2 ;;
	--data) data="$2"; shift 2 ;;
	http*://*) url="$1"; shift ;;
	*) shift ;;
	esac
done

sanitize() { printf '%s' "$1" | tr -c 'A-Za-z0-9' '_'; }

case "$url" in
*/zones\?name=*)
	if [ -n "${FAKE_CURL_ZONE_MISSING-}" ]; then
		printf '{"result":[]}'
	else
		printf '{"result":[{"id":"zone123"}]}'
	fi
	;;
*/dns_records\?type=TXT\&name=*)
	name=$(printf '%s' "$url" | sed -n 's/.*name=\([^&]*\).*/\1/p')
	printf '%s\n' "$name" >>"$STATE/requested_names.log"
	f="$STATE/record_$(sanitize "$name")"
	if [ -f "$f" ]; then
		id=$(sed -n 1p "$f")
		content=$(sed -n 2p "$f")
		printf '{"result":[{"id":"%s","content":"%s"}]}' "$id" "$content"
	else
		printf '{"result":[]}'
	fi
	;;
*/dns_records)
	# create: pull name/content out of the JSON body curl was given
	name=$(printf '%s' "$data" | grep -o '"name":"[^"]*"' | head -1 | sed 's/"name":"\(.*\)"/\1/')
	content=$(printf '%s' "$data" | grep -o '"content":"[^"]*"' | head -1 | sed 's/"content":"\(.*\)"/\1/')
	id="rec-$(sanitize "$name")"
	f="$STATE/record_$(sanitize "$name")"
	printf '%s\n%s\n' "$id" "$content" >"$f"
	printf '{"result":{"id":"%s","name":"%s","content":"%s"}}' "$id" "$name" "$content"
	;;
*/dns_records/*)
	id=${url##*/}
	content=$(printf '%s' "$data" | grep -o '"content":"[^"]*"' | head -1 | sed 's/"content":"\(.*\)"/\1/')
	for f in "$STATE"/record_*; do
		[ -f "$f" ] || continue
		if [ "$(sed -n 1p "$f")" = "$id" ]; then
			printf '%s\n%s\n' "$id" "$content" >"$f"
		fi
	done
	printf '{"result":{"id":"%s","content":"%s"}}' "$id" "$content"
	;;
*)
	printf '{"result":[]}'
	;;
esac
SH
chmod +x "$FAKE_DIR/curl"

PATH="$FAKE_DIR:$PATH"
export PATH FAKE_CURL_ZONE_MISSING= STATE

cleanup() { rm -rf "$FAKE_DIR"; }
trap cleanup EXIT

check() { # description got want
	if [ "$2" = "$3" ]; then
		echo "ok: $1"
	else
		echo "FAIL: $1 -- got [$2] want [$3]"
		fail=1
	fi
}

check_nonzero() { # description status
	if [ "$2" != 0 ]; then
		echo "ok: $1"
	else
		echo "FAIL: $1 -- exited 0, want non-zero"
		fail=1
	fi
}

echo "== 1. missing required flags =="
status=0
printf '' | bash "$PLUGIN" >/dev/null 2>/dev/null || status=$?
check_nonzero "no -zone/-token exits non-zero" "$status"

echo "== 2. unknown CLI option =="
status=0
printf '' | bash "$PLUGIN" -bogus x >/dev/null 2>/dev/null || status=$?
check_nonzero "unknown option exits non-zero" "$status"

echo "== 3. zone lookup failure =="
status=0
FAKE_CURL_ZONE_MISSING=1 sh -c "printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=$KEY\n' | bash '$PLUGIN' -zone example.com -token tok" >/dev/null 2>/dev/null || status=$?
check_nonzero "unresolvable zone exits non-zero" "$status"

echo "== 4. set/get happy path =="
status=0
printf 'STUNMESH_ACTION=set\nSTUNMESH_KEY=%s\nSTUNMESH_VALUE=deadbeef\n' "$KEY" |
	bash "$PLUGIN" -zone example.com -token tok -subdomain stunmesh >/dev/null 2>"$FAKE_DIR/set.err" || status=$?
check "set exits 0" "$status" "0"

get_out=$(printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=%s\n' "$KEY" |
	bash "$PLUGIN" -zone example.com -token tok -subdomain stunmesh)
check "get returns the value that was set" "$get_out" "deadbeef"

want_name="${KEY}.stunmesh.example.com"
bad=0
while IFS= read -r n; do
	[ "$n" = "$want_name" ] || bad=1
done <"$STATE/requested_names.log"
check "every DNS lookup used the un-hashed key (a double-hash would diverge here)" "$bad" "0"

echo "== 5. malformed input: missing/unknown action =="
status=0
printf 'STUNMESH_KEY=%s\n' "$KEY" | bash "$PLUGIN" -zone example.com -token tok >/dev/null 2>/dev/null || status=$?
check_nonzero "missing STUNMESH_ACTION exits non-zero" "$status"

status=0
printf 'STUNMESH_ACTION=delete\nSTUNMESH_KEY=%s\n' "$KEY" | bash "$PLUGIN" -zone example.com -token tok >/dev/null 2>/dev/null || status=$?
check_nonzero "unknown action exits non-zero" "$status"

echo "== 6. get on a key nobody set =="
status=0
printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n' |
	bash "$PLUGIN" -zone example.com -token tok -subdomain stunmesh >/dev/null 2>/dev/null || status=$?
check_nonzero "get on unset key exits non-zero" "$status"

if [ "$fail" = "0" ]; then
	echo "PASS"
else
	echo "smoke test failures above"
	exit 1
fi
