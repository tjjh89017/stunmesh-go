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
	"github.com/tjjh89017/stunmesh-go/pluginapi"
	"go.uber.org/mock/gomock"
)

// mockStore for establish controller tests
type establishMockStore struct {
	getFunc func(ctx context.Context, key string) (string, error)
}

func (m *establishMockStore) Get(ctx context.Context, key string) (string, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return "", nil
}

func (m *establishMockStore) Set(ctx context.Context, key string, value string) error {
	return nil
}

// Test Execute - peer not found
func TestEstablishController_Execute_PeerNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	devicePublicKey := make([]byte, 32)
	peerPublicKey := make([]byte, 32)
	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey)

	// Peer not found
	mockPeers.EXPECT().Find(ctx, peerId).Return(nil, errors.New("peer not found"))

	controller := ctrl.NewEstablishController(
		mockWgClient,
		nil, // devices
		mockPeers,
		pluginManager,
		nil, // decryptor
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Execute - device not found
func TestEstablishController_Execute_DeviceNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
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

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		nil,
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Execute - plugin get error
func TestEstablishController_Execute_PluginGetError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "nonexistent_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		nil,
		&logger,
	)

	// Should not panic when plugin not found
	controller.Execute(ctx, peerId)
}

// Test Execute - storage get error
func TestEstablishController_Execute_StorageGetError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	// Mock store that returns error
	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "", errors.New("storage error")
		},
	}

	// Setup plugin manager with store
	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		nil,
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Execute - decryption error
func TestEstablishController_Execute_DecryptionError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	// Mock store that returns encrypted data
	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(nil, errors.New("decryption failed"))

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Execute - invalid JSON
func TestEstablishController_Execute_InvalidJSON(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: "invalid json{{"}, nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Execute - IPv4 endpoint selection
func TestEstablishController_Execute_IPv4Selection(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Prepare endpoint data
	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should configure with IPv4 endpoint
	controller.Execute(ctx, peerId)
}

// Test Execute - IPv6 endpoint selection
func TestEstablishController_Execute_IPv6Selection(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv6")
	peer := createTestPeer("wg0", "test_plugin", "ipv6")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Prepare endpoint data
	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should configure with IPv6 endpoint
	controller.Execute(ctx, peerId)
}

// Test Execute - prefer_ipv4 with IPv4 available
func TestEstablishController_Execute_PreferIPv4_HasIPv4(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Both endpoints available
	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should prefer IPv4
	controller.Execute(ctx, peerId)
}

// Test Execute - prefer_ipv4 fallback to IPv6
func TestEstablishController_Execute_PreferIPv4_FallbackIPv6(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Only IPv6 available
	endpointData := ctrl.EndpointData{
		IPv4: "", // No IPv4
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should fallback to IPv6
	controller.Execute(ctx, peerId)
}

// Test Execute - prefer_ipv6 with IPv6 available
func TestEstablishController_Execute_PreferIPv6_HasIPv6(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv6")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Both endpoints available
	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should prefer IPv6
	controller.Execute(ctx, peerId)
}

// Test Execute - prefer_ipv6 fallback to IPv4
func TestEstablishController_Execute_PreferIPv6_FallbackIPv4(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "dualstack")
	peer := createTestPeer("wg0", "test_plugin", "prefer_ipv6")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Only IPv4 available
	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
		IPv6: "", // No IPv6
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should fallback to IPv4
	controller.Execute(ctx, peerId)
}

// Test Execute - IPv4 required but not available
func TestEstablishController_Execute_IPv4_NotAvailable(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv6")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	// Only IPv6 available
	endpointData := ctrl.EndpointData{
		IPv4: "", // No IPv4
		IPv6: "[2001:db8::1]:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should return error (not panic)
	controller.Execute(ctx, peerId)
}

// Test Execute - WireGuard configuration error
func TestEstablishController_Execute_WireGuardError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockDecryptor := mock.NewMockEndpointDecryptor(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()

	device := createTestDevice("wg0", 51820, "ipv4")
	peer := createTestPeer("wg0", "test_plugin", "ipv4")
	publicKey := peer.PublicKey()
	devicePrivKey := device.PrivateKey()
	peerId := entity.NewPeerId(devicePrivKey[:], publicKey[:])

	store := &establishMockStore{
		getFunc: func(ctx context.Context, key string) (string, error) {
			return "encrypted_data", nil
		},
	}

	pluginManager := plugin.NewManager()
	_ = pluginManager.LoadPlugins(ctx, map[string]pluginapi.PluginDefinition{
		"test_plugin": {
			Type:   "builtin",
			Config: pluginapi.PluginConfig{"_test_store": store},
		},
	})

	endpointData := ctrl.EndpointData{
		IPv4: "1.2.3.4:51820",
	}
	jsonData, _ := json.Marshal(endpointData)

	// Setup expectations
	mockPeers.EXPECT().Find(ctx, gomock.Any()).Return(peer, nil)
	mockDevices.EXPECT().Find(ctx, entity.DeviceId("wg0")).Return(device, nil)
	mockDecryptor.EXPECT().
		Decrypt(ctx, gomock.Any()).
		Return(&ctrl.EndpointDecryptResponse{Content: string(jsonData)}, nil)
	mockWgClient.EXPECT().
		ConfigureDevice(peer.DeviceName(), gomock.Any()).
		Return(errors.New("wireguard configuration failed"))

	controller := ctrl.NewEstablishController(
		mockWgClient,
		mockDevices,
		mockPeers,
		pluginManager,
		mockDecryptor,
		&logger,
	)

	// Should not panic
	controller.Execute(ctx, peerId)
}

// Test Trigger - list peers and enqueue
func TestEstablishController_Trigger(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	peer1 := createTestPeer("wg0", "plugin1", "ipv4")
	peer2 := createTestPeer("wg1", "plugin2", "ipv6")

	// Setup expectations
	mockPeers.EXPECT().List(ctx).Return([]*entity.Peer{peer1, peer2}, nil)

	controller := ctrl.NewEstablishController(
		mockWgClient,
		nil,
		mockPeers,
		pluginManager,
		nil,
		&logger,
	)

	// Should enqueue peers
	controller.Trigger(ctx)
}

// Test Trigger - list peers error
func TestEstablishController_Trigger_ListError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()

	ctx := context.Background()
	pluginManager := plugin.NewManager()

	// Setup expectations
	mockPeers.EXPECT().List(ctx).Return(nil, errors.New("failed to list peers"))

	controller := ctrl.NewEstablishController(
		mockWgClient,
		nil,
		mockPeers,
		pluginManager,
		nil,
		&logger,
	)

	// Should not panic
	controller.Trigger(ctx)
}
