package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type Manager struct {
	plugins map[string]pluginapi.Store
	dedup   map[string]bool
}

func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]pluginapi.Store),
		dedup:   make(map[string]bool),
	}
}

func (m *Manager) LoadPlugins(ctx context.Context, definitions map[string]pluginapi.PluginDefinition) error {
	for name, def := range definitions {
		store, err := m.createPlugin(ctx, def)
		if err != nil {
			return fmt.Errorf("failed to create plugin %s: %w", name, err)
		}
		m.plugins[name] = store
		m.dedup[name] = parseDedup(def.Config["dedup"])
	}
	return nil
}

func (m *Manager) GetPlugin(name string) (pluginapi.Store, error) {
	store, ok := m.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", name)
	}
	return store, nil
}

// Close closes every plugin instance that implements io.Closer, in sorted
// name order, and joins any errors. It is idempotent: after closing, the
// plugin set is emptied, so a second call is a no-op returning nil.
func (m *Manager) Close() error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(m.plugins)) {
		closer, ok := m.plugins[name].(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", name, err))
		}
	}
	clear(m.plugins)
	return errors.Join(errs...)
}

// IsDedup reports whether the named plugin instance has dedup enabled.
// Unknown plugin names return false.
func (m *Manager) IsDedup(name string) bool {
	return m.dedup[name]
}

// parseDedup coerces a raw config value into a dedup flag. It accepts a
// real bool, or a string such as "true"/"1" (e.g. from an environment
// override). Anything else, including an absent value (nil), is false.
func parseDedup(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

func (m *Manager) createPlugin(ctx context.Context, def pluginapi.PluginDefinition) (pluginapi.Store, error) {
	switch def.Type {
	case "exec":
		return NewExecPlugin(def.Config)
	case "shell":
		return NewShellPlugin(def.Config)
	case "builtin":
		return NewBuiltinPlugin(def.Config)
	default:
		return nil, fmt.Errorf("unsupported plugin type: %s", def.Type)
	}
}
