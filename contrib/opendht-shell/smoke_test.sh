#!/bin/sh
# Script-level smoke test for opendht-shell.sh (shell protocol).
#
# Same idea as contrib/opendht/smoke_test.sh, but drives the shell-protocol
# sibling: STUNMESH_ACTION/KEY/VALUE lines on stdin, raw value on stdout for
# get, exit status for set. Exercises the same hand-rolled envelope parsing
# (this variant decodes with sed instead of jq) and multi-endpoint fallback
# against a throwaway local HTTP server standing in for the OpenDHT proxy.
#
# Requires: curl, base64, python3 (mock server only -- not a runtime
# dependency of opendht-shell.sh itself).
set -eu

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
PLUGIN="$HERE/opendht-shell.sh"
KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352
fail=0

MOCK_DIR=$(mktemp -d)
cat >"$MOCK_DIR/server.py" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

store = {}

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        key = self.path.split("/key/", 1)[-1]
        body = "\n".join(store.get(key, [])).encode()
        self.send_response(200)
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        key = self.path.split("/key/", 1)[-1]
        store.setdefault(key, []).append(raw.decode())
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"{}")

port = int(sys.argv[1])
ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

PORT=$((22000 + $$ % 20000))
python3 "$MOCK_DIR/server.py" "$PORT" &
SERVER_PID=$!
ENDPOINT="http://127.0.0.1:$PORT"

cleanup() {
	kill "$SERVER_PID" 2>/dev/null || true
	rm -rf "$MOCK_DIR"
}
trap cleanup EXIT

for _ in 1 2 3 4 5 6 7 8 9 10; do
	curl -sS -o /dev/null "$ENDPOINT/key/probe" 2>/dev/null && break
	sleep 0.2
done

check() { # description got want
	if [ "$2" = "$3" ]; then
		echo "ok: $1"
	else
		echo "FAIL: $1 -- got [$2] want [$3]"
		fail=1
	fi
}

echo "== 1. set/get happy path =="
set_status=0
printf 'STUNMESH_ACTION=set\nSTUNMESH_KEY=%s\nSTUNMESH_VALUE=deadbeef\n' "$KEY" |
	sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>"$MOCK_DIR/set.err" || set_status=$?
check "set exits 0" "$set_status" "0"

get_out=$(printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=%s\n' "$KEY" | sh "$PLUGIN" -endpoint "$ENDPOINT")
check "get returns the value that was set" "$get_out" "deadbeef"

echo "== 2. get on a key nobody set =="
missing_status=0
printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n' |
	sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || missing_status=$?
check "get on unset key exits non-zero" "$([ "$missing_status" != 0 ] && echo yes || echo no)" "yes"

echo "== 3. malformed input =="
noaction_status=0
printf 'STUNMESH_KEY=%s\n' "$KEY" | sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || noaction_status=$?
check "missing STUNMESH_ACTION exits non-zero" "$([ "$noaction_status" != 0 ] && echo yes || echo no)" "yes"

badkey_status=0
printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=not-hex\n' | sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || badkey_status=$?
check "non-hex key exits non-zero" "$([ "$badkey_status" != 0 ] && echo yes || echo no)" "yes"

shortkey_status=0
printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=abcd\n' | sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || shortkey_status=$?
check "short key exits non-zero" "$([ "$shortkey_status" != 0 ] && echo yes || echo no)" "yes"

unknown_status=0
printf 'STUNMESH_ACTION=delete\nSTUNMESH_KEY=%s\n' "$KEY" | sh "$PLUGIN" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || unknown_status=$?
check "unknown action exits non-zero" "$([ "$unknown_status" != 0 ] && echo yes || echo no)" "yes"

echo "== 4. hand-rolled JSON parsing: multiple values, most recent wins =="
MVKEY=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
old_envelope=$(printf '{"magic":"stunmesh-v1","ts":100,"data":"old-value"}' | base64 | tr -d '\n')
new_envelope=$(printf '{"magic":"stunmesh-v1","ts":200,"data":"new-value"}' | base64 | tr -d '\n')
foreign_envelope=$(printf '{"magic":"someone-else","ts":300,"data":"not-ours"}' | base64 | tr -d '\n')
# Seed the store directly, out of order, so the plugin's own ts comparison
# (not insertion order) is what is under test.
curl -sS -X POST -d "{\"data\":\"$new_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null
curl -sS -X POST -d "{\"data\":\"$foreign_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null
curl -sS -X POST -d "{\"data\":\"$old_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null

mv_out=$(printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=%s\n' "$MVKEY" | sh "$PLUGIN" -endpoint "$ENDPOINT")
check "highest-ts envelope with matching magic wins" "$mv_out" "new-value"

echo "== 5. multi-endpoint fallback =="
DEAD_PORT=$((PORT - 1))
FBKEY=cccccccccccccccccccccccccccccccccccccccc
fb_set_status=0
printf 'STUNMESH_ACTION=set\nSTUNMESH_KEY=%s\nSTUNMESH_VALUE=deadc0de\n' "$FBKEY" |
	sh "$PLUGIN" -endpoint "http://127.0.0.1:$DEAD_PORT" -endpoint "$ENDPOINT" >/dev/null 2>/dev/null || fb_set_status=$?
check "set falls through to the second endpoint" "$fb_set_status" "0"

fb_get=$(printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=%s\n' "$FBKEY" |
	sh "$PLUGIN" -endpoint "http://127.0.0.1:$DEAD_PORT" -endpoint "$ENDPOINT")
check "get falls through and reads back the fallback write" "$fb_get" "deadc0de"

echo "== 6. no endpoint configured =="
noendpoint_status=0
printf 'STUNMESH_ACTION=get\nSTUNMESH_KEY=%s\n' "$KEY" | sh "$PLUGIN" >/dev/null 2>/dev/null || noendpoint_status=$?
check "missing -endpoint exits non-zero" "$([ "$noendpoint_status" != 0 ] && echo yes || echo no)" "yes"

if [ "$fail" = "0" ]; then
	echo "PASS"
else
	echo "smoke test failures above"
	exit 1
fi
