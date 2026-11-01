package pluginapi_test

import (
	"context"
	"fmt"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// wrapper forwards every call to its inner store.
type wrapper struct {
	inner pluginapi.Store
}

func (m *wrapper) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	return m.inner.Get(ctx, req)
}

func (m *wrapper) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	return m.inner.Set(ctx, req)
}

// An outer plugin reads the inner instance name from its own config. The
// field name ("next" here) is the plugin's choice; the core reserves none.
func ExampleRegisterWithStores() {
	pluginapi.RegisterWithStores("example_wrapper", func(ctx context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
		next, ok := config["next"].(string)
		if !ok {
			return nil, fmt.Errorf("example_wrapper: 'next' must name a plugin instance")
		}
		inner, err := stores.Lookup(next)
		if err != nil {
			return nil, err
		}
		return &wrapper{inner: inner}, nil
	})

	_, ok := pluginapi.LookupFactory("example_wrapper")
	fmt.Println(ok)
	// Output: true
}
