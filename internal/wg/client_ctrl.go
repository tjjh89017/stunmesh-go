//go:build !wgcli

package wg

import (
	"net"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type ctrlClient struct {
	c *wgctrl.Client
}

func New() (Client, error) {
	c, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	return &ctrlClient{c: c}, nil
}

func (cc *ctrlClient) Device(name string) (*DeviceInfo, error) {
	d, err := cc.c.Device(name)
	if err != nil {
		return nil, err
	}

	peerKeys := make([]Key, 0, len(d.Peers))
	for _, peer := range d.Peers {
		peerKeys = append(peerKeys, Key(peer.PublicKey))
	}

	return &DeviceInfo{
		Name:       d.Name,
		ListenPort: d.ListenPort,
		PrivateKey: Key(d.PrivateKey),
		PublicKey:  Key(d.PublicKey),
		PeerKeys:   peerKeys,
	}, nil
}

func (cc *ctrlClient) UpdatePeerEndpoint(u PeerEndpointUpdate) error {
	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  wgtypes.Key(u.PublicKey),
				UpdateOnly: UpdateOnly,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(u.Host),
					Port: u.Port,
				},
			},
		},
	}
	return cc.c.ConfigureDevice(u.DeviceName, cfg)
}

func (cc *ctrlClient) Close() error {
	return cc.c.Close()
}
