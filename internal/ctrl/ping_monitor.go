package ctrl

import (
	"context"
	"fmt"
	"math/rand"
	"net"
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
	// PingMonitorStartupDelay is the delay before starting ping monitoring to allow system initialization
	PingMonitorStartupDelay = 10 * time.Second
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

type DevicePingMonitor struct {
	deviceName    string
	conn          *icmp.PacketConn
	peerStates    map[entity.PeerId]*PeerPingState
	usedIcmpIds   map[uint16]bool          // Track used ICMP IDs
	icmpIdToPeer  map[uint16]entity.PeerId // Map ICMP ID to peer ID
	controller    *PingMonitorController   // Reference to parent controller
	logger        zerolog.Logger
	mu            sync.RWMutex
}

type PingMonitorController struct {
	config         *config.Config
	devices        DeviceRepository
	peers          PeerRepository
	publishCtrl    *PublishController
	establishCtrl  *EstablishController
	refreshCtrl    *RefreshController
	deviceMonitors map[string]*DevicePingMonitor // deviceName -> monitor
	logger         zerolog.Logger
	mu             sync.RWMutex
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
		config:         config,
		devices:        devices,
		peers:          peers,
		publishCtrl:    publishCtrl,
		establishCtrl:  establishCtrl,
		refreshCtrl:    refreshCtrl,
		deviceMonitors: make(map[string]*DevicePingMonitor),
		logger:         logger.With().Str("controller", "ping_monitor").Logger(),
	}
}

func NewDevicePingMonitor(deviceName string, controller *PingMonitorController, logger zerolog.Logger) *DevicePingMonitor {
	return &DevicePingMonitor{
		deviceName:   deviceName,
		peerStates:   make(map[entity.PeerId]*PeerPingState),
		usedIcmpIds:  make(map[uint16]bool),
		icmpIdToPeer: make(map[uint16]entity.PeerId),
		controller:   controller,
		logger:       logger.With().Str("device", deviceName).Logger(),
	}
}

// createDeviceBoundConnection creates an ICMP connection bound to a specific device
func createDeviceBoundConnection(deviceName string) (*icmp.PacketConn, error) {
	// For now, fallback to regular ICMP connection
	// TODO: Implement proper device binding for different platforms
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, err
	}
	
	// TODO: Add SO_BINDTODEVICE socket option here
	// This requires platform-specific implementation
	_ = deviceName // Suppress unused variable warning
	
	return conn, nil
}

func (c *PingMonitorController) Execute(ctx context.Context) {
	// Wait for system initialization before starting ping monitoring
	c.logger.Info().Dur("delay", PingMonitorStartupDelay).Msg("waiting before starting ping monitor")
	time.Sleep(PingMonitorStartupDelay)
	
	// Get all configured peers from all interfaces
	peers, err := c.peers.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list peers for ping monitoring")
		return
	}

	// Group peers by device name
	devicePeers := make(map[string][]*entity.Peer)
	for _, peer := range peers {
		// Only monitor peers that have ping enabled
		if peer.PingConfig().Enabled && peer.PingConfig().Target != "" {
			deviceName := peer.DeviceName()
			devicePeers[deviceName] = append(devicePeers[deviceName], peer)
			c.logger.Info().
				Str("peer", peer.LocalId()).
				Str("device", deviceName).
				Str("target", peer.PingConfig().Target).
				Msg("added peer for ping monitoring")
		}
	}

	// If no peers to monitor, return
	if len(devicePeers) == 0 {
		c.logger.Info().Msg("no peers configured for ping monitoring")
		return
	}

	// Create device monitors for each device
	c.mu.Lock()
	for deviceName, devicePeerList := range devicePeers {
		monitor := NewDevicePingMonitor(deviceName, c, c.logger)
		
		// Create device-bound ICMP connection
		conn, err := createDeviceBoundConnection(deviceName)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("device", deviceName).
				Str("error_type", getErrorType(err)).
				Msg("failed to create device-bound ICMP connection - check if running as root or with CAP_NET_RAW capability")
			continue
		}
		
		monitor.conn = conn
		c.deviceMonitors[deviceName] = monitor
		
		// Add peers to this device monitor
		for _, peer := range devicePeerList {
			monitor.AddPeer(peer.Id(), peer.PingConfig(), c.config)
		}
		
		c.logger.Info().
			Str("device", deviceName).
			Int("peer_count", len(devicePeerList)).
			Msg("created device ping monitor")
	}
	c.mu.Unlock()

	// Start monitoring loops for each device
	for deviceName, monitor := range c.deviceMonitors {
		go monitor.deviceSenderLoop(ctx, c.config.PingMonitor.Interval)
		go monitor.deviceReaderLoop(ctx)
		go monitor.deviceTimeoutChecker(ctx, c.config.PingMonitor.Timeout)
		
		c.logger.Info().
			Str("device", deviceName).
			Msg("started device ping monitoring loops")
	}

	// Wait for context cancellation
	<-ctx.Done()
	
	// Close all device connections
	c.mu.Lock()
	for _, monitor := range c.deviceMonitors {
		if monitor.conn != nil {
			monitor.conn.Close()
		}
	}
	c.mu.Unlock()
}

