//go:generate mockgen -destination=./mock/mock_device_config.go -package=mock_ctrl . DeviceConfigProvider

package ctrl

import (
	"context"
	"encoding/json"
	"net"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

type DeviceConfigProvider interface {
	// GetProxyFib and TunnelInterfaceNames supply what a plugin socket needs
	// to escape a covering tunnel route; see internal/plugin/dialer.
	GetInterfaceProtocol(deviceName string) string
	GetProxyFib(deviceName string) int
	TunnelInterfaceNames() []string
}

type PublishController struct {
	devices       DeviceRepository
	peers         PeerRepository
	pluginManager PluginProvider
	resolver      StunResolver
	encryptor     EndpointEncryptor
	deviceConfig  DeviceConfigProvider
	logger        zerolog.Logger
	triggerQueue  *queue.Queue[struct{}]      // Trigger queue for full publish
	peerQueue     *queue.Queue[entity.PeerId] // Trigger queue for specific peer

	// lastPublished remembers the plaintext endpoint JSON last successfully
	// published for each peer, keyed by peer.LocalId(). It is only read and
	// written from Execute/ExecuteForPeer, both of which are driven
	// sequentially from the single Run() goroutine, so no mutex is needed.
	lastPublished map[string]string
}

func NewPublishController(devices DeviceRepository, peers PeerRepository, pluginManager PluginProvider, resolver StunResolver, encryptor EndpointEncryptor, deviceConfig DeviceConfigProvider, logger *zerolog.Logger) *PublishController {
	return &PublishController{
		devices:       devices,
		peers:         peers,
		pluginManager: pluginManager,
		resolver:      resolver,
		encryptor:     encryptor,
		deviceConfig:  deviceConfig,
		logger:        logger.With().Str("controller", "publish").Logger(),
		triggerQueue:  queue.NewBuffered[struct{}](queue.TriggerQueueSize),   // Buffered trigger queue
		peerQueue:     queue.NewBuffered[entity.PeerId](queue.PeerQueueSize), // Buffered peer queue
		lastPublished: make(map[string]string),
	}
}

// discoverEndpoints performs STUN discovery based on device protocol.
// Returns IPv4 and IPv6 endpoints, or error if discovery failed. The
// resolution and dualstack partial-failure policy live in the shared
// DiscoverEndpoints (internal/ctrl/discover.go); this wraps it with the
// raw-socket StunResolver and this controller's logging.
func (c *PublishController) discoverEndpoints(ctx context.Context, device *entity.Device, logger zerolog.Logger) (ipv4Endpoint, ipv6Endpoint string, err error) {
	resolveFamily := func(family string) FamilyResolver {
		return func(ctx context.Context) (string, error) {
			host, port, err := c.resolver.Resolve(ctx, string(device.Name()), uint16(device.ListenPort()), family, device.FirewallMark())
			if err != nil {
				return "", err
			}
			return net.JoinHostPort(host, strconv.Itoa(port)), nil
		}
	}

	warn := func(family string, ferr error) {
		logger.Warn().Err(ferr).Msg("failed to resolve " + family + " address in dualstack mode")
	}

	ipv4Endpoint, ipv6Endpoint, err = DiscoverEndpoints(ctx, device.Protocol(), warn, resolveFamily("ipv4"), resolveFamily("ipv6"))
	if err != nil {
		logger.Error().Err(err).Msg("failed to discover endpoints")
		return "", "", err
	}

	if ipv4Endpoint != "" {
		logger.Info().Str("ipv4", ipv4Endpoint).Msg("discovered IPv4 endpoint")
	}
	if ipv6Endpoint != "" {
		logger.Info().Str("ipv6", ipv6Endpoint).Msg("discovered IPv6 endpoint")
	}

	return ipv4Endpoint, ipv6Endpoint, nil
}

// publishToPeer builds the endpoint JSON, applies dedup, encrypts and
// stores it for a single peer, and records it in lastPublished on success.
// storeCtx is the context used for the store.Set call (after applying the
// dialer escape); callers pass different bases (Execute keeps the
// peer-scoped logger attached, ExecuteForPeer detaches from cancellation)
// while ctx is used unchanged for encryption.
func (c *PublishController) publishToPeer(ctx, storeCtx context.Context, device *entity.Device, peer *entity.Peer, ipv4Endpoint, ipv6Endpoint string, logger zerolog.Logger) error {
	// Build endpoint data in plain JSON
	endpointData := EndpointData{
		IPv4: ipv4Endpoint,
		IPv6: ipv6Endpoint,
	}

	jsonPlain, err := json.Marshal(endpointData)
	if err != nil {
		logger.Error().Err(err).Msg("failed to marshal endpoint data")
		return err
	}

	// Skip publishing if the plaintext endpoint hasn't changed since
	// the last successful publish for this peer, and the peer's
	// plugin instance has dedup enabled.
	if c.pluginManager.IsDedup(peer.Plugin()) && c.lastPublished[peer.LocalId()] == string(jsonPlain) {
		logger.Debug().Msg("endpoint unchanged, skip publish")
		return nil
	}

	// Encrypt entire JSON content
	res, err := c.encryptor.Encrypt(ctx, &EndpointEncryptRequest{
		PeerPublicKey: peer.PublicKey(),
		PrivateKey:    device.PrivateKey(),
		Content:       string(jsonPlain),
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to encrypt endpoint")
		return err
	}

	store, err := c.pluginManager.GetPlugin(peer.Plugin())
	if err != nil {
		logger.Error().Err(err).Str("plugin", peer.Plugin()).Msg("failed to get plugin")
		return err
	}

	logger.Info().Str("plugin", peer.Plugin()).Msg("store endpoint")
	err = store.Set(dialer.WithEscape(storeCtx, escapeFor(c.deviceConfig, device)), peer.LocalId(), res.Data)
	if err != nil {
		logger.Error().Err(err).Msg("failed to store endpoint")
		return err
	}

	c.lastPublished[peer.LocalId()] = string(jsonPlain)
	return nil
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

			if err := c.publishToPeer(ctx, logger.WithContext(ctx), device, peer, ipv4Endpoint, ipv6Endpoint, logger); err != nil {
				continue
			}
		}
	}
}

