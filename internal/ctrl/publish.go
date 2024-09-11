package ctrl

import (
	"context"
	"log"

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

func (c *PublishController) Execute(ctx context.Context) {
	devices, err := c.devices.List(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	for _, device := range devices {
		host, port, err := c.resolver.Resolve(ctx, uint16(device.ListenPort()))
		if err != nil {
			log.Panic(err)
		}

		peers, err := c.peers.ListByDevice(ctx, device.Name())
		if err != nil {
			log.Print(err)
			continue
		}

		for _, peer := range peers {
			res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
				PeerPublicKey: peer.PublicKey(),
				PrivateKey:    device.PrivateKey(),
				Host:          host,
				Port:          port,
			})
			if err != nil {
				log.Print(err)
				continue
			}

			err = c.store.Set(ctx, peer.LocalId(), res.Data)
			if err != nil {
				log.Print(err)
				continue
			}
		}
	}
}
