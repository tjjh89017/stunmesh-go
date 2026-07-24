package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// resetConfigGlobals saves ConfigFile/ConfigDir/Paths and restores them via t.Cleanup, then
// clears ConfigFile/ConfigDir. These are shared globals: tests using it must not t.Parallel().
func resetConfigGlobals(t *testing.T) {
	t.Helper()
	origFile, origDir, origPaths := ConfigFile, ConfigDir, Paths
	t.Cleanup(func() {
		ConfigFile, ConfigDir, Paths = origFile, origDir, origPaths
	})
	ConfigFile = ""
	ConfigDir = ""
}

func TestLoad_Success(t *testing.T) {
	resetConfigGlobals(t)

	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
interfaces:
  wg0:
    protocol: ipv4
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin
        protocol: ipv4

plugins:
  test_plugin:
    type: builtin
    name: test

refresh_interval: 5m
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
		return
	}

	// Verify basic config values
	if cfg.RefreshInterval != 5*time.Minute {
		t.Errorf("RefreshInterval = %v, want 5m", cfg.RefreshInterval)
	}

	if len(cfg.Interfaces) != 1 {
		t.Errorf("len(Interfaces) = %d, want 1", len(cfg.Interfaces))
	}

	if len(cfg.Plugins) != 1 {
		t.Errorf("len(Plugins) = %d, want 1", len(cfg.Plugins))
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	resetConfigGlobals(t)

	// Use a directory that doesn't contain config file
	tmpDir := t.TempDir()
	Paths = []string{tmpDir}

	// Should still succeed but return config with defaults
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (file not found should not error)", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
		return
	}

	// Should have default values
	if cfg.RefreshInterval != 10*time.Minute {
		t.Errorf("RefreshInterval = %v, want 10m (default)", cfg.RefreshInterval)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `
interfaces:
  wg0:
    - this is invalid yaml
    peers: [1, 2, 3
`

	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with invalid YAML should return error")
	}

	// Malformed YAML fails at yaml.Unmarshal, wrapped in ErrReadConfig
	// (ErrUnmarshalConfig is reserved for mapstructure decode failures).
	if !errors.Is(err, ErrReadConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrReadConfig", err)
	}
}

func TestLoad_InvalidProtocol(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
interfaces:
  wg0:
    protocol: invalid_protocol
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if err == nil {
		t.Error("Load() with invalid protocol should return error")
	}

	// Check error message contains "invalid interface protocol"
	if err != nil && err.Error() == "" {
		t.Error("Load() error message should not be empty")
	}
}

