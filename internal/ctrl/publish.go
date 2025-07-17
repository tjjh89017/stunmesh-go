package ctrl

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

type PublishController struct {
	devices       DeviceRepository
	peers         PeerRepository
	pluginManager *plugin.Manager
	resolver      StunResolver
	encryptor     EndpointEncryptor
	logger        zerolog.Logger
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, pluginManager *plugin.Manager, resolver StunResolver, encryptor EndpointEncryptor, logger *zerolog.Logger) *PublishController {
	return &PublishController{
		devices:       devices,
		peers:         peers,
		pluginManager: pluginManager,
		resolver:      resolver,
		encryptor:     encryptor,
		logger:        logger.With().Str("controller", "publish").Logger(),
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

		host, port, err := c.resolver.Resolve(ctx, string(device.Name()), uint16(device.ListenPort()))
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

			store, err := c.pluginManager.GetPlugin(peer.Plugin())
			if err != nil {
				logger.Error().Err(err).Str("plugin", peer.Plugin()).Msg("failed to get plugin")
				continue
			}

			logger.Info().Str("plugin", peer.Plugin()).Msg("store endpoint")
			storeCtx := logger.WithContext(ctx)
			err = store.Set(storeCtx, peer.LocalId(), res.Data)
			if err != nil {
				logger.Error().Err(err).Msg("failed to store endpoint")
				continue
			}
		}
	}
}

func (c *PublishController) ExecuteForPeer(ctx context.Context, peerId entity.PeerId) {
	// Find the specific peer
	peer, err := c.peers.Find(ctx, peerId)
	if err != nil {
		c.logger.Error().Err(err).Str("peer_id", peerId.String()).Msg("failed to find peer")
		return
	}

	// Find the device for this peer
	device, err := c.devices.Find(ctx, entity.DeviceId(peer.DeviceName()))
	if err != nil {
		c.logger.Error().Err(err).Str("device", peer.DeviceName()).Msg("failed to find device")
		return
	}

	logger := c.logger.With().
		Str("device", string(device.Name())).
		Str("peer", peer.LocalId()).
		Logger()

	// Resolve outside address
	host, port, err := c.resolver.Resolve(ctx, string(device.Name()), uint16(device.ListenPort()))
	if err != nil {
		logger.Error().Err(err).Msg("failed to resolve outside address")
		return
	}

	logger.Info().Str("host", host).Int("port", int(port)).Msg("outside address for specific peer")

	// Encrypt endpoint data
	res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Host:          host,
		Port:          port,
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to encrypt endpoint")
		return
	}

	// Get plugin store
	store, err := c.pluginManager.GetPlugin(peer.Plugin())
	if err != nil {
		logger.Error().Err(err).Str("plugin", peer.Plugin()).Msg("failed to get plugin")
		return
	}

	// Store endpoint data
	storeCtx := context.WithoutCancel(ctx)
	err = store.Set(storeCtx, peer.LocalId(), res.Data)
	if err != nil {
		logger.Error().Err(err).Msg("failed to store endpoint for specific peer")
		return
	}

	logger.Info().Msg("successfully published endpoint for specific peer")
}
