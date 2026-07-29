#!/bin/sh
# Script-level smoke test for opendht.sh (exec protocol).
#
# Drives the actual shell script with canned exec-protocol JSON on stdin
# against a throwaway local HTTP server standing in for the OpenDHT proxy,
# the same way internal/plugin/exec_test.go drives
# testdata/exec_test_plugin.sh -- except here the plugin under test is the
# real contrib script, not a fixture, so its hand-rolled JSON parsing
# (envelope base64/magic/ts decode) and multi-endpoint fallback run for
# real.
#
# Requires: jq, curl, python3 (mock server only -- not a runtime dependency
# of opendht.sh itself).
set -eu

HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd)
PLUGIN="$HERE/opendht.sh"
KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352
fail=0

# --- minimal OpenDHT proxy stand-in -----------------------------------
# GET  /key/<k>  -> newline-delimited JSON values stored under <k> (empty
#                   body, 200, if none -- matching a real proxy's "no value"
#                   response, which curl -f treats as success)
# POST /key/<k>  -> body is {"data": "<base64 envelope>"}; appended to the
#                   key's value list (a DHT key holds a set of values)
MOCK_DIR=$(mktemp -d)
cat >"$MOCK_DIR/server.py" <<'PY'
import sys, json
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

PORT=$((20000 + $$ % 20000))
python3 "$MOCK_DIR/server.py" "$PORT" &
SERVER_PID=$!
ENDPOINT="http://127.0.0.1:$PORT"

cleanup() {
	kill "$SERVER_PID" 2>/dev/null || true
	rm -rf "$MOCK_DIR"
}
trap cleanup EXIT

# give the server a moment to bind
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
set_out=$(printf '{"action":"set","key":"%s","value":"deadbeef"}' "$KEY" |
	sh "$PLUGIN" -endpoint "$ENDPOINT")
check "set responds success" "$(printf '%s' "$set_out" | jq -r .success)" "true"

get_out=$(printf '{"action":"get","key":"%s"}' "$KEY" |
	sh "$PLUGIN" -endpoint "$ENDPOINT")
check "get responds success" "$(printf '%s' "$get_out" | jq -r .success)" "true"
check "get returns the value that was set" "$(printf '%s' "$get_out" | jq -r .value)" "deadbeef"

echo "== 2. get on a key nobody set =="
missing_out=$(printf '{"action":"get","key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' |
	sh "$PLUGIN" -endpoint "$ENDPOINT")
check "get on unset key fails" "$(printf '%s' "$missing_out" | jq -r .success)" "false"

echo "== 3. malformed input =="
malformed_out=$(printf '{not valid json' | sh "$PLUGIN" -endpoint "$ENDPOINT" || true)
check "malformed JSON is rejected" "$(printf '%s' "$malformed_out" | jq -r .success)" "false"

badkey_out=$(printf '{"action":"get","key":"not-hex"}' | sh "$PLUGIN" -endpoint "$ENDPOINT" || true)
check "non-hex key is rejected" "$(printf '%s' "$badkey_out" | jq -r .success)" "false"

shortkey_out=$(printf '{"action":"get","key":"abcd"}' | sh "$PLUGIN" -endpoint "$ENDPOINT" || true)
check "short key is rejected" "$(printf '%s' "$shortkey_out" | jq -r .success)" "false"

unknown_out=$(printf '{"action":"delete","key":"%s"}' "$KEY" | sh "$PLUGIN" -endpoint "$ENDPOINT" || true)
check "unknown action is rejected" "$(printf '%s' "$unknown_out" | jq -r .success)" "false"

echo "== 4. hand-rolled JSON parsing: multiple values, most recent wins =="
MVKEY=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
old_envelope=$(printf '{"magic":"stunmesh-v1","ts":100,"data":"old-value"}' | base64 | tr -d '\n')
new_envelope=$(printf '{"magic":"stunmesh-v1","ts":200,"data":"new-value"}' | base64 | tr -d '\n')
foreign_envelope=$(printf '{"magic":"someone-else","ts":300,"data":"not-ours"}' | base64 | tr -d '\n')
# Seed the store directly, out of order, so the plugin's own sort_by(.ts)
# (not insertion order) is what is under test.
curl -sS -X POST -d "{\"data\":\"$new_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null
curl -sS -X POST -d "{\"data\":\"$foreign_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null
curl -sS -X POST -d "{\"data\":\"$old_envelope\"}" "$ENDPOINT/key/$MVKEY" >/dev/null

mv_out=$(printf '{"action":"get","key":"%s"}' "$MVKEY" | sh "$PLUGIN" -endpoint "$ENDPOINT")
check "highest-ts envelope with matching magic wins" "$(printf '%s' "$mv_out" | jq -r .value)" "new-value"

echo "== 5. multi-endpoint fallback =="
# First endpoint has nobody listening (connection refused); the plugin must
# fall through to the second, working one.
DEAD_PORT=$((PORT - 1))
FBKEY=cccccccccccccccccccccccccccccccccccccccc
fb_set=$(printf '{"action":"set","key":"%s","value":"fallback-ok"}' "$FBKEY" |
	sh "$PLUGIN" -endpoint "http://127.0.0.1:$DEAD_PORT" -endpoint "$ENDPOINT")
check "set falls through to the second endpoint" "$(printf '%s' "$fb_set" | jq -r .success)" "true"

fb_get=$(printf '{"action":"get","key":"%s"}' "$FBKEY" |
	sh "$PLUGIN" -endpoint "http://127.0.0.1:$DEAD_PORT" -endpoint "$ENDPOINT")
check "get falls through and reads back the fallback write" "$(printf '%s' "$fb_get" | jq -r .value)" "fallback-ok"

echo "== 6. no endpoint configured =="
noendpoint_out=$(printf '{"action":"get","key":"%s"}' "$KEY" | sh "$PLUGIN" || true)
check "missing -endpoint is rejected" "$(printf '%s' "$noendpoint_out" | jq -r .success)" "false"

if [ "$fail" = "0" ]; then
	echo "PASS"
else
	echo "smoke test failures above"
	exit 1
fi