func TestLoad_InvalidPeerProtocol(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
interfaces:
  wg0:
    protocol: ipv4
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin
        protocol: invalid_peer_protocol
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if err == nil {
		t.Error("Load() with invalid peer protocol should return error")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Minimal config without optional fields
	configContent := `
interfaces:
  wg0:
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Check default values
	if cfg.RefreshInterval != 10*time.Minute {
		t.Errorf("RefreshInterval = %v, want 10m (default)", cfg.RefreshInterval)
	}

	if cfg.PingMonitor.Interval != 1*time.Second {
		t.Errorf("PingMonitor.Interval = %v, want 1s (default)", cfg.PingMonitor.Interval)
	}

	if cfg.PingMonitor.Timeout != 1*time.Second {
		t.Errorf("PingMonitor.Timeout = %v, want 1s (default)", cfg.PingMonitor.Timeout)
	}

	if cfg.PingMonitor.FixedRetries != 3 {
		t.Errorf("PingMonitor.FixedRetries = %d, want 3 (default)", cfg.PingMonitor.FixedRetries)
	}
}

func TestLoad_PluginDefinitions(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
interfaces:
  wg0:
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin

plugins:
  test_plugin:
    type: exec
    command: /bin/test
    args: ["-v"]
  another_plugin:
    type: shell
    command: /bin/sh
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if len(cfg.Plugins) != 2 {
		t.Errorf("len(Plugins) = %d, want 2", len(cfg.Plugins))
	}

	testPlugin, ok := cfg.Plugins["test_plugin"]
	if !ok {
		t.Fatal("test_plugin not found in config")
	}

	if testPlugin.Type != "exec" {
		t.Errorf("test_plugin.Type = %q, want %q", testPlugin.Type, "exec")
	}

	if testPlugin.Config["command"] != "/bin/test" {
		t.Errorf("test_plugin command = %v, want /bin/test", testPlugin.Config["command"])
	}

	args, ok := testPlugin.Config["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "-v" {
		t.Errorf("test_plugin args = %v, want [-v]", testPlugin.Config["args"])
	}

	anotherPlugin, ok := cfg.Plugins["another_plugin"]
	if !ok {
		t.Fatal("another_plugin not found in config")
	}

	if anotherPlugin.Type != "shell" {
		t.Errorf("another_plugin.Type = %q, want %q", anotherPlugin.Type, "shell")
	}

	if anotherPlugin.Config["command"] != "/bin/sh" {
		t.Errorf("another_plugin command = %v, want /bin/sh", anotherPlugin.Config["command"])
	}
}

func TestLoad_InterfaceConfig(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
interfaces:
  wg0:
    protocol: ipv4
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: plugin1
        protocol: prefer_ipv4
      peer2:
        public_key: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="
        plugin: plugin2
        protocol: ipv6
  wg1:
    protocol: dualstack
    peers:
      peer3:
        public_key: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC="
        plugin: plugin3
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if len(cfg.Interfaces) != 2 {
		t.Errorf("len(Interfaces) = %d, want 2", len(cfg.Interfaces))
	}

	// Check wg0
	wg0, ok := cfg.Interfaces["wg0"]
	if !ok {
		t.Fatal("wg0 not found in interfaces")
	}

	if wg0.Protocol != "ipv4" {
		t.Errorf("wg0.Protocol = %q, want %q", wg0.Protocol, "ipv4")
	}

	if len(wg0.Peers) != 2 {
		t.Errorf("len(wg0.Peers) = %d, want 2", len(wg0.Peers))
	}

	// Check peer protocols
	peer1 := wg0.Peers["peer1"]
	if peer1.Protocol != "prefer_ipv4" {
		t.Errorf("peer1.Protocol = %q, want %q", peer1.Protocol, "prefer_ipv4")
	}

	peer2 := wg0.Peers["peer2"]
	if peer2.Protocol != "ipv6" {
		t.Errorf("peer2.Protocol = %q, want %q", peer2.Protocol, "ipv6")
	}

	// Check wg1
	wg1, ok := cfg.Interfaces["wg1"]
	if !ok {
		t.Fatal("wg1 not found in interfaces")
	}

	if wg1.Protocol != "dualstack" {
		t.Errorf("wg1.Protocol = %q, want %q", wg1.Protocol, "dualstack")
	}
}

func TestValidateConfig_ValidProtocols(t *testing.T) {
	tests := []struct {
		name           string
		interfaceProto string
		peerProto      string
		wantErr        bool
	}{
		{"ipv4 interface, ipv4 peer", "ipv4", "ipv4", false},
		{"ipv6 interface, ipv6 peer", "ipv6", "ipv6", false},
		{"dualstack interface, prefer_ipv4 peer", "dualstack", "prefer_ipv4", false},
		{"dualstack interface, prefer_ipv6 peer", "dualstack", "prefer_ipv6", false},
		{"invalid interface protocol", "invalid", "ipv4", true},
		{"invalid peer protocol", "ipv4", "invalid", true},
		{"empty protocols (should use defaults)", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Interfaces: Interfaces{
					"wg0": Interface{
						Protocol: tt.interfaceProto,
						Peers: map[string]Peer{
							"peer1": {
								PublicKey: "test",
								Plugin:    "test",
								Protocol:  tt.peerProto,
							},
						},
					},
				},
			}

			err := validateConfig(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- ConfigFile / ConfigDir / Paths resolution ---

func TestLoad_File_Exists(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "custom-name.yaml")
	if err := os.WriteFile(path, []byte("refresh_interval: 11m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// ConfigFile must win even though Paths would never find this file.
	Paths = []string{t.TempDir()}
	ConfigFile = path

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 11*time.Minute {
		t.Errorf("RefreshInterval = %v, want 11m", cfg.RefreshInterval)
	}
}

func TestLoad_File_NotExists_ErrorsWithoutFallback(t *testing.T) {
	resetConfigGlobals(t)

	ConfigFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with nonexistent ConfigFile should return an error, not fall back to defaults")
	}

	if !errors.Is(err, ErrReadConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrReadConfig", err)
	}
}

func TestLoad_Dir_WithConfig(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("refresh_interval: 13m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ConfigDir = tmpDir

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 13*time.Minute {
		t.Errorf("RefreshInterval = %v, want 13m", cfg.RefreshInterval)
	}
}

func TestLoad_Dir_WithoutConfig_ErrorsWithoutFallback(t *testing.T) {
	resetConfigGlobals(t)

	ConfigDir = t.TempDir()

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with ConfigDir lacking config.yaml should return an error, not fall back to defaults")
	}

	if !errors.Is(err, ErrReadConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrReadConfig", err)
	}
}

func TestLoad_File_TakesPriorityOverDir(t *testing.T) {
	resetConfigGlobals(t)

	fileDir := t.TempDir()
	filePath := filepath.Join(fileDir, "explicit-file.yaml")
	if err := os.WriteFile(filePath, []byte("refresh_interval: 21m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dirWithConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirWithConfig, "config.yaml"), []byte("refresh_interval: 22m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ConfigFile = filePath
	ConfigDir = dirWithConfig

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 21*time.Minute {
		t.Errorf("RefreshInterval = %v, want 21m (ConfigFile must win over ConfigDir)", cfg.RefreshInterval)
	}
}

func TestLoad_Paths_FindsConfigYaml(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("refresh_interval: 9m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 9*time.Minute {
		t.Errorf("RefreshInterval = %v, want 9m", cfg.RefreshInterval)
	}
}

func TestLoad_Paths_FindsConfigYml(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	// Only the .yml variant is present.
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("refresh_interval: 14m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 14*time.Minute {
		t.Errorf("RefreshInterval = %v, want 14m", cfg.RefreshInterval)
	}
}

func TestLoad_Paths_SearchesInOrder(t *testing.T) {
	resetConfigGlobals(t)

	emptyDir := t.TempDir()
	configuredDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configuredDir, "config.yaml"), []byte("refresh_interval: 17m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Search must fall through the empty first entry, not stop or error.
	Paths = []string{emptyDir, configuredDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 17*time.Minute {
		t.Errorf("RefreshInterval = %v, want 17m", cfg.RefreshInterval)
	}
}

func TestLoad_Paths_EnvVarExpansion(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("refresh_interval: 7m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("STUNMESH_CONFIG_DIR", tmpDir)
	Paths = []string{"$STUNMESH_CONFIG_DIR"}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 7*time.Minute {
		t.Errorf("RefreshInterval = %v, want 7m", cfg.RefreshInterval)
	}
}

func TestLoad_Paths_EmptyExpansionIsSkipped(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("refresh_interval: 8m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The unset env var expands to ""; findConfigFile must skip that entry
	// (not treat it as the current directory) and continue.
	Paths = []string{"$STUNMESH_CONFIG_DIR_UNSET_FOR_TEST", tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 8*time.Minute {
		t.Errorf("RefreshInterval = %v, want 8m", cfg.RefreshInterval)
	}
}

func TestLoad_NoConfigFound_ReturnsAllDefaults(t *testing.T) {
	resetConfigGlobals(t)

	Paths = []string{t.TempDir()}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (no config file must not error)", err)
	}

	if cfg.RefreshInterval != 10*time.Minute {
		t.Errorf("RefreshInterval = %v, want 10m (default)", cfg.RefreshInterval)
	}
	if cfg.PingMonitor.Interval != 1*time.Second {
		t.Errorf("PingMonitor.Interval = %v, want 1s (default)", cfg.PingMonitor.Interval)
	}
	if cfg.PingMonitor.Timeout != 1*time.Second {
		t.Errorf("PingMonitor.Timeout = %v, want 1s (default)", cfg.PingMonitor.Timeout)
	}
	if cfg.PingMonitor.FixedRetries != 3 {
		t.Errorf("PingMonitor.FixedRetries = %d, want 3 (default)", cfg.PingMonitor.FixedRetries)
	}
	if got := cfg.Stun.GetServers(); len(got) != 1 || got[0] != "stun.l.google.com:19302" {
		t.Errorf("Stun.GetServers() = %v, want [stun.l.google.com:19302] (fallback)", got)
	}
	if len(cfg.Interfaces) != 0 {
		t.Errorf("len(Interfaces) = %d, want 0", len(cfg.Interfaces))
	}
	if len(cfg.Plugins) != 0 {
		t.Errorf("len(Plugins) = %d, want 0", len(cfg.Plugins))
	}
}

// TestLoad_PartialYAML_UnsetFieldsKeepDefaults verifies that yaml keys absent
// from the file leave pre-Decode defaults untouched rather than zeroing them.
func TestLoad_PartialYAML_UnsetFieldsKeepDefaults(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("refresh_interval: 42m\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.RefreshInterval != 42*time.Minute {
		t.Errorf("RefreshInterval = %v, want 42m", cfg.RefreshInterval)
	}
	if cfg.PingMonitor.Interval != 1*time.Second {
		t.Errorf("PingMonitor.Interval = %v, want 1s (default, untouched by yaml)", cfg.PingMonitor.Interval)
	}
	if cfg.PingMonitor.Timeout != 1*time.Second {
		t.Errorf("PingMonitor.Timeout = %v, want 1s (default, untouched by yaml)", cfg.PingMonitor.Timeout)
	}
	if cfg.PingMonitor.FixedRetries != 3 {
		t.Errorf("PingMonitor.FixedRetries = %d, want 3 (default, untouched by yaml)", cfg.PingMonitor.FixedRetries)
	}
}

func TestLoad_DurationStringParsing(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := `
ping_monitor:
  interval: 5m
  timeout: 30s
  fixed_retries: 7
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.PingMonitor.Interval != 5*time.Minute {
		t.Errorf("PingMonitor.Interval = %v, want 5m", cfg.PingMonitor.Interval)
	}
	if cfg.PingMonitor.Timeout != 30*time.Second {
		t.Errorf("PingMonitor.Timeout = %v, want 30s", cfg.PingMonitor.Timeout)
	}
	if cfg.PingMonitor.FixedRetries != 7 {
		t.Errorf("PingMonitor.FixedRetries = %d, want 7", cfg.PingMonitor.FixedRetries)
	}
}

// TestLoad_PluginDefinition_RemainCapturesExtraKeys pins mapstructure `,remain`:
// keys other than "type" fall through into Config instead of being dropped.
func TestLoad_PluginDefinition_RemainCapturesExtraKeys(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := `
plugins:
  cloudflare_builtin:
    type: builtin
    name: cloudflare
    zone: example.com
    token: secret-token
    subdomain: stunmesh
    dedup: true
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	plugin, ok := cfg.Plugins["cloudflare_builtin"]
	if !ok {
		t.Fatal("cloudflare_builtin not found in config")
	}

	if plugin.Type != "builtin" {
		t.Errorf("plugin.Type = %q, want %q", plugin.Type, "builtin")
	}

	// "type" itself must NOT leak into the remainder map.
	if _, present := plugin.Config["type"]; present {
		t.Error(`plugin.Config contains "type", want it consumed by the Type field`)
	}

	wantRemain := map[string]interface{}{
		"name":      "cloudflare",
		"zone":      "example.com",
		"token":     "secret-token",
		"subdomain": "stunmesh",
		"dedup":     true,
	}
	for k, want := range wantRemain {
		got, ok := plugin.Config[k]
		if !ok {
			t.Errorf("plugin.Config[%q] missing, want %v", k, want)
			continue
		}
		if got != want {
			t.Errorf("plugin.Config[%q] = %v, want %v", k, got, want)
		}
	}
}

func TestLoad_MalformedYAML_WrapsErrReadConfig(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	// Unterminated flow sequence -- not valid YAML.
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("plugins: [1, 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with malformed YAML should return an error")
	}
	if !errors.Is(err, ErrReadConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrReadConfig", err)
	}
}

// TestLoad_TypeMismatch_WrapsErrUnmarshalConfig: valid YAML that fails
// mapstructure decoding (a map where a time.Duration is expected).
func TestLoad_TypeMismatch_WrapsErrUnmarshalConfig(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "refresh_interval:\n  nested: not-a-duration\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with a type-mismatched field should return an error")
	}
	if !errors.Is(err, ErrUnmarshalConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrUnmarshalConfig", err)
	}
}

// --- WeaklyTypedInput regression tests ---
//
// main used viper 1.21, whose default decoder sets WeaklyTypedInput: true and
// composes StringToTimeDurationHookFunc with StringToSliceHookFunc(","). Configs
// rendered by templating tools (Ansible/Jinja) commonly quote scalars, so these
// shapes must keep loading after the viper -> yaml+mapstructure migration.

// writeWeakTypingConfig writes configContent into a temp dir and points Paths at it.
func writeWeakTypingConfig(t *testing.T, configContent string) {
	t.Helper()
	resetConfigGlobals(t)
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	Paths = []string{tmpDir}
}

// Quoted numeric scalar decodes into an int field.
func TestLoad_WeaklyTypedInput_QuotedIntScalar(t *testing.T) {
	writeWeakTypingConfig(t, "ping_monitor:\n  fixed_retries: \"7\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.PingMonitor.FixedRetries != 7 {
		t.Errorf("PingMonitor.FixedRetries = %d, want 7", cfg.PingMonitor.FixedRetries)
	}
}

// A plain string for a list field decodes into a single-element list.
func TestLoad_WeaklyTypedInput_StringToSingleElementList(t *testing.T) {
	writeWeakTypingConfig(t, "stun:\n  addresses: \"stun.example.com:3478\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []string{"stun.example.com:3478"}
	if len(cfg.Stun.Addresses) != 1 || cfg.Stun.Addresses[0] != want[0] {
		t.Errorf("Stun.Addresses = %v, want %v", cfg.Stun.Addresses, want)
	}
}

// A comma-separated string for a list field splits into multiple elements.
func TestLoad_WeaklyTypedInput_CommaSeparatedStringToList(t *testing.T) {
	writeWeakTypingConfig(t, "stun:\n  addresses: \"stun1.example.com:3478,stun2.example.com:3478\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if len(cfg.Stun.Addresses) != len(want) {
		t.Fatalf("Stun.Addresses = %v, want %v", cfg.Stun.Addresses, want)
	}
	for i := range want {
		if cfg.Stun.Addresses[i] != want[i] {
			t.Errorf("Stun.Addresses[%d] = %q, want %q", i, cfg.Stun.Addresses[i], want[i])
		}
	}
}

// Quoted boolean scalar decodes into a bool field.
func TestLoad_WeaklyTypedInput_QuotedBool(t *testing.T) {
	writeWeakTypingConfig(t, `
interfaces:
  wg0:
    peers:
      peer1:
        public_key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
        plugin: test_plugin
        ping:
          enabled: "true"
          target: "192.0.2.1"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	peer := cfg.Interfaces["wg0"].Peers["peer1"]
	if peer.Ping == nil {
		t.Fatal("peer1.Ping = nil, want non-nil")
	}
	if !peer.Ping.Enabled {
		t.Error("peer1.Ping.Enabled = false, want true")
	}
}

// Numeric scalar decodes into a string field.
func TestLoad_WeaklyTypedInput_NumberToString(t *testing.T) {
	writeWeakTypingConfig(t, "log:\n  level: 5\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.Log.Level != "5" {
		t.Errorf("Log.Level = %q, want \"5\"", cfg.Log.Level)
	}
}

// An empty string for stun.addresses goes through StringToSliceHookFunc and
// becomes a non-nil empty list, which — with no stun.address — must hit the
// "explicitly provided but unusable" error branch, never the silent default.
func TestLoad_WeaklyTypedInput_EmptyStringAddresses(t *testing.T) {
	writeWeakTypingConfig(t, "stun:\n  addresses: \"\"\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with addresses: \"\" should return an error")
	}
	if !errors.Is(err, ErrNoStunServers) {
		t.Errorf("Load() error = %v, want ErrNoStunServers", err)
	}
}

