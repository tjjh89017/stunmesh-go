package ctrl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	mock "github.com/tjjh89017/stunmesh-go/internal/ctrl/mock"
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
	config := &config.Config{
		WireGuard: "wg0",
	}

	mockWgClient.EXPECT().Device("wg0").Return(nil, errors.New("device not found"))

	bootstrap := ctrl.NewBootstrapController(
		mockWgClient,
		config,
		mockDevices,
		mockPeers,
		&logger,
	)

	bootstrap.Execute(context.TODO())
}

func TestBootstrap_WithSingleInterface(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)
	mockDevices := mock.NewMockDeviceRepository(mockCtrl)
	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	logger := zerolog.Nop()
	config := &config.Config{
		WireGuard: "wg0",
	}

	mockDevice := &wgtypes.Device{
		Name:       "wg0",
		ListenPort: 51820,
		PrivateKey: [32]byte{},
		Peers: []wgtypes.Peer{
			{
				PublicKey: [32]byte{},
			},
		},
	}

	mockWgClient.EXPECT().Device("wg0").Return(mockDevice, nil)
	mockDevices.EXPECT().Save(gomock.Any(), gomock.Any())
	mockPeers.EXPECT().Save(gomock.Any(), gomock.Any())

	bootstrap := ctrl.NewBootstrapController(
		mockWgClient,
		config,
		mockDevices,
		mockPeers,
		&logger,
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
	config := &config.Config{
		Interfaces: map[string]config.Interface{
			"wg0": config.Interface{
				Peers: map[string]config.Peer{
					"test_peer1": config.Peer{
						PublicKey: "XgPRso34lnrSAx8nJtdj1/zlF7CoNj7B64LPElYdOGs=",
					},
				},
			},
			"wg1": config.Interface{
				Peers: map[string]config.Peer{
					"test_peer2": config.Peer{
						PublicKey: "FQ9/2l8t4xmQQbs6SB03+Lh2VijJX74rxRUOv7YT03k=",
					},
					"test_peer3": config.Peer{
						PublicKey: "Cud5HogJJLCppoUuHnWrSvEJuI49D01sQcfiD3Y9RRU=",
					},
				},
			},
		},
	}

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

	bootstrap := ctrl.NewBootstrapController(
		mockWgClient,
		config,
		mockDevices,
		mockPeers,
		&logger,
	)

	bootstrap.Execute(context.TODO())
}
