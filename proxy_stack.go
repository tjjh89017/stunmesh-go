package main

import (
	"context"
	"runtime"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

// proxyStack bundles the two components whose construction depends on
// whether proxy mode is on; newProxyStack builds it either way so
// wire_gen.go stays a single, unconditional code path.
type proxyStack struct {
	Client   wg.Client
	Resolver *stun.Resolver
}

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
	// mode (the OR across every interface) only decides whether proxy
	// infrastructure is built at all. It does not decide whether any given
	// device's traffic rides it — wg.NewProxyClient and
	// newPerDeviceStunFactory each re-check deviceConfig.GetProxyEnabled
	// per deviceName, so an interface that opted out is left on the plain
	// path even while the decorator/factory is installed for the process.
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
		factory := newPerDeviceStunFactory(deviceConfig, runtime.GOOS, stun.NewProxyLookupFactory(lookup, logger), stun.NewDefaultFactory())
		resolver = stun.NewResolverWithFactory(cfg, deviceConfig, logger, factory)
	}

	return &proxyStack{Client: client, Resolver: resolver}, cleanup, nil
}

// newPerDeviceStunFactory routes each Resolve call to proxyFactory or
// plainFactory based on that device's own proxy.enabled, rather than the
// process-wide OR that decided whether to build proxy infrastructure at all.
func newPerDeviceStunFactory(deviceConfig *config.DeviceConfig, goos string, proxyFactory, plainFactory stun.ClientFactory) stun.ClientFactory {
	return func(ctx context.Context, deviceName string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (stun.StunClient, error) {
		if deviceConfig.GetProxyEnabled(deviceName, goos) {
			return proxyFactory(ctx, deviceName, port, protocol, firewallMark, listenInterfaces, listenDefaultRoute)
		}
		return plainFactory(ctx, deviceName, port, protocol, firewallMark, listenInterfaces, listenDefaultRoute)
	}
}
