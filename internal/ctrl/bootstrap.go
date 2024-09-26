package ctrl

import (
	"context"

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
	for deviceName := range ctrl.config.Interfaces {
		if err := ctrl.registerDevice(ctx, deviceName); err != nil {
			ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("failed to register device")
			continue
		}
	}

	if ctrl.config.WireGuard != "" {
		if err := ctrl.registerDevice(ctx, ctrl.config.WireGuard); err != nil {
			ctrl.logger.Error().Err(err).Str("device", ctrl.config.WireGuard).Msg("failed to register device")
		}
	}
}

func (ctrl *BootstrapController) registerDevice(ctx context.Context, deviceName string) error {
	device, err := ctrl.wg.Device(deviceName)
	if err != nil {
		return err
	}

	deviceEntity := entity.NewDevice(
		entity.DeviceId(device.Name),
		device.ListenPort,
		device.PrivateKey[:],
	)

	ctrl.devices.Save(ctx, deviceEntity)

	for _, p := range device.Peers {
		peer := entity.NewPeer(
			entity.NewPeerId(device.PublicKey[:], p.PublicKey[:]),
			device.Name,
			p.PublicKey,
		)

		ctrl.peers.Save(ctx, peer)
	}

	return nil
}