func (c *PublishController) ExecuteForPeer(ctx context.Context, peerId entity.PeerId) {
	// Find the specific peer
	peer, err := c.peers.Find(ctx, peerId)
	if err != nil {
		c.logger.Error().Err(err).Str("peer_id", peerId.PeerPublicKeyString()).Msg("failed to find peer")
		return
	}

	// Find the device for this peer
	device, err := c.devices.Find(ctx, peer.DeviceName())
	if err != nil {
		c.logger.Error().Err(err).Str("device", string(peer.DeviceName())).Msg("failed to find device")
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

	if err := c.publishToPeer(ctx, context.WithoutCancel(ctx), device, peer, ipv4Endpoint, ipv6Endpoint, logger); err != nil {
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
		c.logger.Debug().Str("peer", peerId.PeerPublicKeyString()).Msg("publish triggered for peer")
	} else {
		c.logger.Warn().Str("peer", peerId.PeerPublicKeyString()).Msg("peer publish queue full, dropping trigger")
	}
}

// escapeFor describes how this device's plugin traffic should leave the host.
func escapeFor(deviceConfig DeviceConfigProvider, device *entity.Device) dialer.Escape {
	escape := dialer.Escape{FirewallMark: device.FirewallMark()}
	if deviceConfig == nil {
		return escape
	}
	name := string(device.Name())
	escape.Fib = deviceConfig.GetProxyFib(name)
	escape.TunnelIfaces = routeprobe.NewTunnelInterfaces(deviceConfig.TunnelInterfaceNames()...)
	return escape
}
