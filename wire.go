//go:build wireinject
// +build wireinject

package main

import (
	"github.com/cloudflare/cloudflare-go"
	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/store"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
)

func setup() (*daemon.Daemon, error) {
	wire.Build(
		config.Load,
		wgctrl.New,
		provideCloudflareApi,
		provideStore,
		wire.Bind(new(plugin.Store), new(*store.CloudflareStore)),
		provideRefreshQueue,
		wire.Bind(new(ctrl.RefreshQueue), new(*queue.Queue[entity.PeerId])),
		repo.DefaultSet,
		stun.DefaultSet,
		crypto.DefaultSet,
		ctrl.DefaultSet,
		daemon.New,
	)

	return nil, nil
}

func provideCloudflareApi(config *config.Config) (*cloudflare.API, error) {
	return cloudflare.New(config.Cloudflare.ApiKey, config.Cloudflare.ApiEmail)
}

func provideStore(cfApi *cloudflare.API, config *config.Config) *store.CloudflareStore {
	return store.NewCloudflareStore(cfApi, config.Cloudflare.ZoneName)
}

func provideRefreshQueue() *queue.Queue[entity.PeerId] {
	return queue.New[entity.PeerId]()
}
