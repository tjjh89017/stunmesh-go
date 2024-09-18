package ctrl

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"golang.zx2c4.com/wireguard/wgctrl"
)

type BootstrapController struct {
	wg      *wgctrl.Client
	config  *config.Config
	devices DeviceRepository
	peers   PeerRepository
	logger  zerolog.Logger
}

func NewBootstrapController(wg *wgctrl.Client, config *config.Config, devices DeviceRepository, peers PeerRepository, logger *zerolog.Logger) *BootstrapController {
	return &BootstrapController{
		wg:      wg,
		config:  config,
		devices: devices,
		peers:   peers,
		logger:  logger.With().Str("controller", "bootstrap").Logger(),
	}
}

func (ctrl *BootstrapController) Execute(ctx context.Context) {
	device, err := ctrl.wg.Device(ctrl.config.WireGuard)
	if err != nil {
		ctrl.logger.Error().Err(err).Msg("failed to get device")
		return
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

		ctrl.peers.Save(context.Background(), peer)
	}
}
