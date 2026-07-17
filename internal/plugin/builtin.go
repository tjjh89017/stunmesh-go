package plugin

import (
	"fmt"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/registry"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// NewBuiltinPlugin creates a built-in plugin instance
func NewBuiltinPlugin(config pluginapi.PluginConfig) (pluginapi.Store, error) {
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
	factory, exists := registry.Get(name)
	if !exists {
		return nil, fmt.Errorf("builtin plugin not found: %s", name)
	}

	// Create the plugin instance
	return factory(config)
}
