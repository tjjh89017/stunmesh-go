package plugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/registry"
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
func mockFactory(name string) registry.Factory {
	return func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return &mockStore{name: name}, nil
	}
}

// Helper to create a failing factory
func failingFactory(errMsg string) registry.Factory {
	return func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return nil, fmt.Errorf("%s", errMsg)
	}
}

func TestNewBuiltinPlugin_Success(t *testing.T) {
	pluginName := "test_plugin_new_success"
	registry.Register(pluginName, mockFactory(pluginName))

	config := pluginapi.PluginConfig{
		"name": pluginName,
		"key":  "value",
	}

	store, err := NewBuiltinPlugin(config)
	if err != nil {
		t.Fatalf("NewBuiltinPlugin() error = %v, want nil", err)
	}

	if store == nil {
		t.Fatal("NewBuiltinPlugin() returned nil store")
	}

	// Verify it's our mock store
	mockStore, ok := store.(*mockStore)
	if !ok {
		t.Errorf("NewBuiltinPlugin() returned wrong type: %T", store)
	} else if mockStore.name != pluginName {
		t.Errorf("NewBuiltinPlugin() store name = %q, want %q", mockStore.name, pluginName)
	}
}

func TestNewBuiltinPlugin_MissingName(t *testing.T) {
	config := pluginapi.PluginConfig{
		"key": "value",
		// Missing "name" field
	}

	store, err := NewBuiltinPlugin(config)
	if err == nil {
		t.Error("NewBuiltinPlugin() should return error when 'name' field is missing")
	}

	if store != nil {
		t.Error("NewBuiltinPlugin() should return nil store on error")
	}

	expectedMsg := "builtin plugin requires 'name' field"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewBuiltinPlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestNewBuiltinPlugin_NameNotString(t *testing.T) {
	tests := []struct {
		name      string
		nameValue interface{}
	}{
		{"int name", 123},
		{"bool name", true},
		{"slice name", []string{"test"}},
		{"map name", map[string]string{"key": "value"}},
		{"nil name", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := pluginapi.PluginConfig{
				"name": tt.nameValue,
			}

			store, err := NewBuiltinPlugin(config)
			if err == nil {
				t.Error("NewBuiltinPlugin() should return error when 'name' is not a string")
			}

			if store != nil {
				t.Error("NewBuiltinPlugin() should return nil store on error")
			}

			expectedMsg := "builtin plugin 'name' must be a string"
			if err != nil && err.Error() != expectedMsg {
				t.Errorf("NewBuiltinPlugin() error = %q, want %q", err.Error(), expectedMsg)
			}
		})
	}
}

func TestNewBuiltinPlugin_NotFound(t *testing.T) {
	pluginName := "nonexistent_plugin"
	config := pluginapi.PluginConfig{
		"name": pluginName,
	}

	store, err := NewBuiltinPlugin(config)
	if err == nil {
		t.Error("NewBuiltinPlugin() should return error for nonexistent plugin")
	}

	if store != nil {
		t.Error("NewBuiltinPlugin() should return nil store on error")
	}

	expectedMsg := fmt.Sprintf("builtin plugin not found: %s", pluginName)
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewBuiltinPlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestNewBuiltinPlugin_FactoryError(t *testing.T) {
	pluginName := "test_plugin_factory_error"
	errMsg := "factory initialization failed"
	registry.Register(pluginName, failingFactory(errMsg))

	config := pluginapi.PluginConfig{
		"name": pluginName,
	}

	store, err := NewBuiltinPlugin(config)
	if err == nil {
		t.Error("NewBuiltinPlugin() should return error when factory fails")
	}

	if store != nil {
		t.Error("NewBuiltinPlugin() should return nil store on error")
	}

	if err != nil && err.Error() != errMsg {
		t.Errorf("NewBuiltinPlugin() error = %q, want %q", err.Error(), errMsg)
	}
}
