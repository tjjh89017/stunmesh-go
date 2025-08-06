package config

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type PingConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Target   string        `mapstructure:"target"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

type Peer struct {
	Description string      `mapstructure:"description"`
	PublicKey   string      `mapstructure:"public_key"`
	Plugin      string      `mapstructure:"plugin"`
	Ping        *PingConfig `mapstructure:"ping"`
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

		// Convert config.PingConfig to entity.PeerPingConfig
		var pingConfig entity.PeerPingConfig
		if configPeer.Ping != nil {
			pingConfig = entity.PeerPingConfig{
				Enabled:  configPeer.Ping.Enabled,
				Target:   configPeer.Ping.Target,
				Interval: configPeer.Ping.Interval,
				Timeout:  configPeer.Ping.Timeout,
			}
		} else {
			// Default to disabled if no ping config provided
			pingConfig = entity.PeerPingConfig{
				Enabled: false,
			}
		}

		peer := entity.NewPeer(peerId, deviceName, publicKeyArray, configPeer.Plugin, pingConfig)
		peers = append(peers, peer)
	}

	return peers, nil
}
