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
	"github.com/tjjh89017/stunmesh-go/internal/plugin/registry"
	"github.com/tjjh89017/stunmesh-go/pluginapi"
	"go.uber.org/mock/gomock"
)

// fakeDedupStore is a minimal pluginapi.Store used to observe how many
// times Set/Get are invoked, without needing to fake encryption.
type fakeDedupStore struct {
	setCalls int
	getCalls int

	// setErr, if non-nil, is returned by the next Set call and then cleared,
	// so it simulates a single transient storage failure without affecting
	// subsequent calls.
	setErr error
}

func (f *fakeDedupStore) Get(ctx context.Context, key string) (string, error) {
	f.getCalls++
	return "", nil
}

func (f *fakeDedupStore) Set(ctx context.Context, key string, value string) error {
	f.setCalls++
	if f.setErr != nil {
		err := f.setErr
		f.setErr = nil
		return err
	}
	return nil
}

// newDedupTestManager builds a real *plugin.Manager with a single "builtin"
// plugin instance named "test_plugin" backed by a fakeDedupStore, with
// dedup configured as requested. Using the real Manager (instead of a mock)
// exercises the actual IsDedup() lookup path.
func newDedupTestManager(t *testing.T, builtinName string, dedup bool) (*plugin.Manager, *fakeDedupStore) {
	t.Helper()

	store := &fakeDedupStore{}
	registry.Register(builtinName, func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return store, nil
	})

	m := plugin.NewManager()
	err := m.LoadPlugins(context.Background(), map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type: "builtin",
			Config: pluginapi.PluginConfig{
				"name":  builtinName,
				"dedup": dedup,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to load test plugin: %v", err)
	}

	return m, store
}

// Helper function to create test device
func createTestDevice(name string, port int, protocol string) *entity.Device {
	return entity.NewDevice(entity.DeviceId(name), port, make([]byte, 32), protocol, 0)
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
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

// The device's fwmark has to reach the resolver, or the STUN probe leaves
// unmarked and takes a different route than the WireGuard traffic it is
// measuring. gomock fails the exact-value expectation if it does not.
func TestPublishController_Execute_ForwardsDeviceFirewallMark(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	const mark = 0xca6c
	device := entity.NewDevice(entity.DeviceId("wg0"), 51820, make([]byte, 32), "ipv4", mark)

	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil)
	// Exact value, not gomock.Any(): this is the assertion. Erroring out here
	// keeps the test to the one thing it is about.
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4", mark).
		Return("", 0, errors.New("stop after the resolver call"))

	controller := ctrl.NewPublishController(
		mockDevices,
		nil,
		pluginManager,
		mockResolver,
		nil,
		nil,
		&logger,
	)

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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv6", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("", 0, errors.New("IPv4 failed"))
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil)
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted1"}, nil)

	// Device 2 (wg1)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg1")).Return([]*entity.Peer{peer2}, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg1", uint16(51821), "ipv6", gomock.Any()).
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
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil)
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv6", gomock.Any()).
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

// Test Execute with dedup ON and a changed endpoint - should publish every time
func TestPublishController_Execute_Dedup_ChangedEndpoint_Publishes(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_changed", true)

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil).Times(2)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil).Times(2)

	// Two calls, two different resolved endpoints.
	gomock.InOrder(
		mockResolver.EXPECT().
			Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
			Return("1.2.3.4", 51820, nil),
		mockResolver.EXPECT().
			Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
			Return("5.6.7.8", 51820, nil),
	)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(2)

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
	controller.Execute(ctx)

	if store.setCalls != 2 {
		t.Errorf("store.Set call count = %d, want 2 (endpoint changed between calls)", store.setCalls)
	}
}

// Test Execute with dedup ON and an unchanged endpoint - second publish should be skipped
func TestPublishController_Execute_Dedup_UnchangedEndpoint_Skips(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_unchanged", true)

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil).Times(2)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil).Times(2)

	// Same endpoint resolved both times.
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil).
		Times(2)

	// Encrypt must only happen on the first (non-duplicate) publish.
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(1)

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
	controller.Execute(ctx)

	if store.setCalls != 1 {
		t.Errorf("store.Set call count = %d, want 1 (second publish should be deduped)", store.setCalls)
	}
}

