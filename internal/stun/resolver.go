package stun

import (
	"context"

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

func (r *Resolver) Resolve(ctx context.Context, port uint16) (_ string, _ int, err error) {
	stun, err := New(port)
	if err != nil {
		return "", 0, err
	}

	stun.Start(ctx)
	defer func() {
		err = stun.Stop()
	}()

	return stun.Connect(ctx, r.config.Stun.Address)
}
