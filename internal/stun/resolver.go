package stun

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

type Resolver struct {
	config       *config.Config
	deviceConfig *config.DeviceConfig
	logger       zerolog.Logger
	mu           sync.Mutex
}

func NewResolver(config *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) *Resolver {
	return &Resolver{
		config:       config,
		deviceConfig: deviceConfig,
		logger:       logger.With().Str("component", "stun").Logger(),
	}
}

// Resolve performs STUN discovery for the specified device and protocol
// protocol must be "ipv4" or "ipv6" (not "dualstack")
// Returns error if STUN discovery fails or returns invalid endpoint (port=0 or empty host)
func (r *Resolver) Resolve(ctx context.Context, deviceName string, port uint16, protocol string) (_ string, _ int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stunCtx := r.logger.WithContext(ctx)

	r.logger.Debug().Str("device", deviceName).Str("protocol", protocol).Msg("resolving with protocol")

	stun, err := New(stunCtx, deviceName, port, protocol)
	if err != nil {
		return "", 0, err
	}

	stun.Start(stunCtx)
	defer func() {
		err = stun.Stop()
	}()

	host, discoveredPort, err := stun.Connect(stunCtx, r.config.Stun.Address)
	if err != nil {
		return "", 0, err
	}

	// Validate the discovered endpoint
	if discoveredPort == 0 || host == "" {
		r.logger.Warn().
			Str("host", host).
			Int("port", discoveredPort).
			Str("protocol", protocol).
			Msg("STUN returned invalid endpoint")
		return "", 0, ErrInvalidEndpoint
	}

	return host, discoveredPort, nil
}
