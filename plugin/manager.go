package plugin

import (
	"context"
	"fmt"
)

type PluginType string

const (
	PluginTypeExec PluginType = "exec"
)

type PluginConfig map[string]interface{}

type PluginDefinition struct {
	Type   string       `mapstructure:"type"`
	Config PluginConfig `mapstructure:",remain"`
}

type Manager struct {
	plugins map[string]Store
}

func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]Store),
	}
}

func (m *Manager) LoadPlugins(ctx context.Context, definitions map[string]PluginDefinition) error {
	for name, def := range definitions {
		store, err := m.createPlugin(ctx, def)
		if err != nil {
			return fmt.Errorf("failed to create plugin %s: %w", name, err)
		}
		m.plugins[name] = store
	}
	return nil
}

func (m *Manager) GetPlugin(name string) (Store, error) {
	store, ok := m.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", name)
	}
	return store, nil
}

func (m *Manager) createPlugin(ctx context.Context, def PluginDefinition) (Store, error) {
	switch PluginType(def.Type) {
	case PluginTypeExec:
		return NewExecPlugin(def.Config)
	default:
		return nil, fmt.Errorf("unsupported plugin type: %s", def.Type)
	}
}
