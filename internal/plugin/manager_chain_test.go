package plugin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

var errChainBoom = errors.New("boom")

// chainStore forwards to its first inner store and records its Close.
type chainStore struct {
	name     string
	inner    pluginapi.Store
	inners   []pluginapi.Store
	closeLog *[]string
}

func (s *chainStore) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	if s.inner == nil {
		return "", fmt.Errorf("%s has no inner store", s.name)
	}
	return s.inner.Get(ctx, req)
}

func (s *chainStore) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	if s.inner == nil {
		return fmt.Errorf("%s has no inner store", s.name)
	}
	return s.inner.Set(ctx, req)
}

func (s *chainStore) Close() error {
	*s.closeLog = append(*s.closeLog, s.name)
	return nil
}

// chainHarness is a manager whose only built-in is "chain". A chain instance
// takes its own instance name from "id", its inner instances from "next"
// (a string or []string), fails when "fail" is set, and returns no store
// when "nil" is set.
type chainHarness struct {
	m        *Manager
	closeLog []string
	builds   map[string]int
	stores   map[string]pluginapi.PluginStores
}

func newChainHarness() *chainHarness {
	h := &chainHarness{builds: map[string]int{}, stores: map[string]pluginapi.PluginStores{}}
	factory := func(ctx context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
		id, _ := config["id"].(string)
		h.builds[id]++
		h.stores[id] = stores
		if config["fail"] == true {
			return nil, errChainBoom
		}
		if config["nil"] == true {
			return nil, nil
		}
		s := &chainStore{name: id, closeLog: &h.closeLog}
		var nexts []string
		switch v := config["next"].(type) {
		case string:
			nexts = []string{v}
		case []string:
			nexts = v
		}
		for _, next := range nexts {
			inner, err := stores.Lookup(next)
			if err != nil {
				return nil, fmt.Errorf("chain %s: %w", id, err)
			}
			s.inners = append(s.inners, inner)
		}
		if len(s.inners) > 0 {
			s.inner = s.inners[0]
		}
		return s, nil
	}
	h.m = newManagerWithLookup(func(name string) (pluginapi.FactoryWithStores, bool) {
		if name == "chain" {
			return factory, true
		}
		return nil, false
	})
	return h
}

// chain returns a definition for a chain instance named id using next ("" for none).
func chain(id, next string) pluginapi.PluginDefinition {
	cfg := pluginapi.PluginConfig{"name": "chain", "id": id}
	if next != "" {
		cfg["next"] = next
	}
	return pluginapi.PluginDefinition{Type: "builtin", Config: cfg}
}

func (h *chainHarness) assertEmpty(t *testing.T) {
	t.Helper()
	if len(h.m.plugins) != 0 || len(h.m.order) != 0 || len(h.m.dedup) != 0 {
		t.Errorf("manager not empty: plugins=%v order=%v dedup=%v", h.m.plugins, h.m.order, h.m.dedup)
	}
}

func TestLoadPlugins_ChainBuildOrder(t *testing.T) {
	tests := []struct {
		name string
		defs map[string]pluginapi.PluginDefinition
		want []string
	}{
		{
			name: "outer sorts first",
			defs: map[string]pluginapi.PluginDefinition{"a": chain("a", "b"), "b": chain("b", "c"), "c": chain("c", "")},
			want: []string{"c", "b", "a"},
		},
		{
			name: "inner sorts first",
			defs: map[string]pluginapi.PluginDefinition{"z": chain("z", "y"), "y": chain("y", "x"), "x": chain("x", "")},
			want: []string{"x", "y", "z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 20 {
				h := newChainHarness()
				if err := h.m.LoadPlugins(context.Background(), tt.defs); err != nil {
					t.Fatalf("LoadPlugins() error = %v", err)
				}
				if !slices.Equal(h.m.order, tt.want) {
					t.Fatalf("order = %v, want %v", h.m.order, tt.want)
				}
				for name, n := range h.builds {
					if n != 1 {
						t.Errorf("%s built %d times, want 1", name, n)
					}
				}
			}
		})
	}
}