// Test Execute with dedup OFF (default) and an unchanged endpoint - should publish every time
func TestPublishController_Execute_Dedup_OffByDefault_AlwaysPublishes(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_off", false)

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil).Times(2)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil).Times(2)

	// Same endpoint resolved both times.
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil).
		Times(2)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(2)

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
	controller.Execute(ctx)

	if store.setCalls != 2 {
		t.Errorf("store.Set call count = %d, want 2 (dedup disabled, default behavior unchanged)", store.setCalls)
	}
}

// Test ExecuteForPeer with dedup ON and an unchanged endpoint - second call
// should skip both encryption and storage.
func TestPublishController_ExecuteForPeer_Dedup_UnchangedEndpoint_Skips(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_peer_unchanged", true)

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	peerId := entity.NewPeerId(make([]byte, 32), publicKey[:])

	mockPeers.EXPECT().Find(ctx, peerId).Return(peer, nil).Times(2)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil).Times(2)

	// Same endpoint resolved both times.
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil).
		Times(2)

	// Encrypt must only happen on the first (non-duplicate) publish; a
	// second Encrypt call would fail the test via gomock's unmet/overrun
	// expectation check.
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(1)

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
	controller.ExecuteForPeer(ctx, peerId)

	if store.setCalls != 1 {
		t.Errorf("store.Set call count = %d, want 1 (second publish should be deduped)", store.setCalls)
	}
}

// Test ExecuteForPeer with dedup ON and a changed endpoint - should publish
// every time.
func TestPublishController_ExecuteForPeer_Dedup_ChangedEndpoint_Publishes(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_peer_changed", true)

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	peerId := entity.NewPeerId(make([]byte, 32), publicKey[:])

	mockPeers.EXPECT().Find(ctx, peerId).Return(peer, nil).Times(2)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil).Times(2)

	// Two calls, two different resolved endpoints.
	gomock.InOrder(
		mockResolver.EXPECT().
			Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
			Return("1.2.3.4", 51820, nil),
		mockResolver.EXPECT().
			Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
			Return("5.6.7.8", 51820, nil),
	)

	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(2)

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
	controller.ExecuteForPeer(ctx, peerId)

	if store.setCalls != 2 {
		t.Errorf("store.Set call count = %d, want 2 (endpoint changed between calls)", store.setCalls)
	}
}

// Test that a failed store.Set is not cached as a successful publish, so a
// retry with the identical endpoint still attempts to store again instead
// of being deduped.
func TestPublishController_Execute_Dedup_FailedStore_DoesNotCacheAndRetries(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockResolver := mock.NewMockStunResolver(mockCtrl)
	mockEncryptor := mock.NewMockEndpointEncryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager, store := newDedupTestManager(t, "dedup_test_failed_store", true)
	// The first Set call fails; the second (retry) succeeds.
	store.setErr = errors.New("store unavailable")

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")

	mockDevices.EXPECT().List(ctx).Return([]*entity.Device{device}, nil).Times(2)
	mockPeers.EXPECT().ListByDevice(ctx, entity.DeviceId("wg0")).Return([]*entity.Peer{peer}, nil).Times(2)

	// Same endpoint resolved both times.
	mockResolver.EXPECT().
		Resolve(ctx, "wg0", uint16(51820), "ipv4", gomock.Any()).
		Return("1.2.3.4", 51820, nil).
		Times(2)

	// Encrypt runs on both attempts: since the first store.Set failed,
	// lastPublished was never updated, so the second Execute call (with the
	// identical endpoint) is not deduped and retries the full publish.
	mockEncryptor.EXPECT().
		Encrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointEncryptResponse{Data: "encrypted_data"}, nil).
		Times(2)

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
	controller.Execute(ctx)

	if store.setCalls != 2 {
		t.Errorf("store.Set call count = %d, want 2 (failed publish must not be cached as success)", store.setCalls)
	}
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
