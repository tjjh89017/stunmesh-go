// Package passthrough provides the "passthrough" built-in, which forwards
// every Get and Set unchanged to another plugin instance.
//
//	plugins:
//	  dht:
//	    type: builtin
//	    name: opendht
//	    endpoint: https://dhtproxy.example.net
//	  front:
//	    type: builtin
//	    name: passthrough
//	    next: dht
//
// next is this plugin's own field: the name of the inner instance.
package passthrough

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/pluginconfig"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

const configKeyNext = "next"

func init() {
	pluginapi.RegisterWithStores("passthrough", New)
}

// plugin has no Close: the inner instance belongs to the plugin manager.
type plugin struct {
	next pluginapi.Store
}

// New builds a passthrough instance from its config, resolving next through stores.
func New(_ context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
	name, err := pluginconfig.NewConfig(config).GetStringRequired(configKeyNext)
	if err != nil {
		return nil, err
	}
	next, err := stores.Lookup(name)
	if err != nil {
		return nil, err
	}
	return &plugin{next: next}, nil
}

func (p *plugin) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	return p.next.Get(ctx, req)
}

func (p *plugin) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	return p.next.Set(ctx, req)
}
