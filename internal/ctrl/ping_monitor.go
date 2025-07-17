package ctrl

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"reflect"
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
	targetIP     *net.IPAddr // Resolved target IP address
	interval     time.Duration
	timeout      time.Duration
	isHealthy    bool
	failureCount int
	lastPingTime time.Time
	lastSentTime time.Time // Track when last ping was sent

	// Ping identification
	icmpId uint16 // ICMP ID for this peer

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
	usedIcmpIds   map[uint16]bool          // Track used ICMP IDs
	icmpIdToPeer  map[uint16]entity.PeerId // Map ICMP ID to peer ID
	globalConn    *icmp.PacketConn         // Single global ICMP connection
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
		icmpIdToPeer:  make(map[uint16]entity.PeerId),
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

	// Add peers to monitoring
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

	// If no peers to monitor, return
	if len(c.peerStates) == 0 {
		c.logger.Info().Msg("no peers configured for ping monitoring")
		return
	}

	// Create global ICMP connection
	c.logger.Info().
		Str("protocol", "ip4:icmp").
		Str("address", "0.0.0.0").
		Int("uid", os.Getuid()).
		Int("gid", os.Getgid()).
		Msg("attempting to create global ICMP connection")

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("protocol", "ip4:icmp").
			Str("address", "0.0.0.0").
			Int("uid", os.Getuid()).
			Int("gid", os.Getgid()).
			Str("error_type", getErrorType(err)).
			Msg("failed to create global ICMP connection - check if running as root or with CAP_NET_RAW capability")
		return
	}

	c.logger.Info().Msg("successfully created global ICMP connection")
	defer conn.Close()

	c.globalConn = conn

	// Start global sender, reader, and timeout checker loops
	go c.globalSenderLoop(ctx, conn)
	go c.globalReaderLoop(ctx, conn)
	go c.timeoutChecker(ctx)

	// Wait for context cancellation
	<-ctx.Done()
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

	// Resolve target IP address
	targetIP, err := net.ResolveIPAddr("ip4", pingConfig.Target)
	if err != nil {
		c.logger.Warn().Err(err).Str("target", pingConfig.Target).Msg("failed to resolve target IP")
		return
	}

	c.peerStates[peerId] = &PeerPingState{
		peerId:              peerId,
		target:              pingConfig.Target,
		targetIP:            targetIP,
		interval:            interval,
		timeout:             timeout,
		isHealthy:           true, // Assume healthy initially
		failureCount:        0,
		retryCount:          0,
		backoffMultiplier:   1,
		handedOverToRefresh: false,
		icmpId:              icmpId,
	}

	// Map ICMP ID to peer ID for lookup
	c.icmpIdToPeer[icmpId] = peerId

	c.logger.Info().
		Str("peer", peerId.String()).
		Str("target", pingConfig.Target).
		Dur("interval", interval).
		Dur("timeout", timeout).
		Uint16("icmp_id", icmpId).
		Msg("added peer for ping monitoring")
}

func (c *PingMonitorController) globalSenderLoop(ctx context.Context, conn *icmp.PacketConn) {
	ticker := time.NewTicker(c.config.PingMonitor.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendPingsToAllPeers(conn)
		}
	}
}

func (c *PingMonitorController) globalReaderLoop(ctx context.Context, conn *icmp.PacketConn) {
	reply := make([]byte, 1500)

	for {
		// Set a short deadline to allow periodic context checking
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		_, addr, err := conn.ReadFrom(reply)
		if err != nil {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return
			default:
				// Log detailed error info for non-timeout errors
				if !isTimeoutError(err) {
					c.logger.Debug().
						Err(err).
						Str("error_type", getErrorType(err)).
						Msg("error reading ICMP packet")
				}
				continue
			}
		}

		// Parse and dispatch reply to correct peer
		c.dispatchReply(ctx, reply, addr)
	}
}

func (c *PingMonitorController) sendPingsToAllPeers(conn *icmp.PacketConn) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, state := range c.peerStates {
		c.sendPingForPeer(state, conn)
	}
}

func (c *PingMonitorController) timeoutChecker(ctx context.Context) {
	ticker := time.NewTicker(c.config.PingMonitor.Timeout / 2) // Check at half timeout interval
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkForTimeouts(ctx)
		}
	}
}

func (c *PingMonitorController) checkForTimeouts(ctx context.Context) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	for _, state := range c.peerStates {
		state.mu.RLock()
		lastSentTime := state.lastSentTime
		timeout := state.timeout
		state.mu.RUnlock()

		// Check if peer has timed out (no reply received within timeout after last sent)
		if !lastSentTime.IsZero() && now.Sub(lastSentTime) > timeout {
			c.logger.Debug().
				Str("peer", state.peerId.String()).
				Str("target", state.target).
				Dur("timeout", timeout).
				Msg("peer ping timed out")

			// Reset lastSentTime to prevent multiple timeout triggers
			state.mu.Lock()
			state.lastSentTime = time.Time{}
			state.mu.Unlock()

			c.handlePingResult(ctx, state, false)
		}
	}
}

