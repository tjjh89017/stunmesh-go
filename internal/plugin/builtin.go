package plugin

import (
	"context"
	"fmt"
	"strings"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// NewBuiltinPlugin creates a built-in plugin instance
func NewBuiltinPlugin(ctx context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
	return newBuiltinPlugin(ctx, config, stores, pluginapi.LookupFactory)
}

func newBuiltinPlugin(ctx context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores, lookup lookupFunc) (pluginapi.Store, error) {
	nameInterface, ok := config["name"]
	if !ok {
		return nil, fmt.Errorf("builtin plugin requires 'name' field")
	}

	name, ok := nameInterface.(string)
	if !ok {
		return nil, fmt.Errorf("builtin plugin 'name' must be a string")
	}

	factory, exists := lookup(name)
	if !exists {
		return nil, fmt.Errorf("builtin plugin not found: %s (registered: [%s]; the binary must import the package that registers it)",
			name, strings.Join(pluginapi.Registered(), ", "))
	}

	return factory(ctx, config, stores)
}
