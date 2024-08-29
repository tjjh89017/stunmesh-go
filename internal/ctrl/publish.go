package ctrl

import (
	"context"
	"log"
	"net"

	"github.com/pion/stun"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/session"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

const StunServerAddr = "stun.l.google.com:19302"

type PublishController struct {
	devices   DeviceRepository
	peers     PeerRepository
	store     plugin.Store
	encryptor EndpointEncryptor
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, store plugin.Store, encryptor EndpointEncryptor) *PublishController {
	return &PublishController{
		devices:   devices,
		peers:     peers,
		store:     store,
		encryptor: encryptor,
	}
}

func (c *PublishController) Execute(ctx context.Context, peerId entity.PeerId) {
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

	res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Host:          xorAddr.IP.String(),
		Port:          xorAddr.Port,
	})
	if err != nil {
		log.Panic(err)
	}

	err = c.store.Set(ctx, peer.LocalId(), res.Data)
	if err != nil {
		log.Panic(err)
	}
}
