package session

import (
	"fmt"
	"log"
	"net"

	"github.com/pion/stun"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

type Resolver struct {
	config *config.Config
}

func NewResolver(config *config.Config) *Resolver {
	return &Resolver{
		config: config,
	}
}

func (r *Resolver) Resolve(port uint16) (string, int, error) {
	log.Printf("connecting to STUN server: %s\n", r.config.Stun.Address)
	stunAddr, err := net.ResolveUDPAddr("udp4", r.config.Stun.Address)
	if err != nil {
		return "", 0, err
	}

	conn, err := New(port)
	if err != nil {
		return "", 0, err
	}

	defer conn.Close()
	if err := conn.Start(); err != nil {
		return "", 0, err
	}

	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	resData, err := conn.RoundTrip(request, stunAddr)
	if err != nil {
		return "", 0, err
	}

	xorAddr := Parse(resData)
	if xorAddr != nil {
		log.Printf("addr: %s\n", xorAddr.String())
	} else {
		return "", 0, fmt.Errorf("no xor addr")
	}

	return xorAddr.IP.String(), xorAddr.Port, nil
}
