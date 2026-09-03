package ctrl

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type BootstrapController struct {
	wg            WireGuardClient
	config        *config.Config
	deviceConfig  *config.DeviceConfig
	devices       DeviceRepository
	peers         PeerRepository
	logger        zerolog.Logger
	filterService *entity.FilterPeerService
}

func NewBootstrapController(wg WireGuardClient, config *config.Config, deviceConfig *config.DeviceConfig, devices DeviceRepository, peers PeerRepository, logger *zerolog.Logger, filterService *entity.FilterPeerService) *BootstrapController {
	return &BootstrapController{
		wg:            wg,
		config:        config,
		deviceConfig:  deviceConfig,
		devices:       devices,
		peers:         peers,
		logger:        logger.With().Str("controller", "bootstrap").Logger(),
		filterService: filterService,
	}
}

// Execute registers every configured device. It returns wg.ErrElevationRequired
// unchanged (wrapped with the device name) if any device needs elevated
// privileges, since that condition is unrecoverable and affects every device;
// callers should treat a non-nil return as fatal. Other per-device errors are
// logged and skipped so remaining devices still get a chance to register.
func (ctrl *BootstrapController) Execute(ctx context.Context) error {
	for deviceName := range ctrl.config.Interfaces {
		if err := ctrl.registerDevice(ctx, deviceName); err != nil {
			if errors.Is(err, wg.ErrElevationRequired) {
				ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("insufficient privileges for the WireGuard device; run stunmesh as Administrator")
				return fmt.Errorf("device %s: %w", deviceName, err)
			}
			ctrl.logger.Error().Err(err).Str("device", deviceName).Msg("failed to register device")
			continue
		}
	}
	return nil
}

func (ctrl *BootstrapController) registerDevice(ctx context.Context, deviceName string) error {
	device, err := ctrl.wg.Device(ctx, deviceName)
	if err != nil {
		return err
	}

	protocol := ctrl.deviceConfig.GetInterfaceProtocol(deviceName)

	deviceEntity := entity.NewDevice(
		entity.DeviceId(device.Name),
		device.ListenPort,
		device.PrivateKey[:],
		protocol,
		device.FirewallMark,
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
