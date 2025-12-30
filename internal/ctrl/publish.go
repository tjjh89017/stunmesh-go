package ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
)


type DeviceConfigProvider interface {
	GetInterfaceProtocol(deviceName string) string
}

type PublishController struct {
	devices       DeviceRepository
	peers         PeerRepository
	pluginManager *plugin.Manager
	resolver      StunResolver
	encryptor     EndpointEncryptor
	deviceConfig  DeviceConfigProvider
	logger        zerolog.Logger
	triggerQueue  *queue.Queue[struct{}]      // Trigger queue for full publish
	peerQueue     *queue.Queue[entity.PeerId] // Trigger queue for specific peer
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, pluginManager *plugin.Manager, resolver StunResolver, encryptor EndpointEncryptor, deviceConfig DeviceConfigProvider, logger *zerolog.Logger) *PublishController {
	return &PublishController{
		devices:       devices,
		peers:         peers,
		pluginManager: pluginManager,
		resolver:      resolver,
		encryptor:     encryptor,
		deviceConfig:  deviceConfig,
		logger:        logger.With().Str("controller", "publish").Logger(),
		triggerQueue:  queue.NewBuffered[struct{}](queue.TriggerQueueSize), // Buffered trigger queue
		peerQueue:     queue.NewBuffered[entity.PeerId](queue.PeerQueueSize), // Buffered peer queue
	}
}

// discoverEndpoints performs STUN discovery based on device protocol
// Returns IPv4 and IPv6 endpoints, or error if discovery failed
func (c *PublishController) discoverEndpoints(ctx context.Context, device *entity.Device, logger zerolog.Logger) (ipv4Endpoint, ipv6Endpoint string, err error) {
	protocol := device.Protocol()

	// Determine which protocols to resolve
	var resolveIPv4, resolveIPv6 bool
	switch protocol {
	case "ipv4":
		resolveIPv4 = true
	case "ipv6":
		resolveIPv6 = true
	case "dualstack":
		resolveIPv4 = true
		resolveIPv6 = true
	default:
		err := errors.New("unknown protocol: " + protocol)
		logger.Error().Err(err).Msg("invalid protocol")
		return "", "", err
	}

	// Perform IPv4 STUN discovery if needed
	var ipv4Err error
	if resolveIPv4 {
		host, port, err := c.resolver.Resolve(ctx, string(device.Name()), uint16(device.ListenPort()), "ipv4")
		if err != nil {
			ipv4Err = err
		} else {
			ipv4Endpoint = net.JoinHostPort(host, strconv.Itoa(port))
			logger.Info().Str("ipv4", ipv4Endpoint).Msg("discovered IPv4 endpoint")
		}
	}

	// Perform IPv6 STUN discovery if needed
	var ipv6Err error
	if resolveIPv6 {
		host, port, err := c.resolver.Resolve(ctx, string(device.Name()), uint16(device.ListenPort()), "ipv6")
		if err != nil {
			ipv6Err = err
		} else {
			ipv6Endpoint = net.JoinHostPort(host, strconv.Itoa(port))
			logger.Info().Str("ipv6", ipv6Endpoint).Msg("discovered IPv6 endpoint")
		}
	}

	// Handle errors based on protocol mode
	switch protocol {
	case "ipv4":
		if ipv4Err != nil {
			logger.Error().Err(ipv4Err).Msg("failed to resolve IPv4 address")
			return "", "", ipv4Err
		}
	case "ipv6":
		if ipv6Err != nil {
			logger.Error().Err(ipv6Err).Msg("failed to resolve IPv6 address")
			return "", "", ipv6Err
		}
	case "dualstack":
		if ipv4Err != nil {
			logger.Warn().Err(ipv4Err).Msg("failed to resolve IPv4 address in dualstack mode")
		}
		if ipv6Err != nil {
			logger.Warn().Err(ipv6Err).Msg("failed to resolve IPv6 address in dualstack mode")
		}
		// If both failed, return error
		if ipv4Endpoint == "" && ipv6Endpoint == "" {
			err := errors.New("both IPv4 and IPv6 STUN discovery failed")
			logger.Error().Err(err).Msg("dualstack discovery failed")
			return "", "", err
		}
	}

	return ipv4Endpoint, ipv6Endpoint, nil
}

