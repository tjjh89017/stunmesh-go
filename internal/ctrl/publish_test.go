package ctrl_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	mock "github.com/tjjh89017/stunmesh-go/internal/ctrl/mock"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	"go.uber.org/mock/gomock"
)

// Helper function to create test device
func createTestDevice(name string, port int, protocol string) *entity.Device {
	return entity.NewDevice(entity.DeviceId(name), port, make([]byte, 32), protocol)
}

// Helper function to create test peer
func createTestPeer(deviceName string, plugin string, protocol string) *entity.Peer {
	devicePublicKey := make([]byte, 32)
	peerPublicKey := [32]byte{}
	copy(peerPublicKey[:], []byte("test_peer_key_12345678901234567"))

	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey[:])
	return entity.NewPeer(peerId, deviceName, peerPublicKey, plugin, protocol, entity.PeerPingConfig{})
}

// Test Execute with device list error
func TestPublishController_Execute_DeviceListError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	// Expect device list to fail
	mockDevices.EXPECT().List(ctx).Return(nil, errors.New("failed to list devices"))

	controller := ctrl.NewPublishController(
		mockDevices,
		nil, // peers not needed for this test
		pluginManager,
		nil, // resolver not needed
		nil, // encryptor not needed
		nil, // deviceConfig not needed
		&logger,
	)

	// Should not panic
	controller.Execute(ctx)
}

// Test Execute with STUN error
func TestPublishController_Execute_STUNError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv4")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("", 0, errors.New("STUN failed"))

	controller := ctrl.NewPublishController(
		mockDevices,
		nil,
		pluginManager,
		mockResolver,
		nil,
		nil,
		&logger,
	)

	// Should continue without panic
	controller.Execute(ctx)
}

// Test Execute with peer list error
func TestPublishController_Execute_PeerListError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv4")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).
		Return(nil, errors.New("failed to list peers"))

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		nil,
		nil,
		&logger,
	)

	// Should continue without panic
	controller.Execute(ctx)
}

// Test Execute with encryption error
func TestPublishController_Execute_EncryptionError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)

	// Expect encryption to fail
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(nil, errors.New("encryption failed"))

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	// Should not panic, just log error and continue
	controller.Execute(ctx)
}

// Test Execute with successful encryption and endpoint data verification
func TestPublishController_Execute_SuccessfulEncryption(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)

	// Verify the JSON content being encrypted
	encryptCalled := false
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
			encryptCalled = true
			// Verify the content is valid JSON
			var endpointData ctrl.EndpointData
			if err := json.Unmarshal([]byte(req.Content), &endpointData); err != nil {
				t.Errorf("Invalid JSON content: %v", err)
			}
			if endpointData.IPv4 != "1.2.3.4:51820" {
				t.Errorf("Unexpected IPv4 endpoint: got %q, want %q", endpointData.IPv4, "1.2.3.4:51820")
			}
			if endpointData.IPv6 != "" {
				t.Errorf("Unexpected IPv6 endpoint: got %q, want empty", endpointData.IPv6)
			}
			return &ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil
		})

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	controller.Execute(ctx)

	if !encryptCalled {
		t.Error("Encrypt was not called")
	}
}

// Test Execute with IPv6
func TestPublishController_Execute_IPv6(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv6")
	peer := createTestPeer("wg0", "test_plugin", "ipv6")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6").
		Return("2001:db8::1", 51820, nil)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
			var endpointData ctrl.EndpointData
			if err := json.Unmarshal([]byte(req.Content), &endpointData); err != nil {
				t.Errorf("Invalid JSON content: %v", err)
			}
			if endpointData.IPv4 != "" {
				t.Errorf("Unexpected IPv4 endpoint: got %q, want empty", endpointData.IPv4)
			}
			if endpointData.IPv6 != "[2001:db8::1]:51820" {
				t.Errorf("Unexpected IPv6 endpoint: got %q, want %q", endpointData.IPv6, "[2001:db8::1]:51820")
			}
			return &ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil
		})

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	controller.Execute(ctx)
}

// Test Execute with dualstack
func TestPublishController_Execute_Dualstack(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv4")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil)

	// Expect both IPv4 and IPv6 resolution
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6").
		Return("2001:db8::1", 51820, nil)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
			var endpointData ctrl.EndpointData
			if err := json.Unmarshal([]byte(req.Content), &endpointData); err != nil {
				t.Errorf("Invalid JSON content: %v", err)
			}
			// Both endpoints should be present
			if endpointData.IPv4 != "1.2.3.4:51820" {
				t.Errorf("Unexpected IPv4 endpoint: got %q, want %q", endpointData.IPv4, "1.2.3.4:51820")
			}
			if endpointData.IPv6 != "[2001:db8::1]:51820" {
				t.Errorf("Unexpected IPv6 endpoint: got %q, want %q", endpointData.IPv6, "[2001:db8::1]:51820")
			}
			return &ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil
		})

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	controller.Execute(ctx)
}

