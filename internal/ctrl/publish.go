package ctrl

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

type PublishController struct {
	devices   DeviceRepository
	peers     PeerRepository
	store     plugin.Store
	resolver  StunResolver
	encryptor EndpointEncryptor
	logger    zerolog.Logger
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, store plugin.Store, resolver StunResolver, encryptor EndpointEncryptor, logger *zerolog.Logger) *PublishController {
	return &PublishController{
		devices:   devices,
		peers:     peers,
		store:     store,
		resolver:  resolver,
		encryptor: encryptor,
		logger:    logger.With().Str("controller", "publish").Logger(),
	}
}

func (c *PublishController) Execute(ctx context.Context) {
	devices, err := c.devices.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list devices")
		return
	}

	for _, device := range devices {
		logger := c.logger.With().Str("device", string(device.Name())).Logger()

		host, port, err := c.resolver.Resolve(ctx, uint16(device.ListenPort()))
		if err != nil {
			logger.Error().Err(err).Msg("failed to resolve outside address")
			continue
		}

		logger.Info().Str("host", host).Int("port", int(port)).Msg("outside address")
		peers, err := c.peers.ListByDevice(ctx, device.Name())
		if err != nil {
			logger.Error().Err(err).Msg("failed to list peers")
			continue
		}

		for _, peer := range peers {
			logger := logger.With().Str("peer", peer.LocalId()).Logger()

			res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
				PeerPublicKey: peer.PublicKey(),
				PrivateKey:    device.PrivateKey(),
				Host:          host,
				Port:          port,
			})
			if err != nil {
				logger.Error().Err(err).Msg("failed to encrypt endpoint")
				continue
			}

			storeCtx := logger.WithContext(ctx)
			err = c.store.Set(storeCtx, peer.LocalId(), res.Data)
			if err != nil {
				logger.Error().Err(err).Msg("failed to store endpoint")
				continue
			}
		}
	}
}
