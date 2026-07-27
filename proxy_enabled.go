//go:build windows || wgproxy

package main

import (
	"runtime"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

// proxyModeEnabled reports whether the proxy fronts the wg client: on iff any
// interface's proxy.enabled resolves true for runtime.GOOS (see
// config.Proxy.IsEnabled; always true on Windows).
func proxyModeEnabled(cfg *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) bool {
	return proxyModeEnabledForGOOS(cfg, deviceConfig, runtime.GOOS, logger)
}

// proxyModeEnabledForGOOS is the pure, goos-parameterized core of
// proxyModeEnabled, kept testable from any host platform. A lone
// proxy.listen with no proxy.enabled does not turn proxy mode on.
func proxyModeEnabledForGOOS(cfg *config.Config, deviceConfig *config.DeviceConfig, goos string, logger *zerolog.Logger) bool {
	enabled := false
	for name := range cfg.Interfaces {
		if deviceConfig.GetProxyEnabled(name, goos) {
			logger.Info().Str("interface", name).Msg("proxy mode enabled")
			enabled = true
		}
	}
	return enabled
}

// newProxyStack wires the proxy decorator and proxy-backed STUN path when
// proxy mode is on. The per-device transport lookup is safe because bootstrap
// creates each proxy before any Resolve runs; the trailing manager.Close is
// idempotent and covers the idle manager in plain mode.
func newProxyStack(cfg *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) (*proxyStack, func(), error) {
	mode := proxyModeEnabled(cfg, deviceConfig, logger)
	manager := wgproxy.NewManager(logger)

	client, err := wg.New()
	if err != nil {
		return nil, nil, err
	}
	if mode {
		client = wg.NewProxyClient(client, manager, deviceConfig, logger)
	}
	cleanup := func() {
		if err := client.Close(); err != nil {
			logger.Warn().Err(err).Msg("failed to close WireGuard client")
		}
		if err := manager.Close(); err != nil {
			logger.Warn().Err(err).Msg("failed to close wgproxy manager")
		}
	}

	resolver := stun.NewResolver(cfg, deviceConfig, logger)
	if mode {
		lookup := func(deviceName string) (stun.StunTransport, error) {
			proxy, err := manager.Get(deviceName)
			if err != nil {
				return nil, err
			}
			return proxy, nil
		}
		resolver = stun.NewResolverWithFactory(cfg, deviceConfig, logger, stun.NewProxyLookupFactory(lookup, logger))
	}

	return &proxyStack{Client: client, Resolver: resolver}, cleanup, nil
}
