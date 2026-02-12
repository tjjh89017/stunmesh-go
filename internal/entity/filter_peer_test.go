package entity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	mock "github.com/tjjh89017/stunmesh-go/internal/entity/mock"
	"go.uber.org/mock/gomock"
)

func TestNewFilterPeerService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)

	if service == nil {
		t.Fatal("Expected service to be created")
	}
}

func TestFilterPeerService_Execute_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Create test peers
	peerId1 := entity.NewPeerId([]byte{0}, []byte{1})
	peerId2 := entity.NewPeerId([]byte{0}, []byte{2})
	peerId3 := entity.NewPeerId([]byte{0}, []byte{3})

	pubKey1 := [32]byte{1}
	pubKey2 := [32]byte{2}
	pubKey3 := [32]byte{3}

	configPeers := []*entity.Peer{
		entity.NewPeer(peerId1, "wg0", pubKey1, "test", "ipv4", entity.PeerPingConfig{}),
		entity.NewPeer(peerId2, "wg0", pubKey2, "test", "ipv4", entity.PeerPingConfig{}),
		entity.NewPeer(peerId3, "wg0", pubKey3, "test", "ipv4", entity.PeerPingConfig{}),
	}

	// Mock: config returns 3 peers
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return(configPeers, nil)

	// Mock: device has peers 1 and 2, but not 3
	devicePeerMap := map[string]bool{
		string(pubKey1[:]): true,
		string(pubKey2[:]): true,
	}
	deviceChecker.EXPECT().
		GetDevicePeerMap(ctx, "wg0").
		Return(devicePeerMap, nil)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return only peers 1 and 2 (that exist in device)
	if len(peers) != 2 {
		t.Errorf("Expected 2 peers, got %d", len(peers))
	}

	// Verify the returned peers are correct
	foundPeer1 := false
	foundPeer2 := false
	for _, peer := range peers {
		if peer.Id() == peerId1 {
			foundPeer1 = true
		}
		if peer.Id() == peerId2 {
			foundPeer2 = true
		}
		if peer.Id() == peerId3 {
			t.Error("Should not include peer3 (not in device)")
		}
	}

	if !foundPeer1 {
		t.Error("Expected to find peer1")
	}
	if !foundPeer2 {
		t.Error("Expected to find peer2")
	}
}

func TestFilterPeerService_Execute_EmptyDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Create test peers
	peerId1 := entity.NewPeerId([]byte{0}, []byte{1})
	pubKey1 := [32]byte{1}

	configPeers := []*entity.Peer{
		entity.NewPeer(peerId1, "wg0", pubKey1, "test", "ipv4", entity.PeerPingConfig{}),
	}

	// Mock: config returns 1 peer
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return(configPeers, nil)

	// Mock: device has no peers
	devicePeerMap := map[string]bool{}
	deviceChecker.EXPECT().
		GetDevicePeerMap(ctx, "wg0").
		Return(devicePeerMap, nil)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return empty list
	if len(peers) != 0 {
		t.Errorf("Expected 0 peers, got %d", len(peers))
	}
}

func TestFilterPeerService_Execute_NoPeersInConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Mock: config returns no peers
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return([]*entity.Peer{}, nil)

	// Mock: device has some peers (doesn't matter since config is empty)
	devicePeerMap := map[string]bool{
		"some_key": true,
	}
	deviceChecker.EXPECT().
		GetDevicePeerMap(ctx, "wg0").
		Return(devicePeerMap, nil)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return empty list
	if len(peers) != 0 {
		t.Errorf("Expected 0 peers, got %d", len(peers))
	}
}

func TestFilterPeerService_Execute_ConfigProviderError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Mock: config returns error
	expectedErr := errors.New("config error")
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return(nil, expectedErr)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	if peers != nil {
		t.Error("Expected peers to be nil on error")
	}
}

func TestFilterPeerService_Execute_DeviceCheckerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Create test peer
	peerId1 := entity.NewPeerId([]byte{0}, []byte{1})
	pubKey1 := [32]byte{1}

	configPeers := []*entity.Peer{
		entity.NewPeer(peerId1, "wg0", pubKey1, "test", "ipv4", entity.PeerPingConfig{}),
	}

	// Mock: config returns peers
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return(configPeers, nil)

	// Mock: device checker returns error
	expectedErr := errors.New("device error")
	deviceChecker.EXPECT().
		GetDevicePeerMap(ctx, "wg0").
		Return(nil, expectedErr)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	if peers != nil {
		t.Error("Expected peers to be nil on error")
	}
}

func TestFilterPeerService_Execute_AllPeersExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	deviceName := entity.DeviceId("wg0")
	publicKey := make([]byte, 32)

	deviceChecker := mock.NewMockDevicePeerChecker(ctrl)
	configProvider := mock.NewMockConfigPeerProvider(ctrl)

	// Create test peers
	peerId1 := entity.NewPeerId([]byte{0}, []byte{1})
	peerId2 := entity.NewPeerId([]byte{0}, []byte{2})

	pubKey1 := [32]byte{1}
	pubKey2 := [32]byte{2}

	configPeers := []*entity.Peer{
		entity.NewPeer(peerId1, "wg0", pubKey1, "test", "ipv4", entity.PeerPingConfig{}),
		entity.NewPeer(peerId2, "wg0", pubKey2, "test", "ipv4", entity.PeerPingConfig{}),
	}

	// Mock: config returns 2 peers
	configProvider.EXPECT().
		GetConfigPeers(ctx, "wg0", publicKey).
		Return(configPeers, nil)

	// Mock: device has both peers
	devicePeerMap := map[string]bool{
		string(pubKey1[:]): true,
		string(pubKey2[:]): true,
	}
	deviceChecker.EXPECT().
		GetDevicePeerMap(ctx, "wg0").
		Return(devicePeerMap, nil)

	service := entity.NewFilterPeerService(deviceChecker, configProvider)
	peers, err := service.Execute(ctx, deviceName, publicKey)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return both peers
	if len(peers) != 2 {
		t.Errorf("Expected 2 peers, got %d", len(peers))
	}
}
