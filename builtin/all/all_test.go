package all_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	for _, name := range []string{"cloudflare", "mirror", "opendht", "passthrough"} {
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

func TestChainThroughPassthrough(t *testing.T) {
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
    name: passthrough
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

func TestMirrorOverShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture needs /bin/sh")
	}
	script, err := filepath.Abs("../../internal/plugin/testdata/shell_test_plugin.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := loadManager(t, `
stun:
  addresses: ["stun.example.net:3478"]
plugins:
  both:
    type: builtin
    name: mirror
    stores: [sh, front]
  front:
    type: builtin
    name: passthrough
    next: sh
  sh:
    type: shell
    command: `+script+`
`)
	defer func() { _ = m.Close() }()

	both, err := m.GetPlugin("both")
	if err != nil {
		t.Fatal(err)
	}
	req := pluginapi.PluginRequest{Key: "00000000000000000000000000000000000000a1", Value: "cafe"}
	if err := both.Set(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	front, _ := m.GetPlugin("front")
	got, err := front.Get(context.Background(), pluginapi.PluginRequest{Key: req.Key})
	if err != nil {
		t.Fatal(err)
	}
	if got != "cafe" {
		t.Errorf("Get through passthrough = %q, want cafe", got)
	}
}
