//go:generate mockgen -destination=./mock/mock_peer.go -package=mock_entity . ConfigPeerProvider,DevicePeerChecker
package entity

import "context"

type ConfigPeerProvider interface {
	GetConfigPeers(ctx context.Context, deviceName string, localPublicKey []byte) ([]*Peer, error)
}

type DevicePeerChecker interface {
	GetDevicePeerMap(ctx context.Context, deviceName string) (map[string]bool, error)
}

type FilterPeerService struct {
	configProvider ConfigPeerProvider
	deviceChecker  DevicePeerChecker
}

func NewFilterPeerService(deviceChecker DevicePeerChecker, configProvider ConfigPeerProvider) *FilterPeerService {
	return &FilterPeerService{
		configProvider: configProvider,
		deviceChecker:  deviceChecker,
	}
}

func (svc *FilterPeerService) Execute(ctx context.Context, deviceName DeviceId, publicKey []byte) ([]*Peer, error) {
	// Get all peers from config for this device
	configPeers, err := svc.configProvider.GetConfigPeers(ctx, string(deviceName), publicKey)
	if err != nil {
		return nil, err
	}

	// Get map of peers that exist in the actual WireGuard device
	devicePeerMap, err := svc.deviceChecker.GetDevicePeerMap(ctx, string(deviceName))
	if err != nil {
		return nil, err
	}

	// Filter config peers that exist in the device
	existingPeers := make([]*Peer, 0, len(configPeers))
	for _, peer := range configPeers {
		publicKey := peer.PublicKey()
		if devicePeerMap[string(publicKey[:])] {
			existingPeers = append(existingPeers, peer)
		}
	}

	return existingPeers, nil
}
