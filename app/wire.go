//go:build wireinject

package app

import (
	"context"

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
)

func setup(cfg *config.Config) (*daemon.Daemon, func(), error) {
	wire.Build(
		newProxyStack,
		wire.FieldsOf(new(*proxyStack), "Client", "Resolver"),
		wire.Bind(new(ctrl.WireGuardClient), new(wg.Client)),
		wire.Bind(new(repo.WireGuardClient), new(wg.Client)),
		wire.Bind(new(entity.ConfigPeerProvider), new(*config.DeviceConfig)),
		wire.Bind(new(entity.DevicePeerChecker), new(*repo.Peers)),
		wire.Bind(new(ctrl.DeviceConfigProvider), new(*config.DeviceConfig)),
		wire.Bind(new(ctrl.PluginProvider), new(*plugin.Manager)),
		providePluginManager,
		wire.Bind(new(ctrl.StunResolver), new(*stun.Resolver)),
		wire.Bind(new(daemon.BootstrapExecutor), new(*ctrl.BootstrapController)),
		wire.Bind(new(daemon.PublishRunner), new(*ctrl.PublishController)),
		wire.Bind(new(daemon.EstablishRunner), new(*ctrl.EstablishController)),
		wire.Bind(new(daemon.PingMonitorExecutor), new(*ctrl.PingMonitorController)),
		wire.Bind(new(ctrl.Publisher), new(*ctrl.PublishController)),
		wire.Bind(new(ctrl.Establisher), new(*ctrl.EstablishController)),
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

func providePluginManager(config *config.Config, logger *zerolog.Logger) (*plugin.Manager, func(), error) {
	manager := plugin.NewManager()
	ctx := context.Background()

	if err := manager.LoadPlugins(ctx, config.Plugins); err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if err := manager.Close(); err != nil {
			logger.Warn().Err(err).Msg("failed to close plugin manager")
		}
	}

	return manager, cleanup, nil
}
