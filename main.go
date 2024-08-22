package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
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

	var localPrivateKey [32]byte
	copy(localPrivateKey[:], device.PrivateKey[:])

	cfApi, err := cloudflare.New(config.Cloudflare.ApiKey, config.Cloudflare.ApiEmail)
	if err != nil {
		log.Panic(err)
	}

	store := store.NewCloudflareStore(cfApi, config.Cloudflare.ZoneName)
	publishCtrl := ctrl.NewPublishController(wg, store)
	establishCtrl := ctrl.NewEstablishController(wg, store)

	peers := make([]*entity.Peer, len(device.Peers))

	for _, p := range device.Peers {
		peer := entity.NewPeer(
			buildEndpointKey(device.PublicKey[:], p.PublicKey[:]),
			buildEndpointKey(p.PublicKey[:], device.PublicKey[:]),
			device.Name,
			device.ListenPort,
			p.PublicKey,
		)

		peers = append(peers, peer)
	}

	Run(ctx, localPrivateKey, publishCtrl, establishCtrl, peers)
}
