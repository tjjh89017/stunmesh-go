package stun

import (
	"context"
	"errors"
	"sync"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

var ErrAllServersFailed = errors.New("all STUN servers failed")

// StunClient is the interface for a STUN client instance.
type StunClient interface {
	Start(ctx context.Context)
	Stop() error
	Connect(ctx context.Context, addr string) (string, int, error)
}

type Resolver struct {
	config       *config.Config
	deviceConfig *config.DeviceConfig
	logger       zerolog.Logger
	mu           sync.Mutex
	newClient    func(ctx context.Context, deviceName string, port uint16, protocol string) (StunClient, error)
}

func NewResolver(config *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) *Resolver {
	return &Resolver{
		config:       config,
		deviceConfig: deviceConfig,
		logger:       logger.With().Str("component", "stun").Logger(),
		newClient: func(ctx context.Context, deviceName string, port uint16, protocol string) (StunClient, error) {
			return New(ctx, deviceName, port, protocol)
		},
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

	stun, err := r.newClient(stunCtx, deviceName, port, protocol)
	if err != nil {
		return "", 0, err
	}

	stun.Start(stunCtx)
	defer func() {
		if stopErr := stun.Stop(); stopErr != nil {
			r.logger.Warn().Err(stopErr).Msg("failed to stop STUN client")
		}
	}()

	servers := r.config.Stun.GetServers()
	for _, server := range servers {
		host, discoveredPort, connectErr := stun.Connect(stunCtx, server)
		if connectErr != nil {
			r.logger.Warn().Err(connectErr).Str("server", server).Msg("STUN server failed, trying next")
			continue
		}

		// Validate the discovered endpoint
		if discoveredPort == 0 || host == "" {
			r.logger.Warn().
				Str("server", server).
				Str("host", host).
				Int("port", discoveredPort).
				Str("protocol", protocol).
				Msg("STUN returned invalid endpoint, trying next server")
			continue
		}

		return host, discoveredPort, nil
	}

	return "", 0, ErrAllServersFailed
}
