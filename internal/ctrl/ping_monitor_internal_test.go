package ctrl

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

// White-box tests for the ping-monitor retry/backoff state machine.
// They live in package ctrl (not ctrl_test) so they can read the
// unexported PeerPingState fields directly instead of guessing at
// randomly generated values such as the ICMP ID.

// fakePublisher/fakeEstablisher record TriggerForPeer calls without
// requiring gomock, avoiding an internal_test -> mock -> ctrl import cycle.
type fakePublisher struct {
	triggered []entity.PeerId
}

func (f *fakePublisher) TriggerForPeer(peerId entity.PeerId) {
	f.triggered = append(f.triggered, peerId)
}

type fakeEstablisher struct {
	triggered []entity.PeerId
}

func (f *fakeEstablisher) TriggerForPeer(peerId entity.PeerId) {
	f.triggered = append(f.triggered, peerId)
}

func newTestPingMonitor(t *testing.T, cfg *config.Config) (*PingMonitorController, *DevicePingMonitor, *fakePublisher, *fakeEstablisher) {
	t.Helper()
	logger := zerolog.Nop()
	pub := &fakePublisher{}
	est := &fakeEstablisher{}
	pingCtrl := NewPingMonitorController(cfg, nil, nil, pub, est, &logger)
	monitor := NewDevicePingMonitor("wg0", pingCtrl, logger)
	return pingCtrl, monitor, pub, est
}

func testPeerId(seed byte) entity.PeerId {
	privateKey := [32]byte{seed}
	publicKey := [32]byte{seed + 1}
	return entity.NewPeerId(privateKey[:], publicKey[:])
}

