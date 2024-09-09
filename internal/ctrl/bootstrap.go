package ctrl

import (
	"context"
	"log"

	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"golang.zx2c4.com/wireguard/wgctrl"
)

type BootstrapController struct {
	wg      *wgctrl.Client
	config  *config.Config
	devices DeviceRepository
	peers   PeerRepository
}

func NewBootstrapController(wg *wgctrl.Client, config *config.Config, devices DeviceRepository, peers PeerRepository) *BootstrapController {
	return &BootstrapController{
		wg:      wg,
		config:  config,
		devices: devices,
		peers:   peers,
	}
}

func (ctrl *BootstrapController) Execute(ctx context.Context) {
	device, err := ctrl.wg.Device(ctrl.config.WireGuard)
	if err != nil {
		log.Panic(err)
		return
	}

	deviceEntity := entity.NewDevice(
		entity.DeviceId(device.Name),
		device.PrivateKey[:],
	)

	ctrl.devices.Save(ctx, deviceEntity)

	for _, p := range device.Peers {
		peer := entity.NewPeer(
			entity.NewPeerId(device.PublicKey[:], p.PublicKey[:]),
			device.Name,
			device.ListenPort,
			p.PublicKey,
		)

		ctrl.peers.Save(context.Background(), peer)
	}
}
