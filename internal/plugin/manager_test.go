package plugin

import (
	"context"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func TestNewManager(t *testing.T) {
	m := NewManager()

	if m == nil {
		t.Fatal("NewManager() returned nil")
	}

	if m.plugins == nil {
		t.Error("Manager.plugins map is nil")
	}

	if len(m.plugins) != 0 {
		t.Errorf("NewManager() should create empty plugins map, got %d plugins", len(m.plugins))
	}
}

func TestGetPlugin_NotFound(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	// Try to get a plugin that doesn't exist
	_, err := m.GetPlugin("nonexistent")
	if err == nil {
		t.Error("GetPlugin() should return error for nonexistent plugin")
	}

	expectedMsg := "plugin nonexistent not found"
	if err.Error() != expectedMsg {
		t.Errorf("GetPlugin() error message = %q, want %q", err.Error(), expectedMsg)
	}

	// Verify context is available (not used in GetPlugin but passed around)
	_ = ctx
}

func TestLoadPlugins_EmptyDefinitions(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	definitions := map[string]pluginapi.PluginDefinition{}

	err := m.LoadPlugins(ctx, definitions)
	if err != nil {
		t.Errorf("LoadPlugins() with empty definitions should not error, got: %v", err)
	}

	if len(m.plugins) != 0 {
		t.Errorf("LoadPlugins() with empty definitions should result in 0 plugins, got %d", len(m.plugins))
	}
}

func TestLoadPlugins_UnsupportedType(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	definitions := map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type: "unsupported_type",
			Config: pluginapi.PluginConfig{
				"key": "value",
			},
		},
	}

	err := m.LoadPlugins(ctx, definitions)
	if err == nil {
		t.Error("LoadPlugins() should return error for unsupported plugin type")
	}

	if len(m.plugins) != 0 {
		t.Errorf("LoadPlugins() failed but plugins map is not empty, got %d plugins", len(m.plugins))
	}
}

func TestLoadPlugins_InvalidPluginConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		pluginName  string
		definition  pluginapi.PluginDefinition
		wantErrText string
	}{
		{
			name:       "builtin without name",
			pluginName: "test_builtin",
			definition: pluginapi.PluginDefinition{
				Type: "builtin",
				Config: pluginapi.PluginConfig{
					// Missing "name" field
					"token": "test",
				},
			},
			wantErrText: "failed to create plugin test_builtin",
		},
		{
			name:       "exec without command",
			pluginName: "test_exec",
			definition: pluginapi.PluginDefinition{
				Type: "exec",
				Config: pluginapi.PluginConfig{
					// Missing "command" field
					"args": []string{},
				},
			},
			wantErrText: "failed to create plugin test_exec",
		},
		{
			name:       "shell without command",
			pluginName: "test_shell",
			definition: pluginapi.PluginDefinition{
				Type: "shell",
				Config: pluginapi.PluginConfig{
					// Missing "command" field
					"env": map[string]string{},
				},
			},
			wantErrText: "failed to create plugin test_shell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager() // Fresh manager for each test
			definitions := map[string]pluginapi.PluginDefinition{
				tt.pluginName: tt.definition,
			}

			err := m.LoadPlugins(ctx, definitions)
			if err == nil {
				t.Errorf("LoadPlugins() should return error for invalid %s config", tt.definition.Type)
			}

			// Check error message contains expected text
			if err != nil && tt.wantErrText != "" {
				errMsg := err.Error()
				if len(errMsg) < len(tt.wantErrText) || errMsg[:len(tt.wantErrText)] != tt.wantErrText {
					t.Errorf("LoadPlugins() error = %q, want error containing %q", errMsg, tt.wantErrText)
				}
			}
		})
	}
}

func TestCreatePlugin_SwitchCoverage(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	tests := []struct {
		name       string
		pluginType string
		wantErr    bool
	}{
		{
			name:       "exec type",
			pluginType: "exec",
			wantErr:    true, // Will fail due to missing config, but tests the switch case
		},
		{
			name:       "shell type",
			pluginType: "shell",
			wantErr:    true, // Will fail due to missing config, but tests the switch case
		},
		{
			name:       "builtin type",
			pluginType: "builtin",
			wantErr:    true, // Will fail due to missing config, but tests the switch case
		},
		{
			name:       "unknown type",
			pluginType: "unknown",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := pluginapi.PluginDefinition{
				Type:   tt.pluginType,
				Config: pluginapi.PluginConfig{},
			}

			_, err := m.createPlugin(ctx, def)
			if (err != nil) != tt.wantErr {
				t.Errorf("createPlugin() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