func TestValidateReply_Table(t *testing.T) {
	cfg := &config.Config{}
	_, monitor, _, _ := newTestPingMonitor(t, cfg)

	targetIP := &net.IPAddr{IP: net.ParseIP("8.8.8.8")}
	state := &PeerPingState{
		peerId:   testPeerId(1),
		target:   "8.8.8.8",
		targetIP: targetIP,
		icmpId:   42,
	}

	tests := []struct {
		name      string
		addr      net.Addr
		icmpId    uint16
		wantValid bool
	}{
		{
			name:      "valid reply",
			addr:      &net.IPAddr{IP: net.ParseIP("8.8.8.8")},
			icmpId:    42,
			wantValid: true,
		},
		{
			name:      "wrong IP",
			addr:      &net.IPAddr{IP: net.ParseIP("8.8.4.4")},
			icmpId:    42,
			wantValid: false,
		},
		{
			name:      "wrong ICMP ID",
			addr:      &net.IPAddr{IP: net.ParseIP("8.8.8.8")},
			icmpId:    43,
			wantValid: false,
		},
		{
			name:      "invalid address type",
			addr:      &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 0},
			icmpId:    42,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.validateReply(tt.addr, state, tt.icmpId)
			if got != tt.wantValid {
				t.Errorf("validateReply() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

func TestShouldRetryPublishEstablish(t *testing.T) {
	cfg := &config.Config{}
	_, monitor, _, _ := newTestPingMonitor(t, cfg)
	now := time.Now()

	tests := []struct {
		name  string
		state *PeerPingState
		want  bool
	}{
		{
			name:  "first failure always retries",
			state: &PeerPingState{retryCount: 0},
			want:  true,
		},
		{
			name:  "handed over to refresh cycle never retries",
			state: &PeerPingState{retryCount: 5, handedOverToRefresh: true},
			want:  false,
		},
		{
			name:  "zero next retry time retries immediately",
			state: &PeerPingState{retryCount: 1, nextRetryTime: time.Time{}},
			want:  true,
		},
		{
			name:  "before next retry time does not retry",
			state: &PeerPingState{retryCount: 1, nextRetryTime: now.Add(1 * time.Hour)},
			want:  false,
		},
		{
			name:  "after next retry time retries",
			state: &PeerPingState{retryCount: 1, nextRetryTime: now.Add(-1 * time.Hour)},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.shouldRetryPublishEstablish(tt.state, now)
			if got != tt.want {
				t.Errorf("shouldRetryPublishEstablish() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScheduleNextRetry(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name                  string
		fixedRetries          int
		refreshInterval       time.Duration
		startRetryCount       int
		wantRetryCount        int
		wantHandedOverRefresh bool
		wantNextRetryZero     bool
		wantNextRetryAfter    time.Duration // approximate offset from now, checked when not zero/handed-over
	}{
		{
			name:               "within fixed retry window uses 2s interval",
			fixedRetries:       3,
			refreshInterval:    time.Hour,
			startRetryCount:    0,
			wantRetryCount:     1,
			wantNextRetryAfter: 2 * time.Second,
		},
		{
			name:               "last fixed retry still uses 2s interval",
			fixedRetries:       3,
			refreshInterval:    time.Hour,
			startRetryCount:    2,
			wantRetryCount:     3,
			wantNextRetryAfter: 2 * time.Second,
		},
		{
			name:               "first backoff retry after fixed retries uses base interval",
			fixedRetries:       3,
			refreshInterval:    time.Hour,
			startRetryCount:    3,
			wantRetryCount:     4,
			wantNextRetryAfter: 5 * time.Second,
		},
		{
			name:               "second backoff retry grows by increment",
			fixedRetries:       3,
			refreshInterval:    time.Hour,
			startRetryCount:    4,
			wantRetryCount:     5,
			wantNextRetryAfter: 10 * time.Second,
		},
		{
			name:                  "backoff reaching refresh interval hands over to refresh",
			fixedRetries:          3,
			refreshInterval:       5 * time.Second,
			startRetryCount:       3,
			wantRetryCount:        4,
			wantHandedOverRefresh: true,
			wantNextRetryZero:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				RefreshInterval: tt.refreshInterval,
				PingMonitor: config.PingMonitor{
					FixedRetries: tt.fixedRetries,
				},
			}
			_, monitor, _, _ := newTestPingMonitor(t, cfg)
			state := &PeerPingState{retryCount: tt.startRetryCount}

			monitor.scheduleNextRetry(state, now)

			if state.retryCount != tt.wantRetryCount {
				t.Errorf("retryCount = %d, want %d", state.retryCount, tt.wantRetryCount)
			}
			if !state.lastRetryTime.Equal(now) {
				t.Errorf("lastRetryTime = %v, want %v", state.lastRetryTime, now)
			}
			if state.handedOverToRefresh != tt.wantHandedOverRefresh {
				t.Errorf("handedOverToRefresh = %v, want %v", state.handedOverToRefresh, tt.wantHandedOverRefresh)
			}
			if tt.wantNextRetryZero {
				if !state.nextRetryTime.IsZero() {
					t.Errorf("nextRetryTime = %v, want zero", state.nextRetryTime)
				}
				return
			}
			wantNextRetry := now.Add(tt.wantNextRetryAfter)
			if !state.nextRetryTime.Equal(wantNextRetry) {
				t.Errorf("nextRetryTime = %v, want %v", state.nextRetryTime, wantNextRetry)
			}
		})
	}
}

func TestHandlePingResult_Success(t *testing.T) {
	cfg := &config.Config{
		PingMonitor:     config.PingMonitor{FixedRetries: 3},
		RefreshInterval: time.Hour,
	}
	_, monitor, pub, est := newTestPingMonitor(t, cfg)

	state := &PeerPingState{
		peerId:              testPeerId(1),
		isHealthy:           false,
		failureCount:        4,
		retryCount:          2,
		backoffMultiplier:   3,
		handedOverToRefresh: true,
		nextRetryTime:       time.Now().Add(time.Hour),
		lastSentTime:        time.Now(),
	}

	monitor.handlePingResult(state, true)

	if !state.isHealthy {
		t.Error("expected isHealthy = true after successful ping")
	}
	if state.failureCount != 0 {
		t.Errorf("expected failureCount reset to 0, got %d", state.failureCount)
	}
	if state.retryCount != 0 {
		t.Errorf("expected retryCount reset to 0, got %d", state.retryCount)
	}
	if state.backoffMultiplier != 1 {
		t.Errorf("expected backoffMultiplier reset to 1, got %d", state.backoffMultiplier)
	}
	if state.handedOverToRefresh {
		t.Error("expected handedOverToRefresh reset to false")
	}
	if !state.nextRetryTime.IsZero() {
		t.Errorf("expected nextRetryTime cleared, got %v", state.nextRetryTime)
	}
	if !state.lastSentTime.IsZero() {
		t.Errorf("expected lastSentTime cleared, got %v", state.lastSentTime)
	}
	if len(pub.triggered) != 0 || len(est.triggered) != 0 {
		t.Error("expected no publish/establish triggers on success")
	}
}

func TestHandlePingResult_FirstFailureTriggersPublishEstablish(t *testing.T) {
	cfg := &config.Config{
		PingMonitor:     config.PingMonitor{FixedRetries: 3},
		RefreshInterval: time.Hour,
	}
	_, monitor, pub, est := newTestPingMonitor(t, cfg)

	peerId := testPeerId(1)
	state := &PeerPingState{
		peerId:     peerId,
		isHealthy:  true,
		retryCount: 0,
	}

	monitor.handlePingResult(state, false)

	if state.isHealthy {
		t.Error("expected isHealthy = false after failed ping")
	}
	if state.failureCount != 1 {
		t.Errorf("expected failureCount = 1, got %d", state.failureCount)
	}
	if len(pub.triggered) != 1 || pub.triggered[0] != peerId {
		t.Errorf("expected publish triggered for peer, got %v", pub.triggered)
	}
	if len(est.triggered) != 1 || est.triggered[0] != peerId {
		t.Errorf("expected establish triggered for peer, got %v", est.triggered)
	}
	// scheduleNextRetry should have run as part of the retry path.
	if state.retryCount != 1 {
		t.Errorf("expected retryCount = 1 after scheduling next retry, got %d", state.retryCount)
	}
}

func TestHandlePingResult_FailureNotYetDueForRetry(t *testing.T) {
	cfg := &config.Config{
		PingMonitor:     config.PingMonitor{FixedRetries: 3},
		RefreshInterval: time.Hour,
	}
	_, monitor, pub, est := newTestPingMonitor(t, cfg)

	state := &PeerPingState{
		peerId:        testPeerId(1),
		isHealthy:     false,
		retryCount:    1,
		nextRetryTime: time.Now().Add(1 * time.Hour),
	}

	monitor.handlePingResult(state, false)

	if state.failureCount != 1 {
		t.Errorf("expected failureCount = 1, got %d", state.failureCount)
	}
	if len(pub.triggered) != 0 || len(est.triggered) != 0 {
		t.Error("expected no publish/establish triggers before retry is due")
	}
	// retryCount must stay unchanged since scheduleNextRetry was not invoked.
	if state.retryCount != 1 {
		t.Errorf("expected retryCount unchanged at 1, got %d", state.retryCount)
	}
}

func TestHandlePingResult_HandedOverToRefreshDoesNotRetry(t *testing.T) {
	cfg := &config.Config{
		PingMonitor:     config.PingMonitor{FixedRetries: 3},
		RefreshInterval: time.Hour,
	}
	_, monitor, pub, est := newTestPingMonitor(t, cfg)

	state := &PeerPingState{
		peerId:              testPeerId(1),
		isHealthy:           false,
		retryCount:          10,
		handedOverToRefresh: true,
	}

	monitor.handlePingResult(state, false)

	if len(pub.triggered) != 0 || len(est.triggered) != 0 {
		t.Error("expected no publish/establish triggers once handed over to refresh")
	}
	if state.retryCount != 10 {
		t.Errorf("expected retryCount unchanged at 10, got %d", state.retryCount)
	}
}

// fakePeerRepository is a minimal PeerRepository for Execute tests, avoiding
// an internal_test -> mock -> ctrl import cycle.
type fakePeerRepository struct {
	peers   []*entity.Peer
	listErr error
}

func (f *fakePeerRepository) List(context.Context) ([]*entity.Peer, error) {
	return f.peers, f.listErr
}

func (f *fakePeerRepository) ListByDevice(context.Context, entity.DeviceId) ([]*entity.Peer, error) {
	return f.peers, f.listErr
}

func (f *fakePeerRepository) Find(context.Context, entity.PeerId) (*entity.Peer, error) {
	return nil, entity.ErrPeerNotFound
}

func (f *fakePeerRepository) Save(context.Context, *entity.Peer) {}

// fakeICMPConn's Recv blocks until Close, like a real blocking socket read,
// so tests can prove Execute waits for the reader loop to actually exit
// instead of returning as soon as ctx is cancelled. postCloseDelay simulates
// the time the reader loop takes to notice the read failed and return.
type fakeICMPConn struct {
	closeCh        chan struct{}
	closeOnce      sync.Once
	postCloseDelay time.Duration
	recvCalls      atomic.Int32
}

func newFakeICMPConn(postCloseDelay time.Duration) *fakeICMPConn {
	return &fakeICMPConn{closeCh: make(chan struct{}), postCloseDelay: postCloseDelay}
}

func (c *fakeICMPConn) Send([]byte, net.Addr) error { return nil }

func (c *fakeICMPConn) Recv([]byte, time.Duration) (int, net.Addr, error) {
	c.recvCalls.Add(1)
	<-c.closeCh
	if c.postCloseDelay > 0 {
		time.Sleep(c.postCloseDelay)
	}
	return 0, nil, errors.New("use of closed connection")
}

func (c *fakeICMPConn) Close() error {
	c.closeOnce.Do(func() { close(c.closeCh) })
	return nil
}

func (c *fakeICMPConn) SetReadDeadline(time.Time) error { return nil }

// waitForRecvCall polls until conn's deviceReaderLoop has entered Recv, so
// callers cancel ctx only once the reader goroutine is actually running
// (setting up the monitor and starting the loops happens asynchronously
// relative to the newICMPConn call that a test observes).
func waitForRecvCall(t *testing.T, conn *fakeICMPConn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn.recvCalls.Load() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for deviceReaderLoop to call Recv")
}

func testPingPeer(t *testing.T, target string) *entity.Peer {
	t.Helper()
	privateKey := [32]byte{10}
	publicKey := [32]byte{11}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])
	return entity.NewPeer(peerId, entity.DeviceId("wg0"), entity.PeerPublicKey(publicKey), "plugin1", "ipv4", entity.PeerPingConfig{
		Enabled:  true,
		Target:   target,
		Interval: 10 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	})
}

func TestExecute_WaitsForLoopsBeforeReturning(t *testing.T) {
	origDelay := pingMonitorStartupDelay
	pingMonitorStartupDelay = time.Millisecond
	defer func() { pingMonitorStartupDelay = origDelay }()

	const postCloseDelay = 150 * time.Millisecond
	conn := newFakeICMPConn(postCloseDelay)
	connReady := make(chan struct{})
	origNewConn := newICMPConn
	newICMPConn = func(string) (ICMPConnection, error) {
		close(connReady)
		return conn, nil
	}
	defer func() { newICMPConn = origNewConn }()

	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 10 * time.Millisecond,
			Timeout:  50 * time.Millisecond,
		},
		RefreshInterval: time.Hour,
	}
	peers := &fakePeerRepository{peers: []*entity.Peer{testPingPeer(t, "127.0.0.1")}}
	pingCtrl := NewPingMonitorController(cfg, nil, peers, &fakePublisher{}, &fakeEstablisher{}, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pingCtrl.Execute(ctx)
		close(done)
	}()

	select {
	case <-connReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the ICMP connection to be created")
	}
	waitForRecvCall(t, conn)

	start := time.Now()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return after ctx cancellation")
	}
	elapsed := time.Since(start)

	// If Execute returned without waiting for deviceReaderLoop, elapsed would
	// be far below postCloseDelay since the loop is still sleeping in Recv.
	if elapsed < postCloseDelay/2 {
		t.Errorf("Execute returned after %v, want it to wait at least ~%v for the reader loop to exit", elapsed, postCloseDelay)
	}
	if calls := conn.recvCalls.Load(); calls != 1 {
		t.Errorf("expected exactly one Recv call, got %d (possible hot loop on closed conn)", calls)
	}

	pingCtrl.mu.RLock()
	remaining := len(pingCtrl.deviceMonitors)
	pingCtrl.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("expected deviceMonitors to be cleared after Execute returns, got %d entries", remaining)
	}
}

