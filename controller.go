package main

import (
	"context"
	"log"
	"net"

	"github.com/pion/stun"
	"github.com/tjjh89017/stunmesh-go/internal/session"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const StunServerAddr = "stun.l.google.com:19302"

type Controller struct {
	wgCtrl *wgctrl.Client
	store  plugin.Store
}

func NewController(ctrl *wgctrl.Client, store plugin.Store) *Controller {
	return &Controller{
		wgCtrl: ctrl,
		store:  store,
	}
}

func (c *Controller) Publish(ctx context.Context, serializer Serializer, peer *Peer) {
	log.Printf("connecting to STUN server: %s\n", StunServerAddr)
	stunAddr, err := net.ResolveUDPAddr("udp4", StunServerAddr)
	if err != nil {
		log.Panic(err)
	}

	conn, err := session.New(uint16(peer.ListenPort()))
	if err != nil {
		log.Panic(err)
	}

	defer conn.Close()
	if err := conn.Start(); err != nil {
		log.Panic(err)
	}

	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	resData, err := conn.RoundTrip(request, stunAddr)
	if err != nil {
		log.Panic(err)
	}

	xorAddr := session.Parse(resData)
	if xorAddr != nil {
		log.Printf("addr: %s\n", xorAddr.String())
	} else {
		log.Printf("error no xor addr")
	}

	endpointData, err := serializer.Serialize(xorAddr.IP.String(), xorAddr.Port)
	if err != nil {
		log.Panic(err)
	}

	err = c.store.Set(ctx, peer.Id(), endpointData)
	if err != nil {
		log.Panic(err)
	}
}

func (c *Controller) Establish(ctx context.Context, serializer Deserializer, peer *Peer) {
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
