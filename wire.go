//go:build wireinject

package main

import (
	"context"
	"runtime"

	"github.com/google/wire"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/logger"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

func setup() (*daemon.Daemon, func(), error) {
	wire.Build(
		provideProxyMode,
		provideWGProxyManager,
		provideWGClient,
		wire.Bind(new(ctrl.WireGuardClient), new(wg.Client)),
		wire.Bind(new(repo.WireGuardClient), new(wg.Client)),
		wire.Bind(new(entity.ConfigPeerProvider), new(*config.DeviceConfig)),
		wire.Bind(new(entity.DevicePeerChecker), new(*repo.Peers)),
		wire.Bind(new(ctrl.DeviceConfigProvider), new(*config.DeviceConfig)),
		providePluginManager,
		provideStunResolver,
		wire.Bind(new(ctrl.StunResolver), new(*stun.Resolver)),
		ctrl.NewPingMonitorController,
		config.DefaultSet,
		logger.DefaultSet,
		repo.DefaultSet,
		crypto.DefaultSet,
		ctrl.DefaultSet,
		entity.DefaultSet,
		daemon.New,
	)

	return nil, nil, nil
}

func providePluginManager(config *config.Config) (*plugin.Manager, error) {
	manager := plugin.NewManager()
	ctx := context.Background()

	if err := manager.LoadPlugins(ctx, config.Plugins); err != nil {
		return nil, err
	}

	return manager, nil
}

// proxyMode reports whether the wgproxy decorator fronts the wg client.
type proxyMode bool

// provideProxyMode gates the proxy stack at runtime, never by build tag, so
// one wire_gen.go serves every GOOS: always on for Windows (the platform the
// proxy exists for), and on other platforms only when a proxy.listen key is
// set — the test-only harness path (plan section 7), warned as unsupported.
func provideProxyMode(cfg *config.Config, logger *zerolog.Logger) proxyMode {
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

func provideWGProxyManager(logger *zerolog.Logger) *wgproxy.Manager {
	return wgproxy.NewManager(logger)
}

// provideWGClient builds the platform client and, in proxy mode, wraps it
// with the wgproxy decorator. The cleanup closes the client (in proxy mode
// the decorator also closes the manager); the trailing manager.Close is a
// no-op then, and covers the idle manager in plain mode.
func provideWGClient(mode proxyMode, manager *wgproxy.Manager, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) (wg.Client, func(), error) {
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
	return client, cleanup, nil
}

// provideStunResolver picks the STUN data path: in proxy mode discovery rides
// each device's proxy outer sockets, looked up per Resolve by device name
// (bootstrap creates the proxy via the decorator's Device() before any
// Resolve runs); otherwise the platform default (raw socket / pcap).
func provideStunResolver(mode proxyMode, manager *wgproxy.Manager, cfg *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) *stun.Resolver {
	if !mode {
		return stun.NewResolver(cfg, deviceConfig, logger)
	}
	lookup := func(deviceName string) (stun.StunTransport, error) {
		proxy, err := manager.Get(deviceName)
		if err != nil {
			return nil, err
		}
		return proxy, nil
	}
	return stun.NewResolverWithFactory(cfg, deviceConfig, logger, stun.NewProxyLookupFactory(lookup, logger))
}
