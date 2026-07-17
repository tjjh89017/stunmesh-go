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
	Protocol    string      `mapstructure:"protocol"`
	Ping        *PingConfig `mapstructure:"ping"`
}

// GetProtocol returns the peer's protocol, defaulting to "ipv4" for backward compatibility
// Valid values: "ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6"
// Note: Invalid values are caught during config validation in Load()
func (p *Peer) GetProtocol() string {
	if p.Protocol == "" {
		return "ipv4" // Default for backward compatibility
	}
	return p.Protocol
}

type Interface struct {
	Protocol string `mapstructure:"protocol"`
	// ListenInterfaces restricts which underlay interfaces STUN discovery
	// listens on (darwin/bsd only; Linux uses a system-wide raw socket and
	// ignores it). Empty means "all eligible interfaces" -- the default.
	ListenInterfaces []string `mapstructure:"listen_interfaces"`
	// ListenDefaultRoute additionally listens on the default-route interface,
	// resolved per-protocol (darwin/bsd only). Combined with ListenInterfaces
	// as a union; the two are additive, not mutually exclusive.
	ListenDefaultRoute bool            `mapstructure:"listen_default_route"`
	Peers              map[string]Peer `mapstructure:"peers"`
}

// GetProtocol returns the configured protocol, defaulting to "ipv4" for backward compatibility
// Valid values: "ipv4", "ipv6", "dualstack"
// Note: Invalid values are caught during config validation in Load()
func (i *Interface) GetProtocol() string {
	if i.Protocol == "" {
		return "ipv4" // Default for backward compatibility
	}
	return i.Protocol
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

func (c *DeviceConfig) GetInterfaceProtocol(deviceName string) string {
	device, ok := c.interfaces[deviceName]
	if !ok {
		return "ipv4"
	}
	return device.GetProtocol()
}

// GetListenConfig returns the interface's underlay-listen restriction for STUN
// discovery: the explicit interface names and whether to also include the
// default-route interface. A nil/empty list with defaultRoute false means
// "listen on all" -- the zero-breaking default. Only darwin/bsd honor these;
// Linux ignores them (warned about at config load).
func (c *DeviceConfig) GetListenConfig(deviceName string) (interfaces []string, defaultRoute bool) {
	device, ok := c.interfaces[deviceName]
	if !ok {
		return nil, false
	}
	return device.ListenInterfaces, device.ListenDefaultRoute
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

		peer := entity.NewPeer(peerId, deviceName, publicKeyArray, configPeer.Plugin, configPeer.GetProtocol(), pingConfig)
		peers = append(peers, peer)
	}

	return peers, nil
}
