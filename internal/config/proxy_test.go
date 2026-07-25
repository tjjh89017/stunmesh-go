package config

import (
	"errors"
	"testing"
)

func TestLoad_ProxyListen_Present(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    proxy:
      listen: 51999
    peers: {}
`)

	if got := cfg.Interfaces["wg0"].Proxy.Listen; got != 51999 {
		t.Errorf("Proxy.Listen = %d, want 51999", got)
	}

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyListenPort("wg0"); got != 51999 {
		t.Errorf("GetProxyListenPort(wg0) = %d, want 51999", got)
	}
}

// No proxy key means ephemeral (0) — the zero-breaking default.
func TestGetProxyListenPort_Absent(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyListenPort("wg0"); got != 0 {
		t.Errorf("GetProxyListenPort(wg0) = %d, want 0 (ephemeral)", got)
	}
}

func TestGetProxyListenPort_UnknownDevice(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyListenPort("does-not-exist"); got != 0 {
		t.Errorf("GetProxyListenPort(unknown) = %d, want 0", got)
	}
}

func TestValidateConfig_ProxyListen(t *testing.T) {
	tests := []struct {
		name    string
		listen  int
		wantErr bool
	}{
		{"unset (zero)", 0, false},
		{"minimum port", 1, false},
		{"maximum port", 65535, false},
		{"negative", -1, true},
		{"above range", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Interfaces: Interfaces{
					"wg0": Interface{
						Proxy: Proxy{Listen: tt.listen},
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

func TestLoad_ProxyListen_OutOfRange(t *testing.T) {
	writeWeakTypingConfig(t, `
interfaces:
  wg0:
    proxy:
      listen: 70000
    peers: {}
`)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with proxy.listen 70000 should return an error")
	}
}

func TestLoad_ProxyListen_NonInteger(t *testing.T) {
	writeWeakTypingConfig(t, `
interfaces:
  wg0:
    proxy:
      listen: not-a-port
    peers: {}
`)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with non-integer proxy.listen should return an error")
	}
	if !errors.Is(err, ErrUnmarshalConfig) {
		t.Errorf("Load() error = %v, want wrapped ErrUnmarshalConfig", err)
	}
}

// Quoted scalar keeps loading, matching WeaklyTypedInput on other numeric fields.
func TestLoad_ProxyListen_QuotedScalar(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    proxy:
      listen: "51999"
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyListenPort("wg0"); got != 51999 {
		t.Errorf("GetProxyListenPort(wg0) = %d, want 51999", got)
	}
}

// --- testdata fixtures ---

func TestLoad_Testdata_ProxyListen(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = "testdata/valid_config.yaml"

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyListenPort("wg0"); got != 51999 {
		t.Errorf("GetProxyListenPort(wg0) = %d, want 51999", got)
	}
	if got := dc.GetProxyListenPort("wg1"); got != 0 {
		t.Errorf("GetProxyListenPort(wg1) = %d, want 0 (key absent)", got)
	}
}

func TestLoad_Testdata_InvalidProxyListen(t *testing.T) {
	resetConfigGlobals(t)
	ConfigFile = "testdata/invalid_proxy_listen.yaml"

	if _, err := Load(); err == nil {
		t.Fatal("Load() with invalid proxy.listen fixture should return an error")
	}
}
