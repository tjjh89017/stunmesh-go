// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package main

import (
	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/logger"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/store"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"golang.zx2c4.com/wireguard/wgctrl"
)

// Injectors from wire.go:

func setup() (*daemon.Daemon, error) {
	configConfig, err := config.Load()
	if err != nil {
		return nil, err
	}
	queue := provideRefreshQueue()
	client, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	devices := repo.NewDevices()
	peers := repo.NewPeers(client)
	zerologLogger := logger.NewLogger(configConfig)
	deviceConfig := config.NewDeviceConfig(configConfig)
	filterPeerService := entity.NewFilterPeerService(peers, deviceConfig)
	bootstrapController := ctrl.NewBootstrapController(client, configConfig, devices, peers, zerologLogger, filterPeerService)
	api, err := provideCloudflareApi(configConfig)
	if err != nil {
		return nil, err
	}
	cloudflareStore := provideStore(api, configConfig)
	resolver := stun.NewResolver(configConfig, zerologLogger)
	endpoint := crypto.NewEndpoint()
	publishController := ctrl.NewPublishController(devices, peers, cloudflareStore, resolver, endpoint, zerologLogger)
	establishController := ctrl.NewEstablishController(client, devices, peers, cloudflareStore, endpoint, zerologLogger)
	refreshController := ctrl.NewRefreshController(peers, queue, zerologLogger)
	daemonDaemon := daemon.New(configConfig, queue, bootstrapController, publishController, establishController, refreshController, zerologLogger)
	return daemonDaemon, nil
}

// wire.go:

func provideCloudflareApi(config2 *config.Config) (*cloudflare.API, error) {
	if config2.Cloudflare.ApiToken != "" {
		return cloudflare.NewWithAPIToken(config2.Cloudflare.ApiToken)
	}

	return cloudflare.New(config2.Cloudflare.ApiKey, config2.Cloudflare.ApiEmail)
}

func provideStore(cfApi *cloudflare.API, config2 *config.Config) *store.CloudflareStore {
	return store.NewCloudflareStore(cfApi, config2.Cloudflare.ZoneName)
}

func provideRefreshQueue() *queue.Queue[entity.PeerId] {
	return queue.New[entity.PeerId]()
}
