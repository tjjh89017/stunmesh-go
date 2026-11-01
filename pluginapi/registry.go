package pluginapi

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// Factory creates a Store from its instance configuration.
//
// The ctx is for cancellation and for the platform dialer it carries. Do not
// perform network I/O in a factory; open connections lazily on the first Get
// or Set.
type Factory func(ctx context.Context, config PluginConfig) (Store, error)

// FactoryWithStores creates a Store that may use other plugin instances.
// The ctx follows the same rules as for Factory.
type FactoryWithStores func(ctx context.Context, config PluginConfig, stores PluginStores) (Store, error)

// PluginStores looks up the plugin instances the configuration defines.
// It is valid only inside the factory call; do not retain it, and call
// Lookup only from the factory's goroutine.
//
// The returned Store is shared and owned by the core: do not Close it. It
// outlives the instance that looked it up, since the core closes instances
// in reverse build order.
type PluginStores interface {
	Lookup(name string) (Store, error)
}

var (
	mu       sync.RWMutex
	registry = make(map[string]FactoryWithStores)
)

// Register registers a factory under name, usually from an init function.
// It panics if name is empty, f is nil, or name is already registered.
func Register(name string, f Factory) {
	if name == "" {
		panic("plugin name is empty")
	}
	if f == nil {
		panic(fmt.Sprintf("plugin %s: nil factory", name))
	}
	RegisterWithStores(name, func(ctx context.Context, config PluginConfig, _ PluginStores) (Store, error) {
		return f(ctx, config)
	})
}

// RegisterWithStores registers a factory that receives PluginStores under name.
// It panics if name is empty, f is nil, or name is already registered.
func RegisterWithStores(name string, f FactoryWithStores) {
	if name == "" {
		panic("plugin name is empty")
	}
	if f == nil {
		panic(fmt.Sprintf("plugin %s: nil factory", name))
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("plugin %s already registered", name))
	}
	registry[name] = f
}

// LookupFactory returns the factory registered under name. It exists for the
// stunmesh core, which resolves `type: builtin` instances through it, and is
// stable.
func LookupFactory(name string) (FactoryWithStores, bool) {
	mu.RLock()
	defer mu.RUnlock()

	f, ok := registry[name]
	return f, ok
}

// Registered returns the registered names in sorted order.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()

	return slices.Sorted(maps.Keys(registry))
}
