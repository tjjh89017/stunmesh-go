package ctrl

import (
	"context"

	"encoding/base64"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type BootstrapController struct {
	wg      WireGuardClient
	config  *config.Config
	devices DeviceRepository
	peers   PeerRepository
	logger  zerolog.Logger
}

func NewBootstrapController(wg WireGuardClient, config *config.Config, devices DeviceRepository, peers PeerRepository, logger *zerolog.Logger) *BootstrapController {
	return &BootstrapController{
		wg:      wg,
		config:  config,
		devices: devices,
		peers:   peers,
		logger:  logger.With().Str("controller", "bootstrap").Logger(),
	}
}

func (ctrl *BootstrapController) Execute(ctx context.Context) {
	for deviceName, device := range ctrl.config.Interfaces {
		if err := ctrl.registerDevice(ctx, deviceName, device.Peers); err != nil {
			ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("failed to register device")
			continue
		}
	}
}

func (ctrl *BootstrapController) registerDevice(ctx context.Context, deviceName string, peers map[string]config.Peer) error {
	if len(peers) == 0 {
		ctrl.logger.Warn().Str("device", deviceName).Msg("Peers list is empty.")
		return nil
	}

	device, err := ctrl.wg.Device(deviceName)
	if err != nil {
		return err
	}

	peerCount := 0
	for _, p := range device.Peers {
		base64PublicKey := base64.StdEncoding.EncodeToString(p.PublicKey[:])
		if name, ok := containsPeer(peers, base64PublicKey); ok {
			peerCount += 1
			ctrl.logger.Info().Str("device", deviceName).Str("peer", name).Str("publicKey", base64PublicKey).Msg("Register Peer")
			peer := entity.NewPeer(
				entity.NewPeerId(device.PublicKey[:], p.PublicKey[:]),
				device.Name,
				p.PublicKey,
			)

			ctrl.peers.Save(ctx, peer)
		}
	}

	if peerCount > 0 {
		ctrl.logger.Info().Str("device", deviceName).Msg("Register Device")
		deviceEntity := entity.NewDevice(
			entity.DeviceId(device.Name),
			device.ListenPort,
			device.PrivateKey[:],
		)

		ctrl.devices.Save(ctx, deviceEntity)
	}

	return nil
}

func containsPeer(m map[string]config.Peer, publicKey string) (string, bool) {
	for k, v := range m {
		if v.PublicKey == publicKey {
			return k, true
		}
	}
	return "", false
}