func TestLoadPlugins_ChainCloseReverseAndIdempotent(t *testing.T) {
	h := newChainHarness()
	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", "b"), "b": chain("b", "c"), "c": chain("c", "")}
	if err := h.m.LoadPlugins(context.Background(), defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}

	if err := h.m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := h.m.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if want := []string{"a", "b", "c"}; !slices.Equal(h.closeLog, want) {
		t.Errorf("close order = %v, want %v", h.closeLog, want)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_SharedInnerBuiltOnceClosedLast(t *testing.T) {
	h := newChainHarness()
	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", "shared"), "b": chain("b", "shared"), "shared": chain("shared", "")}
	if err := h.m.LoadPlugins(context.Background(), defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}

	if h.builds["shared"] != 1 {
		t.Errorf("shared built %d times, want 1", h.builds["shared"])
	}
	a, _ := h.m.GetPlugin("a")
	b, _ := h.m.GetPlugin("b")
	shared, _ := h.m.GetPlugin("shared")
	if a.(*chainStore).inner != shared || b.(*chainStore).inner != shared {
		t.Error("a and b do not share the same inner instance")
	}

	if err := h.m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if want := []string{"b", "a", "shared"}; !slices.Equal(h.closeLog, want) {
		t.Errorf("close order = %v, want %v", h.closeLog, want)
	}
}

func TestLoadPlugins_CycleRollsBack(t *testing.T) {
	tests := []struct {
		name     string
		defs     map[string]pluginapi.PluginDefinition
		wantPath string
	}{
		{
			name:     "two instances",
			defs:     map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", "c"), "c": chain("c", "b")},
			wantPath: "plugin cycle: b -> c -> b",
		},
		{
			name:     "self",
			defs:     map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", "b")},
			wantPath: "plugin cycle: b -> b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newChainHarness()
			err := h.m.LoadPlugins(context.Background(), tt.defs)
			if err == nil || !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("LoadPlugins() error = %v, want it to contain %q", err, tt.wantPath)
			}
			// "a" sorts first and was built before the cycle was found.
			if !slices.Equal(h.closeLog, []string{"a"}) {
				t.Errorf("closed = %v, want [a]", h.closeLog)
			}
			h.assertEmpty(t)
		})
	}
}

