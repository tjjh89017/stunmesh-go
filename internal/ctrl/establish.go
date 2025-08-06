package ctrl

import (
	"context"
	"net"
	"strconv"
	"sync"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/plugin"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type EstablishController struct {
	wgCtrl        *wgctrl.Client
	devices       DeviceRepository
	peers         PeerRepository
	pluginManager *plugin.Manager
	decryptor     EndpointDecryptor
	logger        zerolog.Logger
	mu            sync.Mutex
}

func NewEstablishController(ctrl *wgctrl.Client, devices DeviceRepository, peers PeerRepository, pluginManager *plugin.Manager, decryptor EndpointDecryptor, logger *zerolog.Logger) *EstablishController {
	return &EstablishController{
		wgCtrl:        ctrl,
		devices:       devices,
		peers:         peers,
		pluginManager: pluginManager,
		decryptor:     decryptor,
		logger:        logger.With().Str("controller", "establish").Logger(),
	}
}

func (c *EstablishController) Execute(ctx context.Context, peerId entity.PeerId) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	store, err := c.pluginManager.GetPlugin(peer.Plugin())
	if err != nil {
		logger.Error().Err(err).Str("plugin", peer.Plugin()).Msg("failed to get plugin")
		return
	}

	storeCtx := logger.WithContext(ctx)
	endpointData, err := store.Get(storeCtx, peer.RemoteId())
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

	err = c.ConfigureDevice(ctx, peer, res)
	if err != nil {
		logger.Error().Err(err).Msg("failed to configure device")
		return
	}
}

func (c *EstablishController) ConfigureDevice(ctx context.Context, peer *entity.Peer, res *EndpointDecryptResponse) error {
	remoteEndpoint := res.Host + ":" + strconv.FormatInt(int64(res.Port), 10)
	c.logger.Debug().Str("peer", peer.LocalId()).Str("remote", remoteEndpoint).Msg("configuring device for peer")

	err := c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey(),
				UpdateOnly: UpdateOnly,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(res.Host),
					Port: res.Port,
				},
			},
		},
	})
	if err != nil {
		c.logger.Error().Err(err).Str("peer", peer.LocalId()).Str("device", peer.DeviceName()).Msg("failed to configure device for peer")
		return err
	}
	c.logger.Debug().Str("peer", peer.LocalId()).Str("device", peer.DeviceName()).Msg("device configured for peer")
	return nil
}