func writeLogConfig(t *testing.T, logSection string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := logSection + `
stun:
  addresses:
    - stun.example.com:19302
interfaces:
  wg0:
    peers: {}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

func TestLoad_LogDefaults(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = writeLogConfig(t, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Format != DefaultLogFormat {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, DefaultLogFormat)
	}
	if cfg.Log.Level != DefaultLogLevel {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, DefaultLogLevel)
	}
}

func TestLoad_LogFromFile(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = writeLogConfig(t, "log:\n  format: json\n  level: debug\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Format != LogFormatJSON {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, LogFormatJSON)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
}

// An explicit empty value means unset, not invalid.
func TestLoad_EmptyLogValuesFallBackToDefaults(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = writeLogConfig(t, "log:\n  format: \"\"\n  level: \"\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Format != DefaultLogFormat {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, DefaultLogFormat)
	}
	if cfg.Log.Level != DefaultLogLevel {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, DefaultLogLevel)
	}
}

func TestLoad_InvalidLogFormatRejected(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = writeLogConfig(t, "log:\n  format: yaml\n")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an invalid log format error")
	}
}

func TestLoad_LogLevels(t *testing.T) {
	// Every level zerolog names must load, in any case; nothing else may.
	for _, level := range LogLevels {
		for _, spelling := range []string{level, strings.ToUpper(level)} {
			t.Run(spelling, func(t *testing.T) {
				resetConfigGlobals(t)
				ConfigFile = writeLogConfig(t, "log:\n  level: "+spelling+"\n")

				if _, err := Load(); err != nil {
					t.Errorf("Load() error = %v, want nil", err)
				}
			})
		}
	}

	for _, level := range []string{"banana", "verbose"} {
		t.Run("invalid/"+level, func(t *testing.T) {
			resetConfigGlobals(t)
			ConfigFile = writeLogConfig(t, "log:\n  level: "+level+"\n")

			if _, err := Load(); err == nil {
				t.Errorf("Load() error = nil, want an invalid log level error")
			}
		})
	}
}

// LogLevels is derived from zerolog rather than hand-written, so pin the set
// and its severity order.
func TestLogLevels_FromZerolog(t *testing.T) {
	want := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"}
	if !slices.Equal(LogLevels, want) {
		t.Errorf("LogLevels = %v, want %v", LogLevels, want)
	}
}
