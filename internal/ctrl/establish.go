package ctrl

import (
	"context"
	"net"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type EstablishController struct {
	wgCtrl    *wgctrl.Client
	devices   DeviceRepository
	peers     PeerRepository
	store     plugin.Store
	decryptor EndpointDecryptor
	logger    zerolog.Logger
}

func NewEstablishController(ctrl *wgctrl.Client, devices DeviceRepository, peers PeerRepository, store plugin.Store, decryptor EndpointDecryptor, logger *zerolog.Logger) *EstablishController {
	return &EstablishController{
		wgCtrl:    ctrl,
		devices:   devices,
		peers:     peers,
		store:     store,
		decryptor: decryptor,
		logger:    logger.With().Str("controller", "establish").Logger(),
	}
}

func (c *EstablishController) Execute(ctx context.Context, peerId entity.PeerId) {
	peer, err := c.peers.Find(ctx, peerId)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to find peer")
		return
	}

	device, err := c.devices.Find(ctx, entity.DeviceId(peer.DeviceName()))
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to find device")
		return
	}

	logger := c.logger.With().Str("peer", peer.LocalId()).Str("device", string(device.Name())).Logger()

	storeCtx := logger.WithContext(ctx)
	endpointData, err := c.store.Get(storeCtx, peer.RemoteId())
	if err != nil {
		logger.Warn().Err(err).Msg("endpoint is unavailable or not ready")
		return
	}

	res, err := c.decryptor.Decrypt(ctx, &EndpointDecryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Data:          endpointData,
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to decrypt endpoint")
		return
	}

	err = c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey(),
				UpdateOnly: true,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(res.Host),
					Port: res.Port,
				},
			},
		},
	})

	if err != nil {
		logger.Error().Err(err).Msg("failed to configure device")
		return
	}
}
