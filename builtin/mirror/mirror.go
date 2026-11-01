// Package mirror is a working plugin and the reference example for a
// chaining plugin with several inner instances: Set writes to every instance
// named in its "stores" config field, and Get returns the first non-empty
// value in that order.
package mirror

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func init() {
	pluginapi.RegisterWithStores("mirror", New)
}

type inner struct {
	name  string
	store pluginapi.Store
}

// config is this plugin's own settings; the core reserves none of these names.
type config struct {
	Stores []string `mapstructure:"stores"`
}

// plugin has no Close: the inner instances belong to the plugin manager.
type plugin struct {
	stores []inner
}

// New builds a mirror instance from its config, resolving each entry of
// stores through the given PluginStores.
func New(ctx context.Context, raw pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
	var cfg config
	if err := pluginapi.DecodeConfig(ctx, raw, &cfg); err != nil {
		return nil, err
	}
	names := cfg.Stores
	if len(names) == 0 {
		return nil, errors.New("stores is required")
	}

	p := &plugin{stores: make([]inner, 0, len(names))}
	for _, name := range names {
		store, err := stores.Lookup(name)
		if err != nil {
			return nil, err
		}
		p.stores = append(p.stores, inner{name: name, store: store})
	}
	return p, nil
}

// Get returns the first non-empty value, asking the inner instances in order.
func (p *plugin) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	var errs []error
	type failure struct {
		name string
		err  error
	}
	var failed []failure
	for _, s := range p.stores {
		value, err := s.store.Get(ctx, req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
			failed = append(failed, failure{name: s.name, err: err})
			continue
		}
		if value != "" {
			for _, f := range failed {
				zerolog.Ctx(ctx).Debug().Err(f.err).Str("store", f.name).Msg("mirror: store failed before a later one answered")
			}
			return value, nil
		}
		errs = append(errs, fmt.Errorf("%s: empty value", s.name))
	}
	return "", errors.Join(errs...)
}

// Set writes to every inner instance and fails if any write fails.
func (p *plugin) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	var errs []error
	for _, s := range p.stores {
		if err := s.store.Set(ctx, req); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
		}
	}
	return errors.Join(errs...)
}
