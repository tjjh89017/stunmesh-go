package config

import (
	"context"
	"encoding/base64"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type Peer struct {
	Description string `mapstructure:"description"`
	PublicKey   string `mapstructure:"public_key"`
}

type Interface struct {
	Peers map[string]Peer `mapstructure:"peers"`
}
type Interfaces map[string]Interface

var _ entity.PeerAllower = &DeviceConfig{}

type DeviceConfig struct {
	interfaces Interfaces
}

func NewDeviceConfig(config *Config) *DeviceConfig {
	return &DeviceConfig{
		interfaces: config.Interfaces,
	}
}

func (c *DeviceConfig) Allow(ctx context.Context, deviceName string, publicKey []byte, peerId entity.PeerId) bool {
	logger := zerolog.Ctx(ctx)

	device, ok := c.interfaces[deviceName]
	if !ok {
		return false
	}

	for _, peer := range device.Peers {
		peerPublicKey, err := base64.StdEncoding.DecodeString(peer.PublicKey)
		if err != nil {
			logger.Error().Err(err).Str("device", deviceName).Str("public_key", peer.PublicKey).Msg("failed to decode public key")
			continue
		}

		currentPeerId := entity.NewPeerId(publicKey, peerPublicKey)
		if peerId == currentPeerId {
			return true
		}
	}

	return false
}
