package ctrl

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	// PeerSpecificRetryThreshold defines how many retries to do peer-specific establish before escalating
	PeerSpecificRetryThreshold = 3
)

type PeerPingState struct {
	peerId       entity.PeerId
	target       string
	interval     time.Duration
	timeout      time.Duration
	isHealthy    bool
	failureCount int
	lastPingTime time.Time
	icmpId       uint16 // ICMP ID for this peer

	// Publish/Establish retry tracking (separate from ping)
	lastRetryTime       time.Time
	nextRetryTime       time.Time
	retryCount          int
	backoffMultiplier   int
	handedOverToRefresh bool // True when retries >= refresh_interval, only ping, no publish/establish
	mu                  sync.RWMutex
}

type PingMonitorController struct {
	config        *config.Config
	devices       DeviceRepository
	peers         PeerRepository
	publishCtrl   *PublishController
	establishCtrl *EstablishController
	refreshCtrl   *RefreshController
	peerStates    map[entity.PeerId]*PeerPingState
	usedIcmpIds   map[uint16]bool // Track used ICMP IDs
	logger        zerolog.Logger
	mu            sync.RWMutex
}

func NewPingMonitorController(
	config *config.Config,
	devices DeviceRepository,
	peers PeerRepository,
	publishCtrl *PublishController,
	establishCtrl *EstablishController,
	refreshCtrl *RefreshController,
	logger *zerolog.Logger,
) *PingMonitorController {
	return &PingMonitorController{
		config:        config,
		devices:       devices,
		peers:         peers,
		publishCtrl:   publishCtrl,
		establishCtrl: establishCtrl,
		refreshCtrl:   refreshCtrl,
		peerStates:    make(map[entity.PeerId]*PeerPingState),
		usedIcmpIds:   make(map[uint16]bool),
		logger:        logger.With().Str("controller", "ping_monitor").Logger(),
	}
}

func (c *PingMonitorController) Execute(ctx context.Context) {
	// Get all configured peers from all interfaces

	peers, err := c.peers.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list peers for ping monitoring")
		return
	}

	for _, peer := range peers {
		// Only monitor peers that have ping enabled
		if peer.PingConfig().Enabled && peer.PingConfig().Target != "" {
			c.AddPeer(peer.Id(), peer.PingConfig())
			c.logger.Info().
				Str("peer", peer.LocalId()).
				Str("target", peer.PingConfig().Target).
				Msg("added peer for ping monitoring")
		}
	}
	// Start ping monitoring
	go c.Start(ctx)
}

func (c *PingMonitorController) generateUniqueIcmpId() uint16 {
	// Generate a random ICMP ID that's not already in use
	for {
		id := uint16(rand.Intn(65536)) // 0-65535
		if !c.usedIcmpIds[id] {
			c.usedIcmpIds[id] = true
			return id
		}
	}
}

func (c *PingMonitorController) AddPeer(peerId entity.PeerId, pingConfig entity.PeerPingConfig) {
	if !pingConfig.Enabled || pingConfig.Target == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Use peer-specific config or fall back to global defaults
	interval := pingConfig.Interval
	if interval == 0 {
		interval = c.config.PingMonitor.Interval
	}

	timeout := pingConfig.Timeout
	if timeout == 0 {
		timeout = c.config.PingMonitor.Timeout
	}

	// Generate unique ICMP ID for this peer
	icmpId := c.generateUniqueIcmpId()

	c.peerStates[peerId] = &PeerPingState{
		peerId:              peerId,
		target:              pingConfig.Target,
		interval:            interval,
		timeout:             timeout,
		isHealthy:           true, // Assume healthy initially
		failureCount:        0,
		retryCount:          0,
		backoffMultiplier:   1,
		handedOverToRefresh: false,
		icmpId:              icmpId,
	}

	c.logger.Info().
		Str("peer", peerId.String()).
		Str("target", pingConfig.Target).
		Dur("interval", interval).
		Dur("timeout", timeout).
		Uint16("icmp_id", icmpId).
		Msg("added peer for ping monitoring")
}

func (c *PingMonitorController) Start(ctx context.Context) {
	c.mu.RLock()
	peers := make([]*PeerPingState, 0, len(c.peerStates))
	for _, state := range c.peerStates {
		peers = append(peers, state)
	}
	c.mu.RUnlock()

	// Start monitoring goroutine for each peer
	for _, peerState := range peers {
		go c.monitorPeer(ctx, peerState)
	}
}

func (c *PingMonitorController) monitorPeer(ctx context.Context, state *PeerPingState) {
	ticker := time.NewTicker(state.interval) // Always use configured interval
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Launch ping asynchronously to avoid blocking the ticker
			go func() {
				success := c.pingTarget(ctx, state)
				c.handlePingResult(ctx, state, success)
			}()
		}
	}
}

