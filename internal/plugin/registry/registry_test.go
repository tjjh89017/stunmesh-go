package registry

import (
	"context"
	"fmt"
	"maps"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// Mock Store implementation for testing
type mockStore struct {
	name string
}

func (m *mockStore) Get(ctx context.Context, key string) (string, error) {
	return fmt.Sprintf("value_%s", key), nil
}

func (m *mockStore) Set(ctx context.Context, key string, value string) error {
	return nil
}

// Helper to create a mock factory
func mockFactory(name string) Factory {
	return func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return &mockStore{name: name}, nil
	}
}

// Save and restore registry for testing
func saveRegistry() map[string]Factory {
	mu.Lock()
	defer mu.Unlock()

	saved := make(map[string]Factory)
	maps.Copy(saved, registry)
	return saved
}

func restoreRegistry(saved map[string]Factory) {
	mu.Lock()
	defer mu.Unlock()

	clear(registry)
	maps.Copy(registry, saved)
}

func TestRegister_Success(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]Factory))

	pluginName := "test_plugin_success"
	factory := mockFactory(pluginName)

	// Should not panic
	Register(pluginName, factory)

	// Verify plugin is registered
	mu.RLock()
	_, exists := registry[pluginName]
	mu.RUnlock()

	if !exists {
		t.Errorf("Register() did not register plugin %s", pluginName)
	}
}

func TestRegister_Duplicate(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]Factory))

	pluginName := "test_plugin_duplicate"
	factory := mockFactory(pluginName)

	// First registration should succeed
	Register(pluginName, factory)

	// Second registration should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register() should panic on duplicate registration")
		} else {
			panicMsg := fmt.Sprintf("%v", r)
			expectedMsg := fmt.Sprintf("builtin plugin %s already registered", pluginName)
			if panicMsg != expectedMsg {
				t.Errorf("Register() panic message = %q, want %q", panicMsg, expectedMsg)
			}
		}
	}()

	Register(pluginName, factory)
}

func TestGet_Success(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]Factory))

	pluginName := "test_plugin_get"
	Register(pluginName, mockFactory(pluginName))

	factory, exists := Get(pluginName)
	if !exists {
		t.Fatalf("Get(%q) reported the plugin as not registered", pluginName)
	}

	store, err := factory(pluginapi.PluginConfig{})
	if err != nil {
		t.Fatalf("factory returned error = %v, want nil", err)
	}

	mockStore, ok := store.(*mockStore)
	if !ok {
		t.Fatalf("factory returned wrong type: %T", store)
	}

	if mockStore.name != pluginName {
		t.Errorf("factory store name = %q, want %q", mockStore.name, pluginName)
	}
}

func TestGet_NotFound(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]Factory))

	factory, exists := Get("nonexistent_plugin")
	if exists {
		t.Error("Get() reported an unregistered plugin as registered")
	}

	if factory != nil {
		t.Error("Get() returned a factory for an unregistered plugin")
	}
}

func TestNames(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry and register test plugins
	restoreRegistry(make(map[string]Factory))

	expectedPlugins := []string{"plugin1", "plugin2", "plugin3"}
	for _, name := range expectedPlugins {
		Register(name, mockFactory(name))
	}

	registeredPlugins := Names()

	if len(registeredPlugins) != len(expectedPlugins) {
		t.Errorf("Names() returned %d plugins, want %d", len(registeredPlugins), len(expectedPlugins))
	}

	// Check all expected plugins are in the result
	pluginMap := make(map[string]bool)
	for _, name := range registeredPlugins {
		pluginMap[name] = true
	}

	for _, expected := range expectedPlugins {
		if !pluginMap[expected] {
			t.Errorf("Names() missing plugin %q", expected)
		}
	}
}

func TestNames_Empty(t *testing.T) {
	// Save original registry
	saved := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]Factory))

	registeredPlugins := Names()

	if len(registeredPlugins) != 0 {
		t.Errorf("Names() on empty registry returned %d plugins, want 0", len(registeredPlugins))
	}
}
