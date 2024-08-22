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
	wgCtrl *wgctrl.Client
	store  plugin.Store
}

func NewEstablishController(ctrl *wgctrl.Client, store plugin.Store) *EstablishController {
	return &EstablishController{
		wgCtrl: ctrl,
		store:  store,
	}
}

func (c *EstablishController) Execute(ctx context.Context, serializer Deserializer, peer *entity.Peer) {
	endpointData, err := c.store.Get(ctx, peer.RemoteId())
	if err != nil {
		log.Panic(err)
	}

	host, port, err := serializer.Deserialize(endpointData)
	if err != nil {
		log.Panic(err)
	}

	err = c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey(),
				UpdateOnly: true,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(host),
					Port: port,
				},
			},
		},
	})

	if err != nil {
		log.Panic(err)
	}
}
