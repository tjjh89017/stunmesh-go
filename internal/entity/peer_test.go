package entity_test

import (
	"testing"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func TestNewPeer(t *testing.T) {
	devicePrivKey := [32]byte{1}
	peerPubKey := [32]byte{2}
	peerId := entity.NewPeerId(devicePrivKey[:], peerPubKey[:])

	deviceName := "wg0"
	plugin := "cloudflare"
	protocol := "ipv4"
	pingConfig := entity.PeerPingConfig{
		Enabled:  true,
		Target:   "8.8.8.8",
		Interval: 5 * time.Second,
		Timeout:  2 * time.Second,
	}

	peer := entity.NewPeer(peerId, deviceName, peerPubKey, plugin, protocol, pingConfig)

	if peer == nil {
		t.Fatal("Expected peer to be created")
	}

	if peer.Id() != peerId {
		t.Error("Expected peer ID to match")
	}

	if peer.DeviceName() != deviceName {
		t.Errorf("Expected device name %s, got %s", deviceName, peer.DeviceName())
	}

	if peer.Plugin() != plugin {
		t.Errorf("Expected plugin %s, got %s", plugin, peer.Plugin())
	}

	if peer.Protocol() != protocol {
		t.Errorf("Expected protocol %s, got %s", protocol, peer.Protocol())
	}
}

func TestPeer_Id(t *testing.T) {
	devicePrivKey := [32]byte{1}
	peerPubKey := [32]byte{2}
	peerId := entity.NewPeerId(devicePrivKey[:], peerPubKey[:])

	peer := entity.NewPeer(peerId, "wg0", peerPubKey, "test", "ipv4", entity.PeerPingConfig{})

	if peer.Id() != peerId {
		t.Error("Expected peer ID to match")
	}
}

func TestPeer_LocalId(t *testing.T) {
	devicePrivKey := [32]byte{1}
	peerPubKey := [32]byte{2}
	peerId := entity.NewPeerId(devicePrivKey[:], peerPubKey[:])

	peer := entity.NewPeer(peerId, "wg0", peerPubKey, "test", "ipv4", entity.PeerPingConfig{})

	localId := peer.LocalId()
	expectedLocalId := peerId.EndpointKey()

	if localId != expectedLocalId {
		t.Errorf("Expected local ID %s, got %s", expectedLocalId, localId)
	}
}

func TestPeer_RemoteId(t *testing.T) {
	devicePrivKey := [32]byte{1}
	peerPubKey := [32]byte{2}
	peerId := entity.NewPeerId(devicePrivKey[:], peerPubKey[:])

	peer := entity.NewPeer(peerId, "wg0", peerPubKey, "test", "ipv4", entity.PeerPingConfig{})

	remoteId := peer.RemoteId()
	expectedRemoteId := peerId.RemoteEndpointKey()

	if remoteId != expectedRemoteId {
		t.Errorf("Expected remote ID %s, got %s", expectedRemoteId, remoteId)
	}
}

func TestPeer_DeviceName(t *testing.T) {
	tests := []struct {
		name       string
		deviceName string
	}{
		{"simple name", "wg0"},
		{"multiple digits", "wg10"},
		{"with dash", "wg-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerId := entity.NewPeerId([]byte{0}, []byte{1})
			peer := entity.NewPeer(peerId, tt.deviceName, [32]byte{}, "test", "ipv4", entity.PeerPingConfig{})

			if peer.DeviceName() != tt.deviceName {
				t.Errorf("Expected device name %s, got %s", tt.deviceName, peer.DeviceName())
			}
		})
	}
}

func TestPeer_PublicKey(t *testing.T) {
	peerId := entity.NewPeerId([]byte{0}, []byte{1})
	publicKey := [32]byte{3}
	for i := 0; i < 32; i++ {
		publicKey[i] = byte(i)
	}

	peer := entity.NewPeer(peerId, "wg0", publicKey, "test", "ipv4", entity.PeerPingConfig{})
	retrievedKey := peer.PublicKey()

	// Verify key is returned correctly
	for i := 0; i < 32; i++ {
		if retrievedKey[i] != byte(i) {
			t.Errorf("Expected key byte %d to be %d, got %d", i, i, retrievedKey[i])
		}
	}
}