func TestLoadPlugins_FactoryReturnsNoStore(t *testing.T) {
	h := newChainHarness()
	empty := chain("b", "")
	empty.Config["nil"] = true
	err := h.m.LoadPlugins(context.Background(), map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": empty})
	want := `plugin "b": factory returned no store`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadPlugins() error = %v, want it to contain %q", err, want)
	}
	if !slices.Equal(h.closeLog, []string{"a"}) {
		t.Errorf("closed = %v, want [a]", h.closeLog)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_PassesContextToFactory(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	var got []any
	m := newManagerWithLookup(func(string) (pluginapi.FactoryWithStores, bool) {
		return func(ctx context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
			got = append(got, ctx.Value(ctxKey{}))
			if next, ok := config["next"].(string); ok {
				if _, err := stores.Lookup(next); err != nil {
					return nil, err
				}
			}
			return &fakePlainStore{}, nil
		}, true
	})
	defs := map[string]pluginapi.PluginDefinition{
		"a": {Type: "builtin", Config: pluginapi.PluginConfig{"name": "x", "next": "b"}},
		"b": {Type: "builtin", Config: pluginapi.PluginConfig{"name": "x"}},
	}
	if err := m.LoadPlugins(ctx, defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}
	if len(got) != 2 || got[0] != "v" || got[1] != "v" {
		t.Errorf("factory ctx values = %v, want [v v]", got)
	}
}

func TestLoadPlugins_UndefinedReference(t *testing.T) {
	h := newChainHarness()
	err := h.m.LoadPlugins(context.Background(), map[string]pluginapi.PluginDefinition{"a": chain("a", "missing")})
	want := `plugin "missing" referenced by "a" is not defined`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadPlugins() error = %v, want it to contain %q", err, want)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_InnerErrorWrappedWithPath(t *testing.T) {
	h := newChainHarness()
	inner := chain("b", "")
	inner.Config["fail"] = true
	err := h.m.LoadPlugins(context.Background(), map[string]pluginapi.PluginDefinition{"a": chain("a", "b"), "b": inner})
	if !errors.Is(err, errChainBoom) {
		t.Fatalf("LoadPlugins() error = %v, want it to wrap %v", err, errChainBoom)
	}
	want := "failed to create plugin a: chain a: failed to create plugin b: boom"
	if err.Error() != want {
		t.Errorf("LoadPlugins() error = %q, want %q", err.Error(), want)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_StoresUnusableAfterConstruction(t *testing.T) {
	h := newChainHarness()
	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", "")}
	if err := h.m.LoadPlugins(context.Background(), defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		_, err := h.stores["a"].Lookup(name)
		want := `plugin "a": PluginStores used after construction`
		if err == nil || err.Error() != want {
			t.Errorf("retained stores.Lookup(%s) error = %v, want %q", name, err, want)
		}
	}
}

func TestLoadPlugins_Diamond(t *testing.T) {
	h := newChainHarness()
	top := chain("a", "")
	top.Config["next"] = []string{"b", "c"}
	defs := map[string]pluginapi.PluginDefinition{"a": top, "b": chain("b", "d"), "c": chain("c", "d"), "d": chain("d", "")}
	if err := h.m.LoadPlugins(context.Background(), defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}
	if h.builds["d"] != 1 {
		t.Errorf("d built %d times, want 1", h.builds["d"])
	}
	if want := []string{"d", "b", "c", "a"}; !slices.Equal(h.m.order, want) {
		t.Errorf("order = %v, want %v", h.m.order, want)
	}
}

func TestLoadPlugins_DepthThreeCycle(t *testing.T) {
	h := newChainHarness()
	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", "b"), "b": chain("b", "c"), "c": chain("c", "a")}
	err := h.m.LoadPlugins(context.Background(), defs)
	want := "plugin cycle: a -> b -> c -> a"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadPlugins() error = %v, want it to contain %q", err, want)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_RollbackClosesInReverse(t *testing.T) {
	h := newChainHarness()
	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", ""), "z": chain("z", "missing")}
	if err := h.m.LoadPlugins(context.Background(), defs); err == nil {
		t.Fatal("LoadPlugins() error = nil, want undefined reference")
	}
	if want := []string{"b", "a"}; !slices.Equal(h.closeLog, want) {
		t.Errorf("close order = %v, want %v", h.closeLog, want)
	}
	h.assertEmpty(t)
}

func TestLoadPlugins_SecondCallRollbackKeepsEarlierInstances(t *testing.T) {
	h := newChainHarness()
	ctx := context.Background()
	if err := h.m.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{"a": chain("a", "")}); err != nil {
		t.Fatalf("first LoadPlugins() error = %v", err)
	}
	a, _ := h.m.GetPlugin("a")

	defs := map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", "a"), "c": chain("c", "missing")}
	if err := h.m.LoadPlugins(ctx, defs); err == nil {
		t.Fatal("second LoadPlugins() error = nil, want undefined reference")
	}
	if want := []string{"b"}; !slices.Equal(h.closeLog, want) {
		t.Errorf("closed = %v, want %v", h.closeLog, want)
	}
	if got, err := h.m.GetPlugin("a"); err != nil || got != a {
		t.Errorf("GetPlugin(a) = %v, %v, want the original instance", got, err)
	}
	if want := []string{"a"}; !slices.Equal(h.m.order, want) {
		t.Errorf("order = %v, want %v", h.m.order, want)
	}
}

func TestLoadPlugins_SecondCallReusesExisting(t *testing.T) {
	h := newChainHarness()
	ctx := context.Background()
	if err := h.m.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{"a": chain("a", "b"), "b": chain("b", "")}); err != nil {
		t.Fatalf("first LoadPlugins() error = %v", err)
	}
	a, _ := h.m.GetPlugin("a")

	// A changed definition for an existing name is ignored.
	if err := h.m.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{"a": chain("a", ""), "b": chain("b", "")}); err != nil {
		t.Fatalf("second LoadPlugins() error = %v", err)
	}
	if h.builds["a"] != 1 || h.builds["b"] != 1 {
		t.Errorf("builds = %v, want each once", h.builds)
	}
	if got, _ := h.m.GetPlugin("a"); got != a || got.(*chainStore).inner == nil {
		t.Error("second LoadPlugins() replaced or rebuilt a")
	}
	if want := []string{"b", "a"}; !slices.Equal(h.m.order, want) {
		t.Errorf("order = %v, want %v", h.m.order, want)
	}
}

func TestLoadPlugins_StoresToExternalInner(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		typ     string
		fixture string
	}{
		{"exec", "exec_test_plugin.sh"},
		{"shell", "shell_test_plugin.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			fixture, err := filepath.Abs(getTestPluginPath(tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			h := newChainHarness()
			defs := map[string]pluginapi.PluginDefinition{
				"outer": chain("outer", "inner"),
				"inner": {Type: tt.typ, Config: pluginapi.PluginConfig{"command": fixture}},
			}
			if err := h.m.LoadPlugins(context.Background(), defs); err != nil {
				t.Fatalf("LoadPlugins() error = %v", err)
			}
			if want := []string{"inner", "outer"}; !slices.Equal(h.m.order, want) {
				t.Errorf("order = %v, want %v", h.m.order, want)
			}

			outer, _ := h.m.GetPlugin("outer")
			inner, _ := h.m.GetPlugin("inner")
			ctx := context.Background()
			req := testRequest("chain_"+tt.typ+"_key", "wg0")
			req.Value = "chainvalue"
			if err := outer.Set(ctx, req); err != nil {
				t.Fatalf("outer.Set() error = %v", err)
			}
			req.Value = ""
			got, err := inner.Get(ctx, req)
			if err != nil || got != "chainvalue" {
				t.Errorf("inner.Get() = %q, %v, want %q", got, err, "chainvalue")
			}
		})
	}
}

var registerLegacyOnce sync.Once

func TestLoadPlugins_LegacyRegisterThroughManager(t *testing.T) {
	name := "test_manager_legacy_register"
	registerLegacyOnce.Do(func() {
		pluginapi.Register(name, func(ctx context.Context, config pluginapi.PluginConfig) (pluginapi.Store, error) {
			return &fakePlainStore{}, nil
		})
	})

	m := NewManager()
	defs := map[string]pluginapi.PluginDefinition{
		"legacy": {Type: "builtin", Config: pluginapi.PluginConfig{"name": name}},
	}
	if err := m.LoadPlugins(context.Background(), defs); err != nil {
		t.Fatalf("LoadPlugins() error = %v", err)
	}
	if _, err := m.GetPlugin("legacy"); err != nil {
		t.Errorf("GetPlugin(legacy) error = %v", err)
	}
}
