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
		return
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

// TestLoadPlugins_InvalidPluginConfig exercises every branch of
// createPlugin's type switch (including the default/unsupported-type
// branch) through the public LoadPlugins entry point, so it doubles as
// coverage for createPlugin without calling the unexported method directly.
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
		{
			name:       "unsupported type",
			pluginName: "test_plugin",
			definition: pluginapi.PluginDefinition{
				Type: "unsupported_type",
				Config: pluginapi.PluginConfig{
					"key": "value",
				},
			},
			wantErrText: "failed to create plugin test_plugin",
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

			if len(m.plugins) != 0 {
				t.Errorf("LoadPlugins() failed but plugins map is not empty, got %d plugins", len(m.plugins))
			}
		})
	}
}

func TestIsDedup_DefaultsToFalseWhenNotSet(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	definitions := map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type: "shell",
			Config: pluginapi.PluginConfig{
				"command": "/bin/true",
			},
		},
	}

	if err := m.LoadPlugins(ctx, definitions); err != nil {
		t.Fatalf("LoadPlugins() unexpected error: %v", err)
	}

	if got := m.IsDedup("test_plugin"); got != false {
		t.Errorf("IsDedup() = %v, want false when dedup is not set", got)
	}
}

func TestIsDedup_TrueWhenConfigured(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	definitions := map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type: "shell",
			Config: pluginapi.PluginConfig{
				"command": "/bin/true",
				"dedup":   true,
			},
		},
	}

	if err := m.LoadPlugins(ctx, definitions); err != nil {
		t.Fatalf("LoadPlugins() unexpected error: %v", err)
	}

	if got := m.IsDedup("test_plugin"); got != true {
		t.Errorf("IsDedup() = %v, want true when dedup: true is configured", got)
	}
}

func TestIsDedup_StringCoercion(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	definitions := map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type: "shell",
			Config: pluginapi.PluginConfig{
				"command": "/bin/true",
				"dedup":   "true",
			},
		},
	}

	if err := m.LoadPlugins(ctx, definitions); err != nil {
		t.Fatalf("LoadPlugins() unexpected error: %v", err)
	}

	if got := m.IsDedup("test_plugin"); got != true {
		t.Errorf("IsDedup() = %v, want true when dedup: \"true\" (string) is configured", got)
	}
}

func TestIsDedup_UnknownPluginReturnsFalse(t *testing.T) {
	m := NewManager()

	if got := m.IsDedup("nonexistent"); got != false {
		t.Errorf("IsDedup() = %v, want false for unknown plugin name", got)
	}
}
