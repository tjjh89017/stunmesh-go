package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	"github.com/tjjh89017/stunmesh-go/internal/store"
	"golang.zx2c4.com/wireguard/wgctrl"
)

func main() {
	fmt.Println("Stunmesh Go")
	ctx := context.Background()

	config, err := config.Load()
	if err != nil {
		log.Panic(err)
	}

	wg, err := wgctrl.New()
	if err != nil {
		log.Panic(err)
	}

	device, err := wg.Device(config.WireGuard)
	if err != nil {
		log.Panic(err)
	}

	deviceEntity := entity.NewDevice(
		entity.DeviceId(device.Name),
		device.PrivateKey[:],
	)

	cfApi, err := cloudflare.New(config.Cloudflare.ApiKey, config.Cloudflare.ApiEmail)
	if err != nil {
		log.Panic(err)
	}

	store := store.NewCloudflareStore(cfApi, config.Cloudflare.ZoneName)
	peers := repo.NewPeers()
	devices := repo.NewDevices()
	endpointCrypto := crypto.NewEndpoint()
	refreshQueue := queue.New[entity.PeerId]()
	publishCtrl := ctrl.NewPublishController(devices, peers, store, endpointCrypto)
	establishCtrl := ctrl.NewEstablishController(wg, devices, peers, store, endpointCrypto)
	refreshCtrl := ctrl.NewRefreshController(peers, refreshQueue)

	devices.Save(ctx, deviceEntity)

	for _, p := range device.Peers {
		peer := entity.NewPeer(
			entity.NewPeerId(device.PublicKey[:], p.PublicKey[:]),
			device.Name,
			device.ListenPort,
			p.PublicKey,
		)

		peers.Save(ctx, peer)
	}

	daemon := daemon.New(config, refreshQueue, publishCtrl, establishCtrl, refreshCtrl)
	daemon.Run(ctx)
}
