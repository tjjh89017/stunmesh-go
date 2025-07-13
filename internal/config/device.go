package config

import (
	"context"
	"encoding/base64"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type Peer struct {
	Description string `mapstructure:"description"`
	PublicKey   string `mapstructure:"public_key"`
	Plugin      string `mapstructure:"plugin"`
}

type Interface struct {
	Peers map[string]Peer `mapstructure:"peers"`
}
type Interfaces map[string]Interface

var _ entity.ConfigPeerProvider = &DeviceConfig{}

type DeviceConfig struct {
	interfaces Interfaces
}

func NewDeviceConfig(config *Config) *DeviceConfig {
	return &DeviceConfig{
		interfaces: config.Interfaces,
	}
}

func (c *DeviceConfig) GetConfigPeers(ctx context.Context, deviceName string, localPublicKey []byte) ([]*entity.Peer, error) {
	device, ok := c.interfaces[deviceName]
	if !ok {
		return []*entity.Peer{}, nil
	}

	peers := make([]*entity.Peer, 0, len(device.Peers))
	for _, configPeer := range device.Peers {
		peerPublicKey, err := base64.StdEncoding.DecodeString(configPeer.PublicKey)
		if err != nil {
			continue
		}

		var publicKeyArray [32]byte
		copy(publicKeyArray[:], peerPublicKey)

		peerId := entity.NewPeerId(localPublicKey, peerPublicKey)
		peer := entity.NewPeer(peerId, deviceName, publicKeyArray, configPeer.Plugin)
		peers = append(peers, peer)
	}

	return peers, nil
}