func (c *PingMonitorController) pingTarget(ctx context.Context, state *PeerPingState) bool {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return false
	default:
	}

	// Get immutable values (no lock needed)
	icmpId := state.icmpId
	target := state.target
	timeout := state.timeout

	// Create ICMP connection
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to create ICMP connection")
		return false
	}
	defer conn.Close()

	// Set timeout based on both context and timeout parameter
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	if err := conn.SetDeadline(deadline); err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to set connection deadline")
		return false
	}

	// Create ICMP message with fixed sequence 0
	message := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(icmpId),
			Seq:  0, // Fixed sequence number
			Data: []byte("stunmesh-ping"),
		},
	}

	data, err := message.Marshal(nil)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to marshal ICMP message")
		return false
	}

	// Send ping
	addr, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to resolve target address")
		return false
	}

	_, err = conn.WriteTo(data, addr)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to send ping")
		return false
	}

	// Wait for reply and validate ICMP ID
	reply := make([]byte, 1500)
	_, _, err = conn.ReadFrom(reply)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to read ping reply")
		return false
	}

	// Parse and validate the reply
	return c.validateReply(reply, icmpId, target)
}

func (c *PingMonitorController) validateReply(reply []byte, expectedId uint16, target string) bool {
	// Parse the reply to verify it's our packet
	replyMsg, err := icmp.ParseMessage(int(ipv4.ICMPTypeEchoReply), reply[20:])
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to parse ICMP reply")
		return false
	}

	// Check if it's an echo reply and verify ID
	if replyEcho, ok := replyMsg.Body.(*icmp.Echo); ok {
		if replyEcho.ID == int(expectedId) {
			c.logger.Debug().
				Str("target", target).
				Uint16("icmp_id", expectedId).
				Int("seq", replyEcho.Seq).
				Msg("valid ping reply received")
			return true
		} else {
			c.logger.Debug().
				Str("target", target).
				Uint16("expected_id", expectedId).
				Int("got_id", replyEcho.ID).
				Msg("ping reply ID mismatch")
		}
	} else {
		c.logger.Debug().Str("target", target).Msg("received non-echo ICMP reply")
	}

	return false
}

func (c *PingMonitorController) handlePingResult(ctx context.Context, state *PeerPingState, success bool) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.lastPingTime = time.Now()
	logger := c.logger.With().Str("peer", state.peerId.String()).Str("target", state.target).Logger()

	if success {
		// Ping succeeded
		if !state.isHealthy {
			logger.Info().Msg("peer tunnel recovered")
		}

		// Reset state to healthy
		state.isHealthy = true
		state.failureCount = 0
		state.retryCount = 0
		state.backoffMultiplier = 1
		state.handedOverToRefresh = false // Re-enable publish/establish on recovery
		state.nextRetryTime = time.Time{} // Clear retry time
	} else {
		// Ping failed
		state.isHealthy = false
		state.failureCount++

		logger.Warn().Int("failure_count", state.failureCount).Msg("peer ping failed")

		// Check if we should trigger publish/establish based on retry logic
		now := time.Now()
		if c.shouldRetryPublishEstablish(state, now) {
			// Always run publish to update our endpoint, then establish the specific peer
			go c.publishCtrl.ExecuteForPeer(ctx, state.peerId)
			go c.establishCtrl.Execute(ctx, state.peerId)

			if state.retryCount < PeerSpecificRetryThreshold {
				logger.Info().Msg("triggered publish and establish for specific peer (early retry)")
			} else {
				logger.Info().Msg("triggered publish and establish for specific peer (late retry)")
			}

			// Calculate next retry time
			c.scheduleNextRetry(state, now)

			logger.Info().Time("next_retry", state.nextRetryTime).Msg("scheduled next retry")
		} else {
			logger.Debug().Msg("ping failed but not time for retry yet")
		}
	}
}

func (c *PingMonitorController) shouldRetryPublishEstablish(state *PeerPingState, now time.Time) bool {
	// First failure - always retry immediately
	if state.retryCount == 0 {
		return true
	}

	// If handed over to refresh cycle, don't retry
	if state.handedOverToRefresh {
		return false
	}

	// Check if enough time has passed since last retry
	return now.After(state.nextRetryTime) || state.nextRetryTime.IsZero()
}

func (c *PingMonitorController) scheduleNextRetry(state *PeerPingState, now time.Time) {
	state.retryCount++
	state.lastRetryTime = now

	fixedRetries := c.config.PingMonitor.FixedRetries

	if state.retryCount <= fixedRetries {
		// Use fixed interval for first few retries (fast recovery)
		state.nextRetryTime = now.Add(time.Duration(2) * time.Second)
	} else {
		// Use Arithmetic backoff after fixed retries
		const baseInterval = 5 // seconds
		const increment = 5    // seconds

		retryAfterfixed := state.retryCount - fixedRetries
		intervalSeconds := baseInterval + (retryAfterfixed-1)*increment
		backoffInterval := time.Duration(intervalSeconds) * time.Second
		if backoffInterval >= c.config.RefreshInterval {
			// Hand over to refresh cycle - no more retries
			state.handedOverToRefresh = true
			state.nextRetryTime = time.Time{} // Clear retry time
		} else {
			state.nextRetryTime = now.Add(backoffInterval)
		}
	}
}

func (c *PingMonitorController) GetPeerState(peerId entity.PeerId) (bool, int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if state, exists := c.peerStates[peerId]; exists {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.isHealthy, state.failureCount
	}

	return true, 0 // Default to healthy if not monitored
}
