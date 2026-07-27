//go:build windows || wgproxy

package main

import (
	"runtime"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

func TestProxyModeEnabledForGOOS(t *testing.T) {
	enabled := true
	disabled := false

	tests := map[string]struct {
		interfaces config.Interfaces
		goos       string
		want       bool
	}{
		"no interfaces, windows defaults on": {
			interfaces: config.Interfaces{"wg0": {}},
			goos:       "windows",
			want:       true,
		},
		"no interfaces, linux defaults off": {
			interfaces: config.Interfaces{"wg0": {}},
			goos:       "linux",
			want:       false,
		},
		"lone proxy.listen does not enable proxy mode on linux": {
			interfaces: config.Interfaces{"wg0": {Proxy: config.Proxy{Listen: 51999}}},
			goos:       "linux",
			want:       false,
		},
		"explicit enabled true on linux turns proxy mode on": {
			interfaces: config.Interfaces{"wg0": {Proxy: config.Proxy{Enabled: &enabled}}},
			goos:       "linux",
			want:       true,
		},
		"explicit enabled false on windows would be a config validation error, not exercised here": {
			interfaces: config.Interfaces{"wg0": {Proxy: config.Proxy{Enabled: &disabled}}},
			goos:       "darwin",
			want:       false,
		},
		"one of several interfaces enabled is enough": {
			interfaces: config.Interfaces{
				"wg0": {},
				"wg1": {Proxy: config.Proxy{Enabled: &enabled}},
			},
			goos: "freebsd",
			want: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			logger := zerolog.Nop()
			cfg := &config.Config{Interfaces: tt.interfaces}
			deviceConfig := config.NewDeviceConfig(cfg)

			got := proxyModeEnabledForGOOS(cfg, deviceConfig, tt.goos, &logger)
			if got != tt.want {
				t.Fatalf("proxyModeEnabledForGOOS = %v, want %v", got, tt.want)
			}
		})
	}
}

// proxyModeEnabled delegates to proxyModeEnabledForGOOS with runtime.GOOS.
func TestProxyModeEnabled_NoListenKey(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {},
	}}
	deviceConfig := config.NewDeviceConfig(cfg)

	got := proxyModeEnabled(cfg, deviceConfig, &logger)
	want := runtime.GOOS == "windows"
	if got != want {
		t.Fatalf("proxyModeEnabled = %v, want %v on %s", got, want, runtime.GOOS)
	}
}