func (m *DevicePingMonitor) generateUniqueIcmpId() uint16 {
	// Generate a random ICMP ID that's not already in use (1-65535, excluding 0)
	for {
		id := uint16(rand.Intn(65535) + 1) // 1-65535 (non-zero)
		if !m.usedIcmpIds[id] {
			m.usedIcmpIds[id] = true
			return id
		}
	}
}

func (m *DevicePingMonitor) AddPeer(peerId entity.PeerId, pingConfig entity.PeerPingConfig, config *config.Config) {
	if !pingConfig.Enabled || pingConfig.Target == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Use peer-specific config or fall back to global defaults
	interval := pingConfig.Interval
	if interval == 0 {
		interval = config.PingMonitor.Interval
	}

	timeout := pingConfig.Timeout
	if timeout == 0 {
		timeout = config.PingMonitor.Timeout
	}

	// Generate unique ICMP ID for this peer
	icmpId := m.generateUniqueIcmpId()

	// Resolve target IP address
	targetIP, err := net.ResolveIPAddr("ip4", pingConfig.Target)
	if err != nil {
		m.logger.Warn().Err(err).Str("target", pingConfig.Target).Msg("failed to resolve target IP")
		return
	}

	m.peerStates[peerId] = &PeerPingState{
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
	m.icmpIdToPeer[icmpId] = peerId

	m.logger.Info().
		Str("peer", peerId.String()).
		Str("target", pingConfig.Target).
		Dur("interval", interval).
		Dur("timeout", timeout).
		Uint16("icmp_id", icmpId).
		Msg("added peer for ping monitoring")
}

func (m *DevicePingMonitor) deviceSenderLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sendPingsToAllPeers()
		}
	}
}

func (m *DevicePingMonitor) deviceReaderLoop(ctx context.Context) {
	reply := make([]byte, 1500)

	for {
		// Set a short deadline to allow periodic context checking
		_ = m.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		_, addr, err := m.conn.ReadFrom(reply)
		if err != nil {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return
			default:
				// Log detailed error info for non-timeout errors
				if !isTimeoutError(err) {
					m.logger.Debug().
						Err(err).
						Str("error_type", getErrorType(err)).
						Msg("error reading ICMP packet")
				}
				continue
			}
		}

		// Parse and dispatch reply to correct peer
		m.dispatchReply(ctx, reply, addr)
	}
}

func (m *DevicePingMonitor) sendPingsToAllPeers() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.peerStates {
		m.sendPingForPeer(state)
	}
}

func (m *DevicePingMonitor) deviceTimeoutChecker(ctx context.Context, timeout time.Duration) {
	ticker := time.NewTicker(timeout / 2) // Check at half timeout interval
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkForTimeouts(ctx)
		}
	}
}

func (m *DevicePingMonitor) checkForTimeouts(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for _, state := range m.peerStates {
		state.mu.RLock()
		lastSentTime := state.lastSentTime
		timeout := state.timeout
		state.mu.RUnlock()

		// Check if peer has timed out (no reply received within timeout after last sent)
		if !lastSentTime.IsZero() && now.Sub(lastSentTime) > timeout {
			m.logger.Debug().
				Str("peer", state.peerId.String()).
				Str("target", state.target).
				Dur("timeout", timeout).
				Msg("peer ping timed out")

			// Reset lastSentTime to prevent multiple timeout triggers
			state.mu.Lock()
			state.lastSentTime = time.Time{}
			state.mu.Unlock()

			m.handlePingResult(ctx, state, false)
		}
	}
}

func (m *DevicePingMonitor) sendPingForPeer(state *PeerPingState) {
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
		m.logger.Debug().Err(err).Str("target", target).Msg("failed to marshal ICMP message")
		return
	}

	// Send ping using pre-resolved IP
	_, err = m.conn.WriteTo(data, targetIP)
	if err != nil {
		m.logger.Debug().Err(err).Str("target", target).Msg("failed to send ping")
		return
	}

	// Update last sent time
	state.mu.Lock()
	state.lastSentTime = time.Now()
	state.mu.Unlock()

	m.logger.Trace().
		Str("target", target).
		Uint16("icmp_id", icmpId).
		Msg("sent ping")
}