// Test Execute with dualstack - both endpoints fail
func TestPublishController_Execute_Dualstack_BothFail(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "dualstack")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)

	// Both resolutions fail
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("", 0, errors.New("IPv4 failed"))
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6").
		Return("", 0, errors.New("IPv6 failed"))

	controller := ctrl.NewPublishController(
		mockDevices,
		nil,
		pluginManager,
		mockResolver,
		nil,
		nil,
		&logger,
	)

	// Should continue without panic
	controller.Execute(ctx)
}

// Test Execute with multiple devices
func TestPublishController_Execute_MultipleDevices(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	// Create two devices with different protocols
	device1 := createTestDevice("wg0", 51820, "ipv4")
	device2 := createTestDevice("wg1", 51821, "ipv6")

	peer1 := createTestPeer("wg0", "plugin1", "ipv4")
	peer2 := createTestPeer("wg1", "plugin2", "ipv6")

	// Setup expectations
	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device1, device2}, nil)

	// Device 1 (wg0)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer1}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted1"}, nil)

	// Device 2 (wg1)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg1")).Return([]*entity.Peer{peer2}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg1", uint16(51821), "ipv6").
		Return("2001:db8::1", 51821, nil)
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted2"}, nil)

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	// Should process both devices
	controller.Execute(ctx)
}

// Test ExecuteForPeer - peer not found
func TestPublishController_ExecuteForPeer_PeerNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	devicePublicKey := make([]byte, 32)
	peerPublicKey := make([]byte, 32)
	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey)

	// Peer not found
	mockPeers.EXPECT().Find(ctx, peerId).Return(nil, errors.New("peer not found"))

	controller := ctrl.NewPublishController(
		nil,
		mockPeers,
		pluginManager,
		nil,
		nil,
		nil,
		&logger,
	)

	// Should return without panic
	controller.ExecuteForPeer(ctx, peerId)
}

// Test ExecuteForPeer - device not found
func TestPublishController_ExecuteForPeer_DeviceNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	peerId := entity.NewPeerId(make([]byte, 32), publicKey[:])

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).
		Return(nil, errors.New("device not found"))

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		nil,
		nil,
		nil,
		&logger,
	)

	// Should return without panic
	controller.ExecuteForPeer(ctx, peerId)
}

// Test ExecuteForPeer - successful execution
func TestPublishController_ExecuteForPeer_Success(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv4")
	publicKey := peer.PublicKey()
	peerId := entity.NewPeerId(make([]byte, 32), publicKey[:])

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4").
		Return("1.2.3.4", 51820, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6").
		Return("2001:db8::1", 51820, nil)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
			var endpointData ctrl.EndpointData
			if err := json.Unmarshal([]byte(req.Content), &endpointData); err != nil {
				t.Errorf("Invalid JSON content: %v", err)
			}
			// Both endpoints should be present for dualstack
			if endpointData.IPv4 != "1.2.3.4:51820" {
				t.Errorf("Unexpected IPv4 endpoint: %s", endpointData.IPv4)
			}
			if endpointData.IPv6 != "[2001:db8::1]:51820" {
				t.Errorf("Unexpected IPv6 endpoint: %s", endpointData.IPv6)
			}
			return &ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil
		})

	controller := ctrl.NewPublishController(
		mockDevices,
		mockPeers,
		pluginManager,
		mockResolver,
		mockEncryptor,
		nil,
		&logger,
	)

	controller.ExecuteForPeer(ctx, peerId)
}

// Test Trigger - should not panic
func TestPublishController_Trigger(t *testing.T) {
	logger := zerolog.Nop()

	controller := ctrl.NewPublishController(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&logger,
	)

	// Should not panic, call multiple times
	controller.Trigger()
	controller.Trigger()
}

// Test TriggerForPeer - should not panic
func TestPublishController_TriggerForPeer(t *testing.T) {
	logger := zerolog.Nop()

	controller := ctrl.NewPublishController(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&logger,
	)

	devicePublicKey := make([]byte, 32)
	peerPublicKey := make([]byte, 32)
	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey)

	// Should not panic, call multiple times
	controller.TriggerForPeer(peerId)
	controller.TriggerForPeer(peerId)
}
