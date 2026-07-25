//go:build !windows && !wgproxy

package main

import (
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

// newProxyStack builds the plain wg client and platform-default STUN resolver.
func newProxyStack(cfg *config.Config, deviceConfig *config.DeviceConfig, logger *zerolog.Logger) (*proxyStack, func(), error) {
	client, err := wg.New()
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if err := client.Close(); err != nil {
			logger.Warn().Err(err).Msg("failed to close WireGuard client")
		}
	}
	return &proxyStack{
		Client:   client,
		Resolver: stun.NewResolver(cfg, deviceConfig, logger),
	}, cleanup, nil
}
