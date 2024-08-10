package main

import (
	"context"
	"log"

	"github.com/pion/stun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func broadcastPeers(
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	serializer Serializer,
	store Store,
) {
	conn, err := connect(uint16(device.ListenPort), "stun.l.google.com:19302")
	if err != nil {
		log.Panic(err)
	}
	defer conn.Close()

	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	resData, err := conn.roundTrip(request, conn.RemoteAddr)
	if err != nil {
		log.Panic(err)
	}

	response := parse(resData)
	if response.xorAddr != nil {
		log.Printf("addr: %s\n", response.xorAddr.String())
	} else {
		log.Printf("error no xor addr")
	}

	endpointData, err := serializer.Serialize(response.xorAddr.IP.String(), response.xorAddr.Port)
	if err != nil {
		log.Panic(err)
	}

	endpointKey := buildEndpointKey(device.PublicKey[:], peer.PublicKey[:])
	err = store.Set(context.Background(), endpointKey, endpointData)
	if err != nil {
		log.Panic(err)
	}
}
