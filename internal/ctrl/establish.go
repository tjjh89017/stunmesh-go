package ctrl

import (
	"context"
	"encoding/json"
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
	encryptedData, err := store.Get(storeCtx, peer.RemoteId())
	if err != nil {
		logger.Warn().Err(err).Msg("endpoint is unavailable or not ready")
		return
	}

	// Decrypt entire JSON content
	res, err := c.decryptor.Decrypt(ctx, &EndpointDecryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Data:          encryptedData,
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to decrypt endpoint")
		return
	}

	// Parse decrypted JSON
	var endpointData EndpointData
	if err := json.Unmarshal([]byte(res.Content), &endpointData); err != nil {
		logger.Error().Err(err).Msg("failed to unmarshal endpoint data")
		return
	}

	// Log decrypted endpoint data for debugging
	logger.Trace().Str("json", res.Content).Msg("decrypted endpoint data")

	// Select endpoint based on peer protocol
	var selectedEndpoint string
	peerProtocol := peer.Protocol()

	switch peerProtocol {
	case "ipv4":
		selectedEndpoint = endpointData.IPv4
		if selectedEndpoint == "" {
			logger.Error().Msg("IPv4 endpoint not available")
			return
		}
		logger.Debug().Str("endpoint", selectedEndpoint).Msg("using IPv4 endpoint")

	case "ipv6":
		selectedEndpoint = endpointData.IPv6
		if selectedEndpoint == "" {
			logger.Error().Msg("IPv6 endpoint not available")
			return
		}
		logger.Debug().Str("endpoint", selectedEndpoint).Msg("using IPv6 endpoint")

	case "prefer_ipv4":
		// Prefer IPv4, fallback to IPv6
		if endpointData.IPv4 != "" {
			selectedEndpoint = endpointData.IPv4
			logger.Debug().Str("endpoint", selectedEndpoint).Msg("using preferred IPv4 endpoint")
		} else if endpointData.IPv6 != "" {
			selectedEndpoint = endpointData.IPv6
			logger.Warn().Str("endpoint", selectedEndpoint).Msg("IPv4 endpoint unavailable, falling back to IPv6")
		} else {
			logger.Error().Msg("no endpoint available (prefer_ipv4)")
			return
		}

	case "prefer_ipv6":
		// Prefer IPv6, fallback to IPv4
		if endpointData.IPv6 != "" {
			selectedEndpoint = endpointData.IPv6
			logger.Debug().Str("endpoint", selectedEndpoint).Msg("using preferred IPv6 endpoint")
		} else if endpointData.IPv4 != "" {
			selectedEndpoint = endpointData.IPv4
			logger.Warn().Str("endpoint", selectedEndpoint).Msg("IPv6 endpoint unavailable, falling back to IPv4")
		} else {
			logger.Error().Msg("no endpoint available (prefer_ipv6)")
			return
		}

	default:
		// Unknown protocol, log and return
		logger.Error().Str("protocol", peerProtocol).Msg("unknown peer protocol")
		return
	}

	// Parse host:port
	host, portStr, err := net.SplitHostPort(selectedEndpoint)
	if err != nil {
		logger.Error().Err(err).Msg("failed to parse endpoint")
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		logger.Error().Err(err).Msg("failed to parse port")
		return
	}

	err = c.ConfigureDevice(ctx, peer, host, port)
	if err != nil {
		logger.Error().Err(err).Msg("failed to configure device")
		return
	}
}

func (c *EstablishController) ConfigureDevice(ctx context.Context, peer *entity.Peer, host string, port int) error {
	remoteEndpoint := host + ":" + strconv.FormatInt(int64(port), 10)
	c.logger.Debug().Str("peer", peer.LocalId()).Str("remote", remoteEndpoint).Msg("configuring device for peer")

	err := c.wgCtrl.ConfigureDevice(peer.DeviceName(), wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey(),
				UpdateOnly: UpdateOnly,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(host),
					Port: port,
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
