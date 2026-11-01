package pluginapi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
)

type mockStore struct {
	name string
}

func (m *mockStore) Get(ctx context.Context, req PluginRequest) (string, error) {
	return fmt.Sprintf("value_%s", req.Key), nil
}

func (m *mockStore) Set(ctx context.Context, req PluginRequest) error {
	return nil
}

func mockFactory(name string) Factory {
	return func(ctx context.Context, config PluginConfig) (Store, error) {
		return &mockStore{name: name}, nil
	}
}

// withEmptyRegistry runs the test against an empty registry and restores the
// original afterwards.
func withEmptyRegistry(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := maps.Clone(registry)
	clear(registry)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		clear(registry)
		maps.Copy(registry, saved)
	})
}

func expectPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		if got := fmt.Sprint(r); got != want {
			t.Errorf("panic = %q, want %q", got, want)
		}
	}()
	f()
}

func TestRegister_Success(t *testing.T) {
	withEmptyRegistry(t)

	Register("test_plugin_success", mockFactory("test_plugin_success"))

	if _, ok := LookupFactory("test_plugin_success"); !ok {
		t.Error("Register() did not register the plugin")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	withEmptyRegistry(t)

	Register("dup", mockFactory("dup"))
	expectPanic(t, "plugin dup already registered", func() {
		Register("dup", mockFactory("dup"))
	})
}

func TestRegisterWithStores_Duplicate(t *testing.T) {
	withEmptyRegistry(t)

	f := func(context.Context, PluginConfig, PluginStores) (Store, error) { return &mockStore{}, nil }
	RegisterWithStores("dup", f)
	expectPanic(t, "plugin dup already registered", func() {
		RegisterWithStores("dup", f)
	})
	expectPanic(t, "plugin dup already registered", func() {
		Register("dup", mockFactory("dup"))
	})
}

func TestRegister_EmptyName(t *testing.T) {
	withEmptyRegistry(t)

	expectPanic(t, "plugin name is empty", func() {
		Register("", mockFactory(""))
	})
	expectPanic(t, "plugin name is empty", func() {
		RegisterWithStores("", func(context.Context, PluginConfig, PluginStores) (Store, error) { return &mockStore{}, nil })
	})
	if got := Registered(); len(got) != 0 {
		t.Errorf("Registered() = %v, want empty", got)
	}
}

func TestRegister_NilFactory(t *testing.T) {
	withEmptyRegistry(t)

	expectPanic(t, "plugin nil_plain: nil factory", func() {
		Register("nil_plain", nil)
	})
	expectPanic(t, "plugin nil_stores: nil factory", func() {
		RegisterWithStores("nil_stores", nil)
	})
	if got := Registered(); len(got) != 0 {
		t.Errorf("Registered() = %v, want empty", got)
	}
}

func TestRegister_AdapterPassesConfigAndIgnoresStores(t *testing.T) {
	withEmptyRegistry(t)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	var gotConfig PluginConfig
	var gotCtx context.Context
	Register("legacy", func(ctx context.Context, config PluginConfig) (Store, error) {
		gotCtx = ctx
		gotConfig = config
		return &mockStore{name: "legacy"}, nil
	})

	factory, ok := LookupFactory("legacy")
	if !ok {
		t.Fatal("LookupFactory(legacy) not found")
	}
	store, err := factory(ctx, PluginConfig{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("factory error = %v", err)
	}
	if s, ok := store.(*mockStore); !ok || s.name != "legacy" {
		t.Errorf("factory returned %#v", store)
	}
	if gotConfig["k"] != "v" {
		t.Errorf("config not passed through: %v", gotConfig)
	}
	if gotCtx == nil || gotCtx.Value(ctxKey{}) != "v" {
		t.Error("ctx not passed through")
	}
}

func TestRegister_AdapterPropagatesError(t *testing.T) {
	withEmptyRegistry(t)

	want := errors.New("boom")
	Register("failing", func(context.Context, PluginConfig) (Store, error) { return nil, want })

	factory, _ := LookupFactory("failing")
	if _, err := factory(context.Background(), PluginConfig{}, nil); !errors.Is(err, want) {
		t.Errorf("factory error = %v, want %v", err, want)
	}
}

func TestLookupFactory_NotFound(t *testing.T) {
	withEmptyRegistry(t)

	factory, ok := LookupFactory("nonexistent_plugin")
	if ok || factory != nil {
		t.Error("LookupFactory() returned a factory for an unregistered plugin")
	}
}

func TestRegistered_Sorted(t *testing.T) {
	withEmptyRegistry(t)

	if got := Registered(); len(got) != 0 {
		t.Errorf("Registered() on empty registry = %v, want empty", got)
	}
	for _, name := range []string{"zeta", "alpha", "mid"} {
		Register(name, mockFactory(name))
	}
	if got, want := Registered(), []string{"alpha", "mid", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("Registered() = %v, want %v", got, want)
	}
}
