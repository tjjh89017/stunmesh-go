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

// proxyModeEnabled reports whether the proxy fronts the wg client: always on
// Windows; elsewhere only when proxy.listen is set (test-only, unsupported).
func proxyModeEnabled(cfg *config.Config, logger *zerolog.Logger) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	for name, iface := range cfg.Interfaces {
		if iface.Proxy.Listen != 0 {
			logger.Warn().Str("interface", name).Msg("proxy.listen is set on a non-Windows platform: proxy mode here is test-only and unsupported")
			return true
		}
	}
	return false
}

// newProxyStack wires the proxy decorator and proxy-backed STUN path when
// proxy mode is on. The per-device transport lookup is safe because bootstrap
// creates each proxy before any Resolve runs; the trailing manager.Close is
// idempotent and covers the idle manager in plain mode.
func newProxyStack(cfg *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) (*proxyStack, func(), error) {
	mode := proxyModeEnabled(cfg, logger)
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