func TestPeer_Plugin(t *testing.T) {
	tests := []struct {
		name   string
		plugin string
	}{
		{"cloudflare", "cloudflare"},
		{"exec plugin", "exec_plugin"},
		{"shell plugin", "shell_plugin"},
		{"custom", "my-custom-plugin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerId := entity.NewPeerId([]byte{0}, []byte{1})
			peer := entity.NewPeer(peerId, "wg0", [32]byte{}, tt.plugin, "ipv4", entity.PeerPingConfig{})

			if peer.Plugin() != tt.plugin {
				t.Errorf("Expected plugin %s, got %s", tt.plugin, peer.Plugin())
			}
		})
	}
}

func TestPeer_Protocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
	}{
		{"ipv4", "ipv4"},
		{"ipv6", "ipv6"},
		{"prefer_ipv4", "prefer_ipv4"},
		{"prefer_ipv6", "prefer_ipv6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerId := entity.NewPeerId([]byte{0}, []byte{1})
			peer := entity.NewPeer(peerId, "wg0", [32]byte{}, "test", tt.protocol, entity.PeerPingConfig{})

			if peer.Protocol() != tt.protocol {
				t.Errorf("Expected protocol %s, got %s", tt.protocol, peer.Protocol())
			}
		})
	}
}

func TestPeer_PingConfig(t *testing.T) {
	peerId := entity.NewPeerId([]byte{0}, []byte{1})

	tests := []struct {
		name       string
		pingConfig entity.PeerPingConfig
	}{
		{
			name: "enabled with custom settings",
			pingConfig: entity.PeerPingConfig{
				Enabled:  true,
				Target:   "8.8.8.8",
				Interval: 5 * time.Second,
				Timeout:  2 * time.Second,
			},
		},
		{
			name: "disabled",
			pingConfig: entity.PeerPingConfig{
				Enabled: false,
			},
		},
		{
			name: "enabled with different target",
			pingConfig: entity.PeerPingConfig{
				Enabled:  true,
				Target:   "1.1.1.1",
				Interval: 10 * time.Second,
				Timeout:  5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := entity.NewPeer(peerId, "wg0", [32]byte{}, "test", "ipv4", tt.pingConfig)

			config := peer.PingConfig()

			if config.Enabled != tt.pingConfig.Enabled {
				t.Errorf("Expected enabled %v, got %v", tt.pingConfig.Enabled, config.Enabled)
			}

			if config.Target != tt.pingConfig.Target {
				t.Errorf("Expected target %s, got %s", tt.pingConfig.Target, config.Target)
			}

			if config.Interval != tt.pingConfig.Interval {
				t.Errorf("Expected interval %v, got %v", tt.pingConfig.Interval, config.Interval)
			}

			if config.Timeout != tt.pingConfig.Timeout {
				t.Errorf("Expected timeout %v, got %v", tt.pingConfig.Timeout, config.Timeout)
			}
		})
	}
}

func TestErrPeerNotFound(t *testing.T) {
	err := entity.ErrPeerNotFound

	if err == nil {
		t.Fatal("ErrPeerNotFound should not be nil")
	}

	if err.Error() == "" {
		t.Error("ErrPeerNotFound should have non-empty error message")
	}

	expectedMsg := "peer not found"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
}

func TestPeerPingConfig_DefaultValues(t *testing.T) {
	// Test zero values
	config := entity.PeerPingConfig{}

	if config.Enabled {
		t.Error("Expected default enabled to be false")
	}

	if config.Target != "" {
		t.Errorf("Expected default target to be empty, got %s", config.Target)
	}

	if config.Interval != 0 {
		t.Errorf("Expected default interval to be 0, got %v", config.Interval)
	}

	if config.Timeout != 0 {
		t.Errorf("Expected default timeout to be 0, got %v", config.Timeout)
	}
}
