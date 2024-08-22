package ctrl

import (
	"context"
	"log"
	"net"

	"github.com/pion/stun"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/session"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
)

const StunServerAddr = "stun.l.google.com:19302"

type PublishController struct {
	wgCtrl *wgctrl.Client
	store  plugin.Store
}

func NewPublishController(ctrl *wgctrl.Client, store plugin.Store) *PublishController {
	return &PublishController{
		wgCtrl: ctrl,
		store:  store,
	}
}

func (c *PublishController) Execute(ctx context.Context, serializer Serializer, peer *entity.Peer) {
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
