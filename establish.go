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
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	serializer Deserializer,
	store Store,
) {
	endpointKey := buildEndpointKey(peer.PublicKey[:], device.PublicKey[:])
	endpointData, err := store.Get(context.Background(), endpointKey)
	if err != nil {
		log.Panic(err)
	}

	host, port, err := serializer.Deserialize(endpointData)
	if err != nil {
		log.Panic(err)
	}

	err = ctrl.ConfigureDevice(device.Name, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey,
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
