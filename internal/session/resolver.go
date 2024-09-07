package session

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

func (r *Resolver) Resolve(ctx context.Context, port uint16) (string, int, error) {
	bpfFilter, err := stunBpfFilter(port)
	if err != nil {
		return "", 0, err
	}

	session, err := New(bpfFilter...)
	if err != nil {
		return "", 0, err
	}

	session.Start(ctx)
	defer session.Stop()

	return session.Bind(ctx, r.config.Stun.Address, port)
}
