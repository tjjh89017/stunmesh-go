package ctrl

import (
	"context"
	"log"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

type PublishController struct {
	devices   DeviceRepository
	peers     PeerRepository
	store     plugin.Store
	resolver  StunResolver
	encryptor EndpointEncryptor
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, store plugin.Store, resolver StunResolver, encryptor EndpointEncryptor) *PublishController {
	return &PublishController{
		devices:   devices,
		peers:     peers,
		store:     store,
		resolver:  resolver,
		encryptor: encryptor,
	}
}

func (c *PublishController) Execute(ctx context.Context, peerId entity.PeerId) {
	peer, err := c.peers.Find(ctx, peerId)
	if err != nil {
		log.Print(err)
		return
	}

	device, err := c.devices.Find(ctx, entity.DeviceId(peer.DeviceName()))
	if err != nil {
		log.Print(err)
		return
	}

	host, port, err := c.resolver.Resolve(uint16(peer.ListenPort()))
	if err != nil {
		log.Panic(err)
	}

	res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Host:          host,
		Port:          port,
	})
	if err != nil {
		log.Panic(err)
	}

	err = c.store.Set(ctx, peer.LocalId(), res.Data)
	if err != nil {
		log.Panic(err)
	}
}
