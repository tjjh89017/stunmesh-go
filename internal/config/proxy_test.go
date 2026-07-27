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

func TestLoad_ProxyFib_Present(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    proxy:
      fib: 3
    peers: {}
`)

	if got := cfg.Interfaces["wg0"].Proxy.Fib; got != 3 {
		t.Errorf("Proxy.Fib = %d, want 3", got)
	}

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyFib("wg0"); got != 3 {
		t.Errorf("GetProxyFib(wg0) = %d, want 3", got)
	}
}

// No proxy key (or no fib key) means the escape is off -- the zero-breaking
// default, and also correct since FIB 0 is where the covering WireGuard
// default route already lives.
func TestGetProxyFib_Absent(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyFib("wg0"); got != 0 {
		t.Errorf("GetProxyFib(wg0) = %d, want 0 (escape off)", got)
	}
}

func TestGetProxyFib_UnknownDevice(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyFib("does-not-exist"); got != 0 {
		t.Errorf("GetProxyFib(unknown) = %d, want 0", got)
	}
}

func TestValidateConfig_ProxyFib(t *testing.T) {
	tests := []struct {
		name    string
		fib     int
		wantErr bool
	}{
		{"unset (zero, escape off)", 0, false},
		{"minimum non-zero fib", 1, false},
		{"maximum range", 65535, false},
		{"negative", -1, true},
		{"above range", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Interfaces: Interfaces{
					"wg0": Interface{
						Proxy: Proxy{Fib: tt.fib},
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

func TestLoad_ProxyFib_OutOfRange(t *testing.T) {
	writeWeakTypingConfig(t, `
interfaces:
  wg0:
    proxy:
      fib: 70000
    peers: {}
`)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with proxy.fib 70000 should return an error")
	}
}

func TestProxy_IsEnabled_Absent(t *testing.T) {
	tests := []struct {
		goos string
		want bool
	}{
		{"windows", true},
		{"linux", false},
		{"darwin", false},
		{"freebsd", false},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			p := Proxy{}
			if got := p.IsEnabled(tt.goos); got != tt.want {
				t.Errorf("IsEnabled(%q) = %v, want %v", tt.goos, got, tt.want)
			}
		})
	}
}

func TestProxy_IsEnabled_Explicit(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		enabled *bool
		goos    string
		want    bool
	}{
		{"explicit true on windows", &trueVal, "windows", true},
		{"explicit true on linux", &trueVal, "linux", true},
		{"explicit false on linux", &falseVal, "linux", false},
		{"explicit false on darwin", &falseVal, "darwin", false},
		{"explicit false on windows still reports false (validation catches this)", &falseVal, "windows", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Proxy{Enabled: tt.enabled}
			if got := p.IsEnabled(tt.goos); got != tt.want {
				t.Errorf("IsEnabled(%q) = %v, want %v", tt.goos, got, tt.want)
			}
		})
	}
}

// A lone proxy.listen must not flip proxy mode on.
func TestProxy_IsEnabled_ListenAloneDoesNotEnable(t *testing.T) {
	p := Proxy{Listen: 51999}
	if got := p.IsEnabled("linux"); got != false {
		t.Errorf("IsEnabled(linux) = %v, want false (listen alone must not enable proxy)", got)
	}
}

func TestLoad_ProxyEnabled_Present(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{"true", "true", true},
		{"false", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    proxy:
      enabled: `+tt.yaml+`
    peers: {}
`)

			enabled := cfg.Interfaces["wg0"].Proxy.Enabled
			if enabled == nil {
				t.Fatal("Proxy.Enabled = nil, want non-nil after explicit config")
			}
			if *enabled != tt.want {
				t.Errorf("Proxy.Enabled = %v, want %v", *enabled, tt.want)
			}
		})
	}
}

// Absent proxy.enabled must decode to nil, not false, so IsEnabled can tell
// "unset" from "explicitly disabled".
func TestLoad_ProxyEnabled_Absent(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    proxy:
      listen: 51999
    peers: {}
`)

	if got := cfg.Interfaces["wg0"].Proxy.Enabled; got != nil {
		t.Errorf("Proxy.Enabled = %v, want nil (key absent)", *got)
	}
}

func TestGetProxyEnabled_UnknownDevice(t *testing.T) {
	cfg := loadConfigFromYAML(t, `
interfaces:
  wg0:
    protocol: ipv4
    peers: {}
`)

	dc := NewDeviceConfig(cfg)
	if got := dc.GetProxyEnabled("does-not-exist", "windows"); got != true {
		t.Errorf("GetProxyEnabled(unknown, windows) = %v, want true (platform default)", got)
	}
	if got := dc.GetProxyEnabled("does-not-exist", "linux"); got != false {
		t.Errorf("GetProxyEnabled(unknown, linux) = %v, want false (platform default)", got)
	}
}

func TestValidateConfig_ProxyEnabled_WindowsFalseIsError(t *testing.T) {
	falseVal := false
	trueVal := true

	tests := []struct {
		name    string
		goos    string
		enabled *bool
		wantErr bool
	}{
		{"windows explicit false errors", "windows", &falseVal, true},
		{"windows explicit true ok", "windows", &trueVal, false},
		{"windows absent ok", "windows", nil, false},
		{"linux explicit false ok", "linux", &falseVal, false},
		{"linux explicit true ok", "linux", &trueVal, false},
		{"linux absent ok", "linux", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Interfaces: Interfaces{
					"wg0": Interface{
						Proxy: Proxy{Enabled: tt.enabled},
					},
				},
			}

			err := validateConfigForGOOS(cfg, tt.goos)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigForGOOS(goos=%s) error = %v, wantErr %v", tt.goos, err, tt.wantErr)
			}
		})
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