func (m *DevicePingMonitor) dispatchReply(ctx context.Context, reply []byte, addr net.Addr) {
	// Parse ICMP reply (reply starts from ICMP header)
	replyMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply)
	if err != nil {
		m.logger.Debug().
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
		m.logger.Debug().
			Str("message_type", fmt.Sprintf("%T", replyMsg.Body)).
			Interface("icmp_type", replyMsg.Type).
			Int("icmp_code", replyMsg.Code).
			Msg("received non-echo ICMP reply")
		return
	}

	// Find peer by ICMP ID
	icmpId := uint16(replyEcho.ID)
	m.mu.RLock()
	peerId, exists := m.icmpIdToPeer[icmpId]
	if !exists {
		m.mu.RUnlock()
		m.logger.Trace().Uint16("icmp_id", icmpId).Msg("received reply for unknown ICMP ID")
		return
	}

	state, exists := m.peerStates[peerId]
	if !exists {
		m.mu.RUnlock()
		m.logger.Trace().Str("peer", peerId.String()).Msg("received reply for unknown peer")
		return
	}
	m.mu.RUnlock()

	// Validate reply (IP address and ICMP ID match)
	if m.validateReply(addr, state, icmpId) {
		m.logger.Trace().
			Str("peer", state.peerId.String()).
			Str("target", state.target).
			Msg("ping success")
		m.handlePingResult(ctx, state, true)
	}
}

func (m *DevicePingMonitor) validateReply(addr net.Addr, state *PeerPingState, icmpId uint16) bool {
	// Check if source IP matches target IP
	sourceIP, ok := addr.(*net.IPAddr)
	if !ok {
		m.logger.Trace().Str("target", state.target).Msg("invalid address type")
		return false
	}

	// Read immutable fields (no lock needed)
	targetIP := state.targetIP
	expectedIcmpId := state.icmpId

	if !sourceIP.IP.Equal(targetIP.IP) {
		m.logger.Trace().
			Str("target", state.target).
			Str("expected_ip", targetIP.IP.String()).
			Str("got_ip", sourceIP.IP.String()).
			Msg("ping reply IP mismatch")
		return false
	}

	// Verify ICMP ID matches (we already parsed the message in dispatchReply)
	if icmpId != expectedIcmpId {
		m.logger.Trace().
			Str("target", state.target).
			Int("expected_id", int(expectedIcmpId)).
			Int("got_id", int(icmpId)).
			Msg("ping reply ID mismatch")
		return false
	}

	m.logger.Trace().
		Str("target", state.target).
		Int("icmp_id", int(icmpId)).
		Msg("valid ping reply received")
	return true
}

func (m *DevicePingMonitor) handlePingResult(ctx context.Context, state *PeerPingState, success bool) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.lastPingTime = time.Now()
	logger := m.logger.With().Str("peer", state.peerId.String()).Str("target", state.target).Logger()

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
		if m.shouldRetryPublishEstablish(state, now) {
			// Always run publish to update our endpoint, then establish the specific peer
			go m.controller.publishCtrl.ExecuteForPeer(ctx, state.peerId)
			go m.controller.establishCtrl.Execute(ctx, state.peerId)

			if state.retryCount < PeerSpecificRetryThreshold {
				logger.Info().Msg("triggered publish and establish for specific peer (early retry)")
			} else {
				logger.Info().Msg("triggered publish and establish for specific peer (late retry)")
			}

			// Calculate next retry time
			m.scheduleNextRetry(state, now)

			logger.Info().Time("next_retry", state.nextRetryTime).Msg("scheduled next retry")
		} else {
			logger.Debug().Msg("ping failed but not time for retry yet")
		}
	}
}

func (m *DevicePingMonitor) shouldRetryPublishEstablish(state *PeerPingState, now time.Time) bool {
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

func (m *DevicePingMonitor) scheduleNextRetry(state *PeerPingState, now time.Time) {
	state.retryCount++
	state.lastRetryTime = now

	fixedRetries := m.controller.config.PingMonitor.FixedRetries

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
		if backoffInterval >= m.controller.config.RefreshInterval {
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

	// Search through all device monitors for this peer
	for _, monitor := range c.deviceMonitors {
		monitor.mu.RLock()
		if state, exists := monitor.peerStates[peerId]; exists {
			state.mu.RLock()
			healthy := state.isHealthy
			failureCount := state.failureCount
			state.mu.RUnlock()
			monitor.mu.RUnlock()
			return healthy, failureCount
		}
		monitor.mu.RUnlock()
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
