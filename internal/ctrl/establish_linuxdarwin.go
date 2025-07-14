//go:build linux || darwin

package ctrl

import (
	"context"
	"net"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func (c *EstablishController) ConfigureDevice(ctx context.Context, peer *entity.Peer, res *EndpointDecryptResponse) error {
	c.logger.Debug().Str("peer", peer.LocalId()).Msg("configuring device for peer")

	err := c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
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

	return err
}
