package ctrl_test

import (
	"context"
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
