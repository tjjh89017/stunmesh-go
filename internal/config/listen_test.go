package config

import (
	"os"
	"path/filepath"
	"testing"
)

// loadConfigFromYAML writes content to a temp config.yaml and loads it.
func loadConfigFromYAML(t *testing.T, content string) *Config {
	t.Helper()
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func TestGetListenConfig_Parsed(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: dualstack
    listen_interfaces: ["em0", "em1"]
    listen_default_route: true
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	interfaces, defaultRoute := dc.GetListenConfig("wg0")

	if want := []string{"em0", "em1"}; len(interfaces) != 2 || interfaces[0] != want[0] || interfaces[1] != want[1] {
		t.Errorf("interfaces = %v, want %v", interfaces, want)
	}
	if !defaultRoute {
		t.Error("defaultRoute = false, want true")
	}
}

// TestGetListenConfig_Defaults pins the zero-breaking default: an interface that
// omits both keys reports no restriction, which the STUN layer reads as "listen
// on all".
func TestGetListenConfig_Defaults(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	interfaces, defaultRoute := dc.GetListenConfig("wg0")

	if len(interfaces) != 0 {
		t.Errorf("interfaces = %v, want empty", interfaces)
	}
	if defaultRoute {
		t.Error("defaultRoute = true, want false")
	}
}

// TestGetListenConfig_UnknownDevice must not panic and reports no restriction.
func TestGetListenConfig_UnknownDevice(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	interfaces, defaultRoute := dc.GetListenConfig("does-not-exist")

	if len(interfaces) != 0 || defaultRoute {
		t.Errorf("GetListenConfig(unknown) = (%v, %v), want (empty, false)", interfaces, defaultRoute)
	}
}