func TestExecute_SecondRunCreatesFreshConnAndNoStaleLoop(t *testing.T) {
	origDelay := pingMonitorStartupDelay
	pingMonitorStartupDelay = time.Millisecond
	defer func() { pingMonitorStartupDelay = origDelay }()

	connCh := make(chan *fakeICMPConn, 2)
	var connsCreated atomic.Int32
	origNewConn := newICMPConn
	newICMPConn = func(string) (ICMPConnection, error) {
		connsCreated.Add(1)
		c := newFakeICMPConn(0)
		connCh <- c
		return c, nil
	}
	defer func() { newICMPConn = origNewConn }()

	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 10 * time.Millisecond,
			Timeout:  50 * time.Millisecond,
		},
		RefreshInterval: time.Hour,
	}
	peers := &fakePeerRepository{peers: []*entity.Peer{testPingPeer(t, "127.0.0.1")}}
	pingCtrl := NewPingMonitorController(cfg, nil, peers, &fakePublisher{}, &fakeEstablisher{}, &logger)

	runOnce := func() *fakeICMPConn {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			pingCtrl.Execute(ctx)
			close(done)
		}()

		var conn *fakeICMPConn
		select {
		case conn = <-connCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the ICMP connection to be created")
		}
		waitForRecvCall(t, conn)

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Execute did not return after ctx cancellation")
		}
		return conn
	}

	conn1 := runOnce()
	conn2 := runOnce()

	if conn1 == conn2 {
		t.Error("expected the second Execute call to create a fresh ICMP connection")
	}
	if got := connsCreated.Load(); got != 2 {
		t.Errorf("expected newICMPConn to be called twice, got %d", got)
	}
	// conn1's reader loop must have exited with the first Execute call, not
	// been left running to call Recv again against the now-closed conn.
	if calls := conn1.recvCalls.Load(); calls != 1 {
		t.Errorf("stale conn1 was read from again after the first Execute returned: %d Recv calls", calls)
	}

	pingCtrl.mu.RLock()
	remaining := len(pingCtrl.deviceMonitors)
	pingCtrl.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("expected deviceMonitors to be cleared after Execute returns, got %d entries", remaining)
	}
}
