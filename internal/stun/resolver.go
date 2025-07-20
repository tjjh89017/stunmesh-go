package stun

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

type Resolver struct {
	config *config.Config
	logger zerolog.Logger
	mu     sync.Mutex
}

func NewResolver(config *config.Config, logger *zerolog.Logger) *Resolver {
	return &Resolver{
		config: config,
		logger: logger.With().Str("component", "stun").Logger(),
	}
}

func (r *Resolver) Resolve(ctx context.Context, deviceName string, port uint16) (_ string, _ int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stunCtx := r.logger.WithContext(ctx)

	stun, err := New(stunCtx, deviceName, port)
	if err != nil {
		return "", 0, err
	}

	stun.Start(stunCtx)
	defer func() {
		err = stun.Stop()
		r.logger.Trace().Msg("resolve stopped")
	}()

	return stun.Connect(stunCtx, r.config.Stun.Address)
}
