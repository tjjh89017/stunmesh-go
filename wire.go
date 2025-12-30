//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/logger"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/pluginapi"
	"golang.zx2c4.com/wireguard/wgctrl"
)

func setup() (*daemon.Daemon, error) {
	wire.Build(
		wgctrl.New,
		wire.Bind(new(ctrl.WireGuardClient), new(*wgctrl.Client)),
		wire.Bind(new(repo.WireGuardClient), new(*wgctrl.Client)),
		wire.Bind(new(entity.ConfigPeerProvider), new(*config.DeviceConfig)),
		wire.Bind(new(entity.DevicePeerChecker), new(*repo.Peers)),
		wire.Bind(new(ctrl.DeviceConfigProvider), new(*config.DeviceConfig)),
		providePluginManager,
		ctrl.NewPingMonitorController,
		config.DefaultSet,
		logger.DefaultSet,
		repo.DefaultSet,
		stun.DefaultSet,
		crypto.DefaultSet,
		ctrl.DefaultSet,
		entity.DefaultSet,
		daemon.New,
	)

	return nil, nil
}

func providePluginManager(config *config.Config) (*plugin.Manager, error) {
	manager := plugin.NewManager()
	ctx := context.Background()

	// Convert config.PluginDefinition to pluginapi.PluginDefinition
	pluginsMap := make(map[string]pluginapi.PluginDefinition)
	for name, def := range config.Plugins {
		pluginsMap[name] = pluginapi.PluginDefinition{
			Type:   def.Type,
			Config: pluginapi.PluginConfig(def.Config),
		}
	}

	if err := manager.LoadPlugins(ctx, pluginsMap); err != nil {
		return nil, err
	}

	return manager, nil
}
