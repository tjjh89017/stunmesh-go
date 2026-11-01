package all_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	_ "github.com/tjjh89017/stunmesh-go/builtin/all"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

var closeLog []string

// recorder answers Get with its id, keeps its next instance for identity
// checks, and records Close. With forward set it passes Get on to next.
type recorder struct {
	id      string
	next    pluginapi.Store
	forward bool
}

func (r *recorder) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	if r.forward {
		return r.next.Get(ctx, req)
	}
	return r.id, nil
}

func (r *recorder) Set(context.Context, pluginapi.PluginRequest) error { return nil }

func (r *recorder) Close() error {
	closeLog = append(closeLog, r.id)
	return nil
}

func init() {
	pluginapi.RegisterWithStores("test_recorder", func(_ context.Context, cfg pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
		r := &recorder{}
		r.id, _ = cfg["id"].(string)
		r.forward, _ = cfg["forward"].(bool)
		if name, ok := cfg["next"].(string); ok {
			next, err := stores.Lookup(name)
			if err != nil {
				return nil, err
			}
			r.next = next
		}
		return r, nil
	})
}

func TestRegistered(t *testing.T) {
	got := pluginapi.Registered()
	for _, name := range []string{"cloudflare", "mirror", "opendht", "trace"} {
		if !slices.Contains(got, name) {
			t.Errorf("Registered() = %v, missing %q", got, name)
		}
	}
}

func loadManager(t *testing.T, yaml string) *plugin.Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	m := plugin.NewManager()
	if err := m.LoadPlugins(context.Background(), cfg.Plugins); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestChainThroughTrace(t *testing.T) {
	closeLog = nil
	m := loadManager(t, `
stun:
  addresses: ["stun.example.net:3478"]
plugins:
  top:
    type: builtin
    name: test_recorder
    id: top
    forward: true
    next: front
  front:
    type: builtin
    name: trace
    next: rec
  rec:
    type: builtin
    name: test_recorder
    id: rec
    next: dht
  dht:
    type: builtin
    name: opendht
    endpoint: https://dhtproxy.example.net
`)

	top, err := m.GetPlugin("top")
	if err != nil {
		t.Fatal(err)
	}
	got, err := top.Get(context.Background(), pluginapi.PluginRequest{Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "rec" {
		t.Errorf("Get through top -> front -> rec = %q, want rec", got)
	}

	rec, _ := m.GetPlugin("rec")
	dht, _ := m.GetPlugin("dht")
	if rec.(*recorder).next != dht {
		t.Error("rec's inner instance is not the manager's dht instance")
	}

	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(closeLog, []string{"top", "rec"}) {
		t.Errorf("Close order = %v, want [top rec]", closeLog)
	}
}

// fakeDHT is an in-memory OpenDHT proxy: POST appends to a key, GET lists
// the key's entries one per line.
type fakeDHT struct {
	mu   sync.Mutex
	vals map[string][]string
}

func (d *fakeDHT) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/key/")
	if r.Method == http.MethodPost {
		body, _ := io.ReadAll(r.Body)
		d.vals[key] = append(d.vals[key], string(body))
	}
	_, _ = w.Write([]byte(strings.Join(d.vals[key], "\n")))
}

func (d *fakeDHT) wipe() {
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.vals)
}

func (d *fakeDHT) has(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.vals[key]) > 0
}

type traceLine struct {
	Op       string `json:"op"`
	Key      string `json:"key"`
	ValueLen int    `json:"value_len"`
	OK       bool   `json:"ok"`
}

func traceLines(t *testing.T, path string) []traceLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []traceLine
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var l traceLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("%s: line %q: %v", path, line, err)
		}
		out = append(out, l)
	}
	return out
}

// TestChainCarriesTraffic loads mirror -> [trace -> shell file store,
// trace -> opendht] through config.Load and the Manager, and checks that every
// link moved the value: Set fans out to both legs, and Get returns the first
// leg that has it.
func TestChainCarriesTraffic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell store needs /bin/sh")
	}
	script, err := filepath.Abs("../../test/e2e/filestore.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	shTrace, dhtTrace := filepath.Join(dir, "sh.jsonl"), filepath.Join(dir, "dht.jsonl")
	dht := &fakeDHT{vals: map[string][]string{}}
	proxy := httptest.NewServer(dht)
	defer proxy.Close()

	m := loadManager(t, `
stun:
  addresses: ["stun.example.net:3478"]
plugins:
  top:
    type: builtin
    name: mirror
    stores: [shleg, dhtleg]
  shleg:
    type: builtin
    name: trace
    next: files
    file: "`+shTrace+`"
  dhtleg:
    type: builtin
    name: trace
    next: dht
    file: "`+dhtTrace+`"
  files:
    type: shell
    command: /bin/sh
    args: ["`+script+`", "`+store+`"]
  dht:
    type: builtin
    name: opendht
    endpoint: `+proxy.URL+`
`)
	defer func() { _ = m.Close() }()

	top, err := m.GetPlugin("top")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const key = "00000000000000000000000000000000000000a1"
	get := pluginapi.PluginRequest{Key: key}

	if err := top.Set(ctx, pluginapi.PluginRequest{Key: key, Value: "cafe01"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(store, key)); string(got) != "cafe01\n" {
		t.Errorf("file store holds %q, want cafe01", got)
	}
	if !dht.has(key) {
		t.Error("opendht leg never received the Set")
	}
	for _, path := range []string{shTrace, dhtTrace} {
		want := []traceLine{{Op: "set", Key: key, ValueLen: 6, OK: true}}
		if got := traceLines(t, path); !slices.Equal(got, want) {
			t.Errorf("%s = %+v, want %+v", filepath.Base(path), got, want)
		}
	}

	// First leg empty: the mirror falls through to opendht.
	if err := os.Remove(filepath.Join(store, key)); err != nil {
		t.Fatal(err)
	}
	if got, err := top.Get(ctx, get); err != nil || got != "cafe01" {
		t.Errorf("Get with the file store empty = %q, %v; want cafe01 from opendht", got, err)
	}
	if got := traceLines(t, dhtTrace); got[len(got)-1] != (traceLine{Op: "get", Key: key, ValueLen: 6, OK: true}) {
		t.Errorf("dht trace last line = %+v, want a 6-byte get", got[len(got)-1])
	}

	// Second leg wiped: the value must come from the file store, and the
	// mirror must not ask opendht at all.
	if err := top.Set(ctx, pluginapi.PluginRequest{Key: key, Value: "cafe0002"}); err != nil {
		t.Fatal(err)
	}
	dht.wipe()
	dhtBefore := len(traceLines(t, dhtTrace))
	got, err := top.Get(ctx, get)
	if err != nil || got != "cafe0002" {
		t.Fatalf("Get with opendht wiped = %q, %v; want cafe0002 from the file store", got, err)
	}
	if lines := traceLines(t, shTrace); lines[len(lines)-1] != (traceLine{Op: "get", Key: key, ValueLen: len(got), OK: true}) {
		t.Errorf("shell trace last line = %+v, want a get with value_len %d", lines[len(lines)-1], len(got))
	}
	if n := len(traceLines(t, dhtTrace)); n != dhtBefore {
		t.Errorf("opendht leg traced %d calls after a Get the file store answered", n-dhtBefore)
	}
}
