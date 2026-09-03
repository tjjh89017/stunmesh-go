package ctrl

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type EstablishController struct {
	wgCtrl        WireGuardClient
	devices       DeviceRepository
	peers         PeerRepository
	pluginManager PluginProvider
	decryptor     EndpointDecryptor
	deviceConfig  DeviceConfigProvider
	logger        zerolog.Logger
	mu            sync.Mutex
	queue         *queue.Queue[entity.PeerId]
}

func NewEstablishController(ctrl WireGuardClient, devices DeviceRepository, peers PeerRepository, pluginManager PluginProvider, decryptor EndpointDecryptor, deviceConfig DeviceConfigProvider, logger *zerolog.Logger) *EstablishController {
	return &EstablishController{
		wgCtrl:        ctrl,
		devices:       devices,
		peers:         peers,
		pluginManager: pluginManager,
		decryptor:     decryptor,
		deviceConfig:  deviceConfig,
		logger:        logger.With().Str("controller", "establish").Logger(),
		queue:         queue.NewBuffered[entity.PeerId](queue.PeerQueueSize),
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

	device, err := c.devices.Find(ctx, peer.DeviceName())
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

	storeCtx := dialer.WithEscape(logger.WithContext(ctx), escapeFor(c.deviceConfig, device))
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
	peerProtocol := peer.Protocol()
	selectedEndpoint, err := SelectEndpoint(endpointData, peerProtocol)
	if err != nil {
		logger.Error().Err(err).Str("protocol", peerProtocol).Msg("failed to select endpoint")
		return
	}
	logger.Debug().Str("endpoint", selectedEndpoint).Str("protocol", peerProtocol).Msg("selected endpoint")

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

	err := c.wgCtrl.UpdatePeerEndpoint(ctx, wg.PeerEndpointUpdate{
		DeviceName: string(peer.DeviceName()),
		PublicKey:  peer.PublicKey(),
		Host:       host,
		Port:       port,
	})
	if err != nil {
		c.logger.Error().Err(err).Str("peer", peer.LocalId()).Str("device", string(peer.DeviceName())).Msg("failed to configure device for peer")
		return err
	}
	c.logger.Debug().Str("peer", peer.LocalId()).Str("device", string(peer.DeviceName())).Msg("device configured for peer")
	return nil
}

// Run starts the worker goroutine that processes establish triggers
func (c *EstablishController) Run(ctx context.Context) {
	c.logger.Info().Msg("establish controller worker started")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("establish controller worker stopped")
			return
		case peerId := <-c.queue.Dequeue():
			c.Execute(ctx, peerId)
		}
	}
}

// Trigger lists all peers and enqueues them for establishment
func (c *EstablishController) Trigger(ctx context.Context) {
	peers, err := c.peers.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list peers")
		return
	}

	enqueued := 0
	for _, peer := range peers {
		if c.queue.TryEnqueue(peer.Id()) {
			enqueued++
		} else {
			c.logger.Warn().Str("peer", peer.Id().PeerPublicKeyString()).Msg("queue full, peer dropped")
		}
	}

	c.logger.Debug().Int("enqueued", enqueued).Int("total", len(peers)).Msg("peers enqueued")
	c.logger.Debug().Int("queue_len", c.queue.Len()).Msg("current queue length")
}

// TriggerForPeer enqueues a specific peer for establishment (non-blocking)
func (c *EstablishController) TriggerForPeer(peerId entity.PeerId) {
	if c.queue.TryEnqueue(peerId) {
		c.logger.Debug().Str("peer", peerId.PeerPublicKeyString()).Msg("establish triggered for peer")
	} else {
		c.logger.Warn().Str("peer", peerId.PeerPublicKeyString()).Msg("establish queue full, dropping trigger for peer")
	}
}

// WaitForCompletion waits until the queue is empty or context is cancelled
func (c *EstablishController) WaitForCompletion(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.queue.Len() == 0 {
				return
			}
		}
	}
}
