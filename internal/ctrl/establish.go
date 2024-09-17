package ctrl

import (
	"context"
	"log"
	"net"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type EstablishController struct {
	wgCtrl    *wgctrl.Client
	devices   DeviceRepository
	peers     PeerRepository
	store     plugin.Store
	decryptor EndpointDecryptor
}

func NewEstablishController(ctrl *wgctrl.Client, devices DeviceRepository, peers PeerRepository, store plugin.Store, decryptor EndpointDecryptor) *EstablishController {
	return &EstablishController{
		wgCtrl:    ctrl,
		devices:   devices,
		peers:     peers,
		store:     store,
		decryptor: decryptor,
	}
}

func (c *EstablishController) Execute(ctx context.Context, peerId entity.PeerId) {
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

	endpointData, err := c.store.Get(ctx, peer.RemoteId())
	if err != nil {
		// Failed to get, maybe endpoint didn't upload the record yet, skip.
		log.Print(err)
		return
	}

	res, err := c.decryptor.Decrypt(ctx, &EndpointDecryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Data:          endpointData,
	})
	if err != nil {
		log.Panic(err)
	}

	err = c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey(),
				UpdateOnly: true,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(res.Host),
					Port: res.Port,
				},
			},
		},
	})

	if err != nil {
		log.Panic(err)
	}
}
