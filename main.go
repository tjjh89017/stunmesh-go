package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"golang.zx2c4.com/wireguard/wgctrl"
)

var (
	ErrResponseMessage = errors.New("error reading from response message channel")
	ErrTimeout         = errors.New("timed out waiting for response")
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

	store := NewCloudflareStore(cfApi, config.Cloudflare.ZoneName)
	ctrl := NewController(wg, store)

	peers := make([]*Peer, len(device.Peers))

	for _, p := range device.Peers {
		peer := NewPeer(
			buildEndpointKey(device.PublicKey[:], p.PublicKey[:]),
			buildEndpointKey(p.PublicKey[:], device.PublicKey[:]),
			device.Name,
			device.ListenPort,
			p.PublicKey,
		)

		peers = append(peers, peer)
	}

	Run(ctx, localPrivateKey, ctrl, peers)
}
