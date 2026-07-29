package main

import (
	"context"
	"runtime"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
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

// stubFactory records which devices it was invoked for and returns a nil
// StunClient — newPerDeviceStunFactory only needs to prove routing, not that
// either underlying factory succeeds.
func stubFactory(calls *[]string) stun.ClientFactory {
	return func(_ context.Context, deviceName string, _ uint16, _ string, _ int, _ []string, _ bool) (stun.StunClient, error) {
		*calls = append(*calls, deviceName)
		return nil, nil
	}
}

// This is the previously missing mixed-config coverage: one interface with
// proxy.enabled true, another false, must route to the proxy and plain STUN
// factories respectively rather than collapsing onto the process-wide OR.
func TestNewPerDeviceStunFactory_MixedConfig_RoutesPerDevice(t *testing.T) {
	enabled := true
	disabled := false
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {Proxy: config.Proxy{Enabled: &enabled}},
		"wg1": {Proxy: config.Proxy{Enabled: &disabled}},
	}}
	deviceConfig := config.NewDeviceConfig(cfg)

	var proxyCalls, plainCalls []string
	factory := newPerDeviceStunFactory(deviceConfig, "linux", stubFactory(&proxyCalls), stubFactory(&plainCalls))

	if _, err := factory(context.Background(), "wg0", 0, "ipv4", 0, nil, false); err != nil {
		t.Fatalf("factory(wg0): %v", err)
	}
	if _, err := factory(context.Background(), "wg1", 0, "ipv4", 0, nil, false); err != nil {
		t.Fatalf("factory(wg1): %v", err)
	}

	if len(proxyCalls) != 1 || proxyCalls[0] != "wg0" {
		t.Errorf("proxy factory calls = %v, want [wg0]", proxyCalls)
	}
	if len(plainCalls) != 1 || plainCalls[0] != "wg1" {
		t.Errorf("plain factory calls = %v, want [wg1]; the disabled interface must not ride the proxy path", plainCalls)
	}
}

// Backward-compat guard: a single-interface config must route identically to
// how it behaved before this change — enabled always hits the proxy factory,
// disabled always hits the plain one, regardless of the other's calls.
func TestNewPerDeviceStunFactory_SingleInterface_MatchesPriorBehavior(t *testing.T) {
	enabled := true
	disabled := false

	t.Run("enabled", func(t *testing.T) {
		cfg := &config.Config{Interfaces: config.Interfaces{"wg0": {Proxy: config.Proxy{Enabled: &enabled}}}}
		deviceConfig := config.NewDeviceConfig(cfg)
		var proxyCalls, plainCalls []string
		factory := newPerDeviceStunFactory(deviceConfig, "linux", stubFactory(&proxyCalls), stubFactory(&plainCalls))

		if _, err := factory(context.Background(), "wg0", 0, "ipv4", 0, nil, false); err != nil {
			t.Fatalf("factory(wg0): %v", err)
		}
		if len(proxyCalls) != 1 || len(plainCalls) != 0 {
			t.Errorf("proxyCalls=%v plainCalls=%v, want proxy-only", proxyCalls, plainCalls)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &config.Config{Interfaces: config.Interfaces{"wg0": {Proxy: config.Proxy{Enabled: &disabled}}}}
		deviceConfig := config.NewDeviceConfig(cfg)
		var proxyCalls, plainCalls []string
		factory := newPerDeviceStunFactory(deviceConfig, "linux", stubFactory(&proxyCalls), stubFactory(&plainCalls))

		if _, err := factory(context.Background(), "wg0", 0, "ipv4", 0, nil, false); err != nil {
			t.Fatalf("factory(wg0): %v", err)
		}
		if len(plainCalls) != 1 || len(proxyCalls) != 0 {
			t.Errorf("proxyCalls=%v plainCalls=%v, want plain-only", proxyCalls, plainCalls)
		}
	})
}