func (c *PingMonitorController) sendPingForPeer(state *PeerPingState, conn *icmp.PacketConn) {
	// Get state values (no need for mutex since these don't change)
	icmpId := state.icmpId
	target := state.target
	targetIP := state.targetIP

	// Create ICMP message with fixed sequence number
	message := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(icmpId),
			Seq:  1, // Fixed sequence number since we don't check it
			Data: []byte("stunmesh-ping"),
		},
	}

	data, err := message.Marshal(nil)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to marshal ICMP message")
		return
	}

	// Send ping using pre-resolved IP
	_, err = conn.WriteTo(data, targetIP)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", target).Msg("failed to send ping")
		return
	}

	// Update last sent time
	state.mu.Lock()
	state.lastSentTime = time.Now()
	state.mu.Unlock()

	/*
		c.logger.Debug().
			Str("target", target).
			Uint16("icmp_id", icmpId).
			Msg("sent ping")
	*/
}

func (c *PingMonitorController) dispatchReply(ctx context.Context, reply []byte, addr net.Addr) {
	// Parse ICMP reply (reply starts from ICMP header)
	replyMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply)
	if err != nil {
		c.logger.Debug().
			Err(err).
			Int("reply_length", len(reply)).
			Str("reply_hex", fmt.Sprintf("%x", reply[:min(len(reply), 64)])).
			Str("source_addr", addr.String()).
			Msg("failed to parse ICMP reply")
		return
	}

	// Check if it's an echo reply
	replyEcho, ok := replyMsg.Body.(*icmp.Echo)
	if !ok {
		c.logger.Debug().
			Str("message_type", fmt.Sprintf("%T", replyMsg.Body)).
			Interface("icmp_type", replyMsg.Type).
			Int("icmp_code", replyMsg.Code).
			Msg("received non-echo ICMP reply")
		return
	}

	// Find peer by ICMP ID
	icmpId := uint16(replyEcho.ID)
	c.mu.RLock()
	peerId, exists := c.icmpIdToPeer[icmpId]
	if !exists {
		c.mu.RUnlock()
		c.logger.Debug().Uint16("icmp_id", icmpId).Msg("received reply for unknown ICMP ID")
		return
	}

	state, exists := c.peerStates[peerId]
	if !exists {
		c.mu.RUnlock()
		c.logger.Debug().Str("peer", peerId.String()).Msg("received reply for unknown peer")
		return
	}
	c.mu.RUnlock()

	// Validate reply and handle result
	if c.validateReply(reply, addr, state) {
		/*
			c.logger.Debug().
				Str("peer", state.peerId.String()).
				Str("target", state.target).
				Msg("ping success")
		*/
		c.handlePingResult(ctx, state, true)
	}
}

func (c *PingMonitorController) validateReply(reply []byte, addr net.Addr, state *PeerPingState) bool {
	// Check if source IP matches target IP
	sourceIP, ok := addr.(*net.IPAddr)
	if !ok {
		c.logger.Debug().Str("target", state.target).Msg("invalid address type")
		return false
	}

	// Read immutable fields (no lock needed)
	targetIP := state.targetIP
	icmpId := state.icmpId

	if !sourceIP.IP.Equal(targetIP.IP) {
		c.logger.Debug().
			Str("target", state.target).
			Str("expected_ip", targetIP.IP.String()).
			Str("got_ip", sourceIP.IP.String()).
			Msg("ping reply IP mismatch")
		return false
	}

	// Parse the reply to verify it's our packet
	replyMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply)
	if err != nil {
		c.logger.Debug().Err(err).Str("target", state.target).Msg("failed to parse ping reply")
		return false
	}

	// Check if it's an echo reply and verify ID only (ignore sequence)
	if replyEcho, ok := replyMsg.Body.(*icmp.Echo); ok {
		if replyEcho.ID == int(icmpId) {
			c.logger.Debug().
				Str("target", state.target).
				Int("icmp_id", int(icmpId)).
				Int("got_seq", replyEcho.Seq).
				Msg("valid ping reply received")
			return true
		} else {
			c.logger.Debug().
				Str("target", state.target).
				Int("expected_id", int(icmpId)).
				Int("got_id", replyEcho.ID).
				Msg("ping reply ID mismatch")
		}
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
		state.lastSentTime = time.Time{}  // Clear sent time to prevent timeout
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

// Helper functions for error debugging
func isTimeoutError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

func getErrorType(err error) string {
	if err == nil {
		return "none"
	}
	return reflect.TypeOf(err).String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
