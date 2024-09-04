package session

import (
	"fmt"
	"log"

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
	conn, err := New(port)
	if err != nil {
		return "", 0, err
	}

	resData, err := conn.Wait(r.config.Stun.Address, port)
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
