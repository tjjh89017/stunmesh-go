package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// resetViper resets viper state between tests
func resetViper() {
	viper.Reset()
}

func TestLoad_Success(t *testing.T) {
	resetViper()

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

	// Override config paths to use temp directory
	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
	resetViper()

	// Use a directory that doesn't contain config file
	tmpDir := t.TempDir()

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	_, err := Load()
	if err == nil {
		t.Error("Load() with invalid YAML should return error")
	}
}

func TestLoad_InvalidProtocol(t *testing.T) {
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	_, err := Load()
	if err == nil {
		t.Error("Load() with invalid peer protocol should return error")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
}

func TestLoad_InterfaceConfig(t *testing.T) {
	resetViper()

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

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

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
