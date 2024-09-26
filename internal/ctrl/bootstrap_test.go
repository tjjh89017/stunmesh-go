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
			"wg0": {},
			"wg1": {},
		},
	}

	mockDevice0 := &wgtypes.Device{
		Name:       "wg0",
		ListenPort: 51820,
		PrivateKey: [32]byte{},
		Peers: []wgtypes.Peer{
			{
				PublicKey: [32]byte{},
			},
		},
	}

	mockDevice1 := &wgtypes.Device{
		Name:       "wg1",
		ListenPort: 51821,
		PrivateKey: [32]byte{},
		Peers: []wgtypes.Peer{
			{
				PublicKey: [32]byte{},
			},
			{
				PublicKey: [32]byte{},
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
