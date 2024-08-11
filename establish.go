package main

import (
	"context"
	"log"
	"net"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func establishPeers(
	ctrl *wgctrl.Client,
	peer *Peer,
	serializer Deserializer,
	store Store,
) {
	endpointData, err := store.Get(context.Background(), peer.RemoteId())
	if err != nil {
		log.Panic(err)
	}

	host, port, err := serializer.Deserialize(endpointData)
	if err != nil {
		log.Panic(err)
	}

	err = ctrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
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
