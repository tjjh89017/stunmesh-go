package main

import (
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

	// assume we only have one peer
	peerCount := len(device.Peers)
	hasPeer := peerCount > 0
	if !hasPeer {
		log.Panicf("at least one peer is required, found %d\n", peerCount)
	}

	firstPeer := device.Peers[0]

	var remotePublicKey [32]byte
	var localPrivateKey [32]byte

	copy(remotePublicKey[:], firstPeer.PublicKey[:])
	copy(localPrivateKey[:], device.PrivateKey[:])
	envelop := NewSecureEnvelope(localPrivateKey, remotePublicKey)

	// prepare save to CloudFlare
	cfApi, err := cloudflare.New(config.Cloudflare.ApiKey, config.Cloudflare.ApiEmail)
	if err != nil {
		log.Panic(err)
	}
	// Fetch zone id
	zoneID, err := cfApi.ZoneIDByName(config.Cloudflare.ZoneName)
	if err != nil {
		log.Panic(err)
	}

	broadcastPeers(
		device,
		&firstPeer,
		envelop,
		cfApi,
		config.Cloudflare.ZoneName,
		zoneID,
	)

	establishPeers(
		wg,
		device,
		&firstPeer,
		envelop,
		cfApi,
		zoneID,
		config.Cloudflare.ZoneName,
	)
}
