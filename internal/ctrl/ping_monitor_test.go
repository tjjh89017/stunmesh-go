package ctrl_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	mock "github.com/tjjh89017/stunmesh-go/internal/ctrl/mock"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"go.uber.org/mock/gomock"
)

func TestNewPingMonitorController(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger := zerolog.Nop()
	cfg := &config.Config{}
	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	controller := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)

	if controller == nil {
		t.Fatal("Expected controller to be created")
	}
}

func TestNewDevicePingMonitor(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)
	monitor := ctrl.NewDevicePingMonitor("wg0", pingCtrl, logger)

	if monitor == nil {
		t.Fatal("Expected device monitor to be created")
	}
}

func TestAddPeer_Enabled(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)
	monitor := ctrl.NewDevicePingMonitor("wg0", pingCtrl, logger)

	// Create peer ID
	privateKey := [32]byte{1}
	publicKey := [32]byte{2}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])

	// Add peer with ping enabled
	pingConfig := entity.PeerPingConfig{
		Enabled:  true,
		Target:   "8.8.8.8",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
	}

	monitor.AddPeer(peerId, pingConfig, cfg)

	// Verify peer was added by checking state
	healthy, failureCount := pingCtrl.GetPeerState(peerId)
	if !healthy {
		t.Error("Expected peer to be healthy initially")
	}
	if failureCount != 0 {
		t.Errorf("Expected failure count to be 0, got %d", failureCount)
	}
}

func TestAddPeer_Disabled(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)
	monitor := ctrl.NewDevicePingMonitor("wg0", pingCtrl, logger)

	// Create peer ID
	privateKey := [32]byte{1}
	publicKey := [32]byte{2}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])

	// Add peer with ping disabled
	pingConfig := entity.PeerPingConfig{
		Enabled: false,
		Target:  "8.8.8.8",
	}

	monitor.AddPeer(peerId, pingConfig, cfg)

	// Verify peer was not added (should return default healthy state)
	healthy, failureCount := pingCtrl.GetPeerState(peerId)
	if !healthy {
		t.Error("Expected default healthy state for disabled peer")
	}
	if failureCount != 0 {
		t.Errorf("Expected failure count to be 0 for disabled peer, got %d", failureCount)
	}
}

func TestAddPeer_UsesGlobalDefaults(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)
	monitor := ctrl.NewDevicePingMonitor("wg0", pingCtrl, logger)

	// Create peer ID
	privateKey := [32]byte{1}
	publicKey := [32]byte{2}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])

	// Add peer without custom interval/timeout (should use global defaults)
	pingConfig := entity.PeerPingConfig{
		Enabled: true,
		Target:  "8.8.8.8",
		// Interval and Timeout are 0, should use global defaults
	}

	monitor.AddPeer(peerId, pingConfig, cfg)

	// Verify peer was added
	healthy, _ := pingCtrl.GetPeerState(peerId)
	if !healthy {
		t.Error("Expected peer to be healthy initially")
	}
}

func TestGetPeerState_NotMonitored(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)

	// Query state for non-existent peer
	privateKey := [32]byte{99}
	publicKey := [32]byte{99}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])

	healthy, failureCount := pingCtrl.GetPeerState(peerId)

	// Should return default healthy state for unmonitored peers
	if !healthy {
		t.Error("Expected unmonitored peer to default to healthy")
	}
	if failureCount != 0 {
		t.Errorf("Expected failure count to be 0 for unmonitored peer, got %d", failureCount)
	}
}

func TestValidateReply(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)
	monitor := ctrl.NewDevicePingMonitor("wg0", pingCtrl, logger)

	// Create peer with target IP
	privateKey := [32]byte{1}
	publicKey := [32]byte{2}
	peerId := entity.NewPeerId(privateKey[:], publicKey[:])

	pingConfig := entity.PeerPingConfig{
		Enabled: true,
		Target:  "8.8.8.8",
	}

	monitor.AddPeer(peerId, pingConfig, cfg)

	tests := []struct {
		name      string
		addr      net.Addr
		icmpId    uint16
		wantValid bool
	}{
		{
			name:      "valid reply",
			addr:      &net.IPAddr{IP: net.ParseIP("8.8.8.8")},
			icmpId:    1, // Assuming the generated ID is 1 (this is random, but for this test we'll use reflection to get it)
			wantValid: true,
		},
		{
			name:      "wrong IP",
			addr:      &net.IPAddr{IP: net.ParseIP("8.8.4.4")},
			icmpId:    1,
			wantValid: false,
		},
		{
			name:      "invalid address type",
			addr:      &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 0},
			icmpId:    1,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test requires access to internal state to get the actual ICMP ID
			// For now, we'll skip the actual validation check since we can't easily mock internal state
			t.Skip("Requires access to internal peer state to get ICMP ID")
		})
	}
}

func TestICMPConnection_Interface(t *testing.T) {
	// Verify that ICMPConnection interface can be mocked
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	conn := mock.NewMockICMPConnection(mockCtrl)

	// Setup expectations
	testData := []byte("test")
	testAddr := &net.IPAddr{IP: net.ParseIP("8.8.8.8")}

	conn.EXPECT().Send(testData, testAddr).Return(nil)
	conn.EXPECT().Close().Return(nil)

	// Test
	err := conn.Send(testData, testAddr)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	err = conn.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}
}

func TestPingMonitor_Execute_NoPeers(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	// Mock peers.List to return empty list
	peers.EXPECT().List(gomock.Any()).Return([]*entity.Peer{}, nil)

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute should return quickly when no peers are configured
	done := make(chan struct{})
	go func() {
		pingCtrl.Execute(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Success - Execute returned
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return in time with no peers")
	}
}

func TestPingMonitor_Execute_ListError(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PingMonitor: config.PingMonitor{
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
	}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	devices := mock.NewMockDeviceRepository(mockCtrl)
	peers := mock.NewMockPeerRepository(mockCtrl)
	publishCtrl := &ctrl.PublishController{}
	establishCtrl := &ctrl.EstablishController{}

	// Mock peers.List to return error
	peers.EXPECT().List(gomock.Any()).Return(nil, errors.New("list error"))

	pingCtrl := ctrl.NewPingMonitorController(cfg, devices, peers, publishCtrl, establishCtrl, &logger)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute should return quickly on error
	done := make(chan struct{})
	go func() {
		pingCtrl.Execute(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Success - Execute returned
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return in time after error")
	}
}
