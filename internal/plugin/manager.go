package plugin

import (
	"context"
	"fmt"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type Manager struct {
	plugins map[string]pluginapi.Store
}

func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]pluginapi.Store),
	}
}

func (m *Manager) LoadPlugins(ctx context.Context, definitions map[string]pluginapi.PluginDefinition) error {
	for name, def := range definitions {
		store, err := m.createPlugin(ctx, def)
		if err != nil {
			return fmt.Errorf("failed to create plugin %s: %w", name, err)
		}
		m.plugins[name] = store
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
