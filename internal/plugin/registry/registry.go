// Package registry holds the built-in plugin registry.
//
// It exists as its own package so that built-in plugins can register
// themselves from their init function. The registry cannot live in package
// plugin, because plugin would then have to import each built-in to register
// it, while each built-in imports the registry to register itself.
package registry

import (
	"fmt"
	"sync"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// Factory creates a new Store instance from configuration
type Factory func(config pluginapi.PluginConfig) (pluginapi.Store, error)

// registry holds all registered built-in plugins
var (
	registry = make(map[string]Factory)
	mu       sync.RWMutex
)

// Register registers a built-in plugin factory with the given name.
// This should be called from a built-in's init function.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("builtin plugin %s already registered", name))
	}

	registry[name] = factory
}

// Get returns the factory registered under the given name
func Get(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()

	factory, exists := registry[name]
	return factory, exists
}

// Names returns a list of registered built-in plugin names
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
