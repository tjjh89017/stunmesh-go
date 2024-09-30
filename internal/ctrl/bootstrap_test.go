package ctrl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	mock "github.com/tjjh89017/stunmesh-go/internal/ctrl/mock"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	mockEntity "github.com/tjjh89017/stunmesh-go/internal/entity/mock"
	gomock "go.uber.org/mock/gomock"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestBootstrap_WithError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()
	cfg := &config.Config{
		Interfaces: map[string]config.Interface{
			"wg0": {
				Peers: map[string]config.Peer{
					"test_peer1": {
						PublicKey: "XgPRso34lnrSAx8nJtdj1/zlF7CoNj7B64LPElYdOGs=",
					},
				},
			},
		},
	}
	deviceConfig := config.NewDeviceConfig(cfg)
	mockPeerSearcher := mockEntity.NewMockPeerSearcher(mockCtrl)
	peerFilterService := entity.NewFilterPeerService(mockPeerSearcher, deviceConfig)

	mockWgClient.EXPECT().Device("wg0").Return(nil, errors.New("device not found"))

	bootstrap := ctrl.NewBootstrapController(
		mockWgClient,
		cfg,
		mockDevices,
		mockPeers,
		&logger,
		peerFilterService,
	)

	bootstrap.Execute(context.TODO())
}

func TestBootstrap_WithMultipleInterfaces(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()
	cfg := &config.Config{
		Interfaces: map[string]config.Interface{
			"wg0": {
				Peers: map[string]config.Peer{
					"test_peer1": {
						PublicKey: "XgPRso34lnrSAx8nJtdj1/zlF7CoNj7B64LPElYdOGs=",
					},
				},
			},
			"wg1": {
				Peers: map[string]config.Peer{
					"test_peer2": {
						PublicKey: "FQ9/2l8t4xmQQbs6SB03+Lh2VijJX74rxRUOv7YT03k=",
					},
					"test_peer3": {
						PublicKey: "Cud5HogJJLCppoUuHnWrSvEJuI49D01sQcfiD3Y9RRU=",
					},
				},
			},
		},
	}
	mockPeerSearcher := mockEntity.NewMockPeerSearcher(mockCtrl)
	deviceConfig := config.NewDeviceConfig(cfg)
	peerFilterService := entity.NewFilterPeerService(mockPeerSearcher, deviceConfig)

	mockDevice0 := &wgtypes.Device{
		Name:       "wg0",
		ListenPort: 51820,
		PrivateKey: [32]byte{},
		Peers: []wgtypes.Peer{
			{
				PublicKey: [32]byte{94, 3, 209, 178, 141, 248, 150, 122, 210, 3, 31, 39, 38, 215, 99, 215, 252, 229, 23, 176, 168, 54, 62, 193, 235, 130, 207, 18, 86, 29, 56, 107},
			},
		},
	}

	mockDevice1 := &wgtypes.Device{
		Name:       "wg1",
		ListenPort: 51821,
		PrivateKey: [32]byte{},
		Peers: []wgtypes.Peer{
			{
				PublicKey: [32]byte{21, 15, 127, 218, 95, 45, 227, 25, 144, 65, 187, 58, 72, 29, 55, 248, 184, 118, 86, 40, 201, 95, 190, 43, 197, 21, 14, 191, 182, 19, 211, 121},
			},
			{
				PublicKey: [32]byte{10, 231, 121, 30, 136, 9, 36, 176, 169, 166, 133, 46, 30, 117, 171, 74, 241, 9, 184, 142, 61, 15, 77, 108, 65, 199, 226, 15, 118, 61, 69, 21},
			},
		},
	}

	mockWgClient.EXPECT().Device("wg0").Return(mockDevice0, nil)
	mockWgClient.EXPECT().Device("wg1").Return(mockDevice1, nil)
	mockDevices.EXPECT().Save(gomock.Any(), gomock.Any()).Times(2)
	mockPeers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(3)

	mockDevice0Peers := []*entity.Peer{
		entity.NewPeer(
			entity.NewPeerId(mockDevice0.PublicKey[:], mockDevice0.Peers[0].PublicKey[:]),
			mockDevice0.Name,
			mockDevice0.Peers[0].PublicKey,
		),
	}

	mockDevice1Peers := []*entity.Peer{
		entity.NewPeer(
			entity.NewPeerId(mockDevice1.PublicKey[:], mockDevice1.Peers[0].PublicKey[:]),
			mockDevice1.Name,
			mockDevice1.Peers[0].PublicKey,
		),
		entity.NewPeer(
			entity.NewPeerId(mockDevice1.PublicKey[:], mockDevice1.Peers[1].PublicKey[:]),
			mockDevice1.Name,
			mockDevice1.Peers[1].PublicKey,
		),
	}

	mockPeerSearcher.EXPECT().SearchByDevice(gomock.Any(), entity.DeviceId("wg0")).Return(mockDevice0Peers, nil)
	mockPeerSearcher.EXPECT().SearchByDevice(gomock.Any(), entity.DeviceId("wg1")).Return(mockDevice1Peers, nil)

	bootstrap := ctrl.NewBootstrapController(
		mockWgClient,
		cfg,
		mockDevices,
		mockPeers,
		&logger,
		peerFilterService,
	)

	bootstrap.Execute(context.TODO())
}
