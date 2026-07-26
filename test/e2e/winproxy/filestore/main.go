// Command filestore is a file-backed exec-plugin store for the Windows proxy
// e2e (test-only). One JSON request on stdin, one JSON response on stdout;
// values live as <dir>/<key> so two stunmesh instances sharing the directory
// see each other's published endpoints without any network dependency.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func main() {
	dir := flag.String("dir", "", "directory holding one file per key")
	flag.Parse()

	resp := handle(*dir)
	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, "filestore:", err)
		os.Exit(1)
	}
}

func handle(dir string) pluginapi.ExecResponse {
	fail := func(format string, args ...any) pluginapi.ExecResponse {
		return pluginapi.ExecResponse{Success: false, Error: fmt.Sprintf(format, args...)}
	}

	if dir == "" {
		return fail("-dir is required")
	}
	var req pluginapi.ExecRequest
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail("read stdin: %v", err)
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fail("decode request: %v", err)
	}
	// Keys are SHA-1 hex; rejecting anything else doubles as a path guard.
	if _, err := hex.DecodeString(req.Key); err != nil || req.Key == "" {
		return fail("key %q is not hex", req.Key)
	}
	path := filepath.Join(dir, req.Key)

	switch req.Action {
	case pluginapi.OpGet:
		value, err := os.ReadFile(path)
		if err != nil {
			return fail("get %s: %v", req.Key, err)
		}
		return pluginapi.ExecResponse{Success: true, Value: string(value)}
	case pluginapi.OpSet:
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(req.Value), 0o600); err != nil {
			return fail("set %s: %v", req.Key, err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return fail("set %s: %v", req.Key, err)
		}
		return pluginapi.ExecResponse{Success: true}
	default:
		return fail("unknown action %q", req.Action)
	}
}
