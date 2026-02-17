package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
func mockFactory(name string) BuiltinFactory {
	return func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return &mockStore{name: name}, nil
	}
}

// Helper to create a failing factory
func failingFactory(errMsg string) BuiltinFactory {
	return func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return nil, fmt.Errorf("%s", errMsg)
	}
}

// Save and restore registry for testing
func saveRegistry() (map[string]BuiltinFactory, *sync.RWMutex) {
	registryMu.Lock()
	defer registryMu.Unlock()

	saved := make(map[string]BuiltinFactory)
	for k, v := range builtinRegistry {
		saved[k] = v
	}
	return saved, &registryMu
}

func restoreRegistry(saved map[string]BuiltinFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	// Clear current registry
	for k := range builtinRegistry {
		delete(builtinRegistry, k)
	}

	// Restore saved registry
	for k, v := range saved {
		builtinRegistry[k] = v
	}
}

func TestRegisterBuiltin_Success(t *testing.T) {
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]BuiltinFactory))

	pluginName := "test_plugin_success"
	factory := mockFactory(pluginName)

	// Should not panic
	RegisterBuiltin(pluginName, factory)

	// Verify plugin is registered
	registryMu.RLock()
	_, exists := builtinRegistry[pluginName]
	registryMu.RUnlock()

	if !exists {
		t.Errorf("RegisterBuiltin() did not register plugin %s", pluginName)
	}
}

func TestRegisterBuiltin_Duplicate(t *testing.T) {
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]BuiltinFactory))

	pluginName := "test_plugin_duplicate"
	factory := mockFactory(pluginName)

	// First registration should succeed
	RegisterBuiltin(pluginName, factory)

	// Second registration should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("RegisterBuiltin() should panic on duplicate registration")
		} else {
			panicMsg := fmt.Sprintf("%v", r)
			expectedMsg := fmt.Sprintf("builtin plugin %s already registered", pluginName)
			if panicMsg != expectedMsg {
				t.Errorf("RegisterBuiltin() panic message = %q, want %q", panicMsg, expectedMsg)
			}
		}
	}()

	RegisterBuiltin(pluginName, factory)
}

func TestNewBuiltinPlugin_Success(t *testing.T) {
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry and register test plugin
	restoreRegistry(make(map[string]BuiltinFactory))

	pluginName := "test_plugin_new_success"
	RegisterBuiltin(pluginName, mockFactory(pluginName))

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
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]BuiltinFactory))

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
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry and register failing plugin
	restoreRegistry(make(map[string]BuiltinFactory))

	pluginName := "test_plugin_factory_error"
	errMsg := "factory initialization failed"
	RegisterBuiltin(pluginName, failingFactory(errMsg))

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

func TestGetRegisteredBuiltins(t *testing.T) {
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry and register test plugins
	restoreRegistry(make(map[string]BuiltinFactory))

	expectedPlugins := []string{"plugin1", "plugin2", "plugin3"}
	for _, name := range expectedPlugins {
		RegisterBuiltin(name, mockFactory(name))
	}

	registeredPlugins := GetRegisteredBuiltins()

	if len(registeredPlugins) != len(expectedPlugins) {
		t.Errorf("GetRegisteredBuiltins() returned %d plugins, want %d", len(registeredPlugins), len(expectedPlugins))
	}

	// Check all expected plugins are in the result
	pluginMap := make(map[string]bool)
	for _, name := range registeredPlugins {
		pluginMap[name] = true
	}

	for _, expected := range expectedPlugins {
		if !pluginMap[expected] {
			t.Errorf("GetRegisteredBuiltins() missing plugin %q", expected)
		}
	}
}

func TestGetRegisteredBuiltins_Empty(t *testing.T) {
	// Save original registry
	saved, _ := saveRegistry()
	defer restoreRegistry(saved)

	// Clear registry for test
	restoreRegistry(make(map[string]BuiltinFactory))

	registeredPlugins := GetRegisteredBuiltins()

	if len(registeredPlugins) != 0 {
		t.Errorf("GetRegisteredBuiltins() on empty registry returned %d plugins, want 0", len(registeredPlugins))
	}
}

func TestBuiltinConfig_GetString(t *testing.T) {
	tests := []struct {
		name      string
		config    pluginapi.PluginConfig
		key       string
		wantValue string
		wantOk    bool
	}{
		{
			name:      "existing string key",
			config:    pluginapi.PluginConfig{"testkey": "testvalue"},
			key:       "testkey",
			wantValue: "testvalue",
			wantOk:    true,
		},
		{
			name:      "nonexistent key",
			config:    pluginapi.PluginConfig{"other": "value"},
			key:       "testkey",
			wantValue: "",
			wantOk:    false,
		},
		{
			name:      "non-string value",
			config:    pluginapi.PluginConfig{"testkey": 123},
			key:       "testkey",
			wantValue: "",
			wantOk:    false,
		},
		{
			name:      "empty string value",
			config:    pluginapi.PluginConfig{"testkey": ""},
			key:       "testkey",
			wantValue: "",
			wantOk:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBuiltinConfig(tt.config)
			gotValue, gotOk := bc.GetString(tt.key)

			if gotValue != tt.wantValue {
				t.Errorf("GetString() value = %q, want %q", gotValue, tt.wantValue)
			}

			if gotOk != tt.wantOk {
				t.Errorf("GetString() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestBuiltinConfig_GetStringRequired(t *testing.T) {
	tests := []struct {
		name    string
		config  pluginapi.PluginConfig
		key     string
		want    string
		wantErr bool
	}{
		{
			name:    "existing string key",
			config:  pluginapi.PluginConfig{"testkey": "testvalue"},
			key:     "testkey",
			want:    "testvalue",
			wantErr: false,
		},
		{
			name:    "nonexistent key",
			config:  pluginapi.PluginConfig{"other": "value"},
			key:     "testkey",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string value",
			config:  pluginapi.PluginConfig{"testkey": 123},
			key:     "testkey",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBuiltinConfig(tt.config)
			got, err := bc.GetStringRequired(tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetStringRequired() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("GetStringRequired() = %q, want %q", got, tt.want)
			}

			if err != nil && tt.wantErr {
				expectedErrMsg := fmt.Sprintf("%s is required", tt.key)
				if !strings.Contains(err.Error(), expectedErrMsg) {
					t.Errorf("GetStringRequired() error = %q, want error containing %q", err.Error(), expectedErrMsg)
				}
			}
		})
	}
}

func TestBuiltinConfig_GetContext(t *testing.T) {
	config := pluginapi.PluginConfig{"key": "value"}
	bc := NewBuiltinConfig(config)

	ctx := bc.GetContext()
	if ctx == nil {
		t.Fatal("GetContext() returned nil context")
	}

	// Verify it's a background context (or at least a valid context)
	if ctx.Err() != nil {
		t.Errorf("GetContext() returned context with error: %v", ctx.Err())
	}
}

func TestNewBuiltinConfig(t *testing.T) {
	config := pluginapi.PluginConfig{
		"key1": "value1",
		"key2": 123,
	}

	bc := NewBuiltinConfig(config)

	if bc == nil {
		t.Fatal("NewBuiltinConfig() returned nil")
		return
	}

	if bc.config == nil {
		t.Error("NewBuiltinConfig() config field is nil")
	}

	// Verify the config is properly stored
	if val, ok := bc.config["key1"]; !ok || val != "value1" {
		t.Error("NewBuiltinConfig() did not properly store config")
	}
}
