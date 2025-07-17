//go:build wireinject
// +build wireinject

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
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
)

func setup(configPath string) (*daemon.Daemon, error) {
	config, err := provideConfigFromPath(configPath)
	if err != nil {
		return nil, err
	}
	
	wire.Build(
		wgctrl.New,
		wire.Bind(new(ctrl.WireGuardClient), new(*wgctrl.Client)),
		wire.Bind(new(repo.WireGuardClient), new(*wgctrl.Client)),
		wire.Bind(new(entity.ConfigPeerProvider), new(*config.DeviceConfig)),
		wire.Bind(new(entity.DevicePeerChecker), new(*repo.Peers)),
		providePluginManager,
		provideRefreshQueue,
		wire.Value(config),
		config.NewDeviceConfig,
		wire.Bind(new(ctrl.RefreshQueue), new(*queue.Queue[entity.PeerId])),
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

	// Convert config.PluginDefinition to plugin.PluginDefinition
	pluginsMap := make(map[string]plugin.PluginDefinition)
	for name, def := range config.Plugins {
		pluginsMap[name] = plugin.PluginDefinition{
			Type:   def.Type,
			Config: plugin.PluginConfig(def.Config),
		}
	}

	if err := manager.LoadPlugins(ctx, pluginsMap); err != nil {
		return nil, err
	}

	return manager, nil
}

func provideRefreshQueue() *queue.Queue[entity.PeerId] {
	return queue.New[entity.PeerId]()
}

func provideConfigFromPath(configPath string) (*config.Config, error) {
	return config.LoadWithPath(configPath)
}
