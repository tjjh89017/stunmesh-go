package plugin

import (
	"context"
	"fmt"
	"sync"
)

// BuiltinFactory creates a new Store instance from configuration
type BuiltinFactory func(config PluginConfig) (Store, error)

// builtinRegistry holds all registered built-in plugins
var (
	builtinRegistry = make(map[string]BuiltinFactory)
	registryMu      sync.RWMutex
)

// RegisterBuiltin registers a built-in plugin factory with the given name
// This should be called during package initialization
func RegisterBuiltin(name string, factory BuiltinFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := builtinRegistry[name]; exists {
		panic(fmt.Sprintf("builtin plugin %s already registered", name))
	}

	builtinRegistry[name] = factory
}

// NewBuiltinPlugin creates a built-in plugin instance
func NewBuiltinPlugin(config PluginConfig) (Store, error) {
	// Get the builtin plugin name from config
	nameInterface, ok := config["name"]
	if !ok {
		return nil, fmt.Errorf("builtin plugin requires 'name' field")
	}

	name, ok := nameInterface.(string)
	if !ok {
		return nil, fmt.Errorf("builtin plugin 'name' must be a string")
	}

	// Look up the factory
	registryMu.RLock()
	factory, exists := builtinRegistry[name]
	registryMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("builtin plugin not found: %s", name)
	}

	// Create the plugin instance
	return factory(config)
}

// GetRegisteredBuiltins returns a list of registered built-in plugin names
func GetRegisteredBuiltins() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(builtinRegistry))
	for name := range builtinRegistry {
		names = append(names, name)
	}
	return names
}

// BuiltinConfig wraps the config and provides helper methods
type BuiltinConfig struct {
	config PluginConfig
}

// NewBuiltinConfig creates a new BuiltinConfig wrapper
func NewBuiltinConfig(config PluginConfig) *BuiltinConfig {
	return &BuiltinConfig{config: config}
}

// GetString retrieves a string value from config
func (c *BuiltinConfig) GetString(key string) (string, bool) {
	val, ok := c.config[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetStringRequired retrieves a required string value
func (c *BuiltinConfig) GetStringRequired(key string) (string, error) {
	val, ok := c.GetString(key)
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}

// GetContext returns a base context (can be extended with values if needed)
func (c *BuiltinConfig) GetContext() context.Context {
	return context.Background()
}