func (c *PublishController) Execute(ctx context.Context) {
	devices, err := c.devices.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list devices")
		return
	}

	for _, device := range devices {
		logger := c.logger.With().Str("device", string(device.Name())).Logger()

		// Perform STUN discovery based on device protocol
		ipv4Endpoint, ipv6Endpoint, err := c.discoverEndpoints(ctx, device, logger)
		if err != nil {
			logger.Error().Err(err).Msg("failed to discover endpoints")
			continue
		}

		// Log discovered endpoints
		logger.Info().
			Str("ipv4", ipv4Endpoint).
			Str("ipv6", ipv6Endpoint).
			Msg("discovered endpoints for device")

		peers, err := c.peers.ListByDevice(ctx, device.Name())
		if err != nil {
			logger.Error().Err(err).Msg("failed to list peers")
			continue
		}

		for _, peer := range peers {
			logger := logger.With().Str("peer", peer.LocalId()).Logger()

			// Build endpoint data in plain JSON
			endpointData := EndpointData{
				IPv4: ipv4Endpoint,
				IPv6: ipv6Endpoint,
			}

			jsonPlain, err := json.Marshal(endpointData)
			if err != nil {
				logger.Error().Err(err).Msg("failed to marshal endpoint data")
				continue
			}

			// Encrypt entire JSON content
			res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
				PeerPublicKey: peer.PublicKey(),
				PrivateKey:    device.PrivateKey(),
				Content:       string(jsonPlain),
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

	// Perform STUN discovery based on device protocol
	ipv4Endpoint, ipv6Endpoint, err := c.discoverEndpoints(ctx, device, logger)
	if err != nil {
		logger.Error().Err(err).Msg("failed to discover endpoints for specific peer")
		return
	}

	// Log discovered endpoints
	logger.Info().
		Str("ipv4", ipv4Endpoint).
		Str("ipv6", ipv6Endpoint).
		Msg("discovered endpoints for peer")

	// Build endpoint data in plain JSON
	endpointData := EndpointData{
		IPv4: ipv4Endpoint,
		IPv6: ipv6Endpoint,
	}

	jsonPlain, err := json.Marshal(endpointData)
	if err != nil {
		logger.Error().Err(err).Msg("failed to marshal endpoint data")
		return
	}

	// Encrypt entire JSON content
	res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Content:       string(jsonPlain),
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

// Run starts the worker goroutine that processes publish triggers
func (c *PublishController) Run(ctx context.Context) {
	c.logger.Info().Msg("publish controller worker started")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("publish controller worker stopped")
			return
		case <-c.triggerQueue.Dequeue():
			c.Execute(ctx)
		case peerId := <-c.peerQueue.Dequeue():
			c.ExecuteForPeer(ctx, peerId)
		}
	}
}

// Trigger requests a full publish operation (non-blocking)
func (c *PublishController) Trigger() {
	if c.triggerQueue.TryEnqueue(struct{}{}) {
		c.logger.Debug().Msg("publish triggered")
	} else {
		c.logger.Debug().Msg("publish queue full, skipping trigger")
	}
}

// TriggerForPeer requests a publish operation for a specific peer (non-blocking)
func (c *PublishController) TriggerForPeer(peerId entity.PeerId) {
	if c.peerQueue.TryEnqueue(peerId) {
		c.logger.Debug().Str("peer", peerId.String()).Msg("publish triggered for peer")
	} else {
		c.logger.Warn().Str("peer", peerId.String()).Msg("peer publish queue full, dropping trigger")
	}
}
