package ctrl

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type BootstrapController struct {
	wg            WireGuardClient
	config        *config.Config
	devices       DeviceRepository
	peers         PeerRepository
	logger        zerolog.Logger
	filterService *entity.FilterPeerService
}

func NewBootstrapController(wg WireGuardClient, config *config.Config, devices DeviceRepository, peers PeerRepository, logger *zerolog.Logger, filterService *entity.FilterPeerService) *BootstrapController {
	return &BootstrapController{
		wg:            wg,
		config:        config,
		devices:       devices,
		peers:         peers,
		logger:        logger.With().Str("controller", "bootstrap").Logger(),
		filterService: filterService,
	}
}

func (ctrl *BootstrapController) Execute(ctx context.Context) {
	for deviceName := range ctrl.config.Interfaces {
		if err := ctrl.registerDevice(ctx, deviceName); err != nil {
			ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("failed to register device")
			continue
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

	allowPeers, err := ctrl.filterService.Execute(ctx, deviceEntity.Name(), device.PublicKey[:])
	if err != nil {
		ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("failed to filter allowed peers")
		return err
	}

	isAnyPeerAllowed := len(allowPeers) > 0
	if !isAnyPeerAllowed {
		ctrl.logger.Warn().Str("device", deviceName).Msg("no peer is allowed")
		return nil
	}

	ctrl.devices.Save(ctx, deviceEntity)
	for _, peer := range allowPeers {
		ctrl.peers.Save(ctx, peer)
	}

	return nil
}
