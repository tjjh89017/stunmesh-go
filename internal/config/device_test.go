package config

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func TestInterface_GetProtocol_Valid(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		want     string
	}{
		{"ipv4", "ipv4", "ipv4"},
		{"ipv6", "ipv6", "ipv6"},
		{"dualstack", "dualstack", "dualstack"},
		{"empty (default)", "", "ipv4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := &Interface{
				Protocol: tt.protocol,
			}

			got := iface.GetProtocol()
			if got != tt.want {
				t.Errorf("GetProtocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPeer_GetProtocol_Valid(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		want     string
	}{
		{"ipv4", "ipv4", "ipv4"},
		{"ipv6", "ipv6", "ipv6"},
		{"prefer_ipv4", "prefer_ipv4", "prefer_ipv4"},
		{"prefer_ipv6", "prefer_ipv6", "prefer_ipv6"},
		{"empty (default)", "", "ipv4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := &Peer{
				Protocol: tt.protocol,
			}

			got := peer.GetProtocol()
			if got != tt.want {
				t.Errorf("GetProtocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceConfig_GetInterfaceProtocol(t *testing.T) {
	tests := []struct {
		name       string
		deviceName string
		interfaces Interfaces
		want       string
	}{
		{
			name:       "existing device with ipv4",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{Protocol: "ipv4"},
			},
			want: "ipv4",
		},
		{
			name:       "existing device with ipv6",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{Protocol: "ipv6"},
			},
			want: "ipv6",
		},
		{
			name:       "existing device with dualstack",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{Protocol: "dualstack"},
			},
			want: "dualstack",
		},
		{
			name:       "existing device with empty protocol (default)",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{Protocol: ""},
			},
			want: "ipv4",
		},
		{
			name:       "nonexistent device (default)",
			deviceName: "wg99",
			interfaces: Interfaces{
				"wg0": Interface{Protocol: "ipv6"},
			},
			want: "ipv4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := &DeviceConfig{
				interfaces: tt.interfaces,
			}

			got := dc.GetInterfaceProtocol(tt.deviceName)
			if got != tt.want {
				t.Errorf("GetInterfaceProtocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceConfig_GetConfigPeers(t *testing.T) {
	ctx := context.Background()
	localPublicKey := make([]byte, 32)

	// Valid base64 encoded 32-byte public key
	peerPublicKey1 := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	peerPublicKey2 := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyz123456"))

	tests := []struct {
		name           string
		deviceName     string
		interfaces     Interfaces
		wantPeerCount  int
		wantErr        bool
		checkFirstPeer func(t *testing.T, peers []*entity.Peer)
	}{
		{
			name:       "single peer",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{
						"peer1": {
							PublicKey: peerPublicKey1,
							Plugin:    "test_plugin",
							Protocol:  "ipv4",
						},
					},
				},
			},
			wantPeerCount: 1,
			wantErr:       false,
			checkFirstPeer: func(t *testing.T, peers []*entity.Peer) {
				if peers[0].Plugin() != "test_plugin" {
					t.Errorf("peer plugin = %q, want %q", peers[0].Plugin(), "test_plugin")
				}
				if peers[0].Protocol() != "ipv4" {
					t.Errorf("peer protocol = %q, want %q", peers[0].Protocol(), "ipv4")
				}
			},
		},
		{
			name:       "multiple peers",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{
						"peer1": {
							PublicKey: peerPublicKey1,
							Plugin:    "plugin1",
							Protocol:  "prefer_ipv4",
						},
						"peer2": {
							PublicKey: peerPublicKey2,
							Plugin:    "plugin2",
							Protocol:  "ipv6",
						},
					},
				},
			},
			wantPeerCount: 2,
			wantErr:       false,
		},
		{
			name:       "peer with ping config",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{
						"peer1": {
							PublicKey: peerPublicKey1,
							Plugin:    "test_plugin",
							Protocol:  "ipv4",
							Ping: &PingConfig{
								Enabled:  true,
								Target:   "10.0.0.1",
								Interval: 5 * time.Second,
								Timeout:  2 * time.Second,
							},
						},
					},
				},
			},
			wantPeerCount: 1,
			wantErr:       false,
			checkFirstPeer: func(t *testing.T, peers []*entity.Peer) {
				pingConfig := peers[0].PingConfig()
				if !pingConfig.Enabled {
					t.Error("peer ping should be enabled")
				}
				if pingConfig.Target != "10.0.0.1" {
					t.Errorf("ping target = %q, want %q", pingConfig.Target, "10.0.0.1")
				}
				if pingConfig.Interval != 5*time.Second {
					t.Errorf("ping interval = %v, want 5s", pingConfig.Interval)
				}
			},
		},
		{
			name:       "peer without ping config (default disabled)",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{
						"peer1": {
							PublicKey: peerPublicKey1,
							Plugin:    "test_plugin",
							Protocol:  "ipv4",
							Ping:      nil,
						},
					},
				},
			},
			wantPeerCount: 1,
			wantErr:       false,
			checkFirstPeer: func(t *testing.T, peers []*entity.Peer) {
				pingConfig := peers[0].PingConfig()
				if pingConfig.Enabled {
					t.Error("peer ping should be disabled by default")
				}
			},
		},
		{
			name:       "peer with invalid base64 public key (should skip)",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{
						"peer1": {
							PublicKey: "invalid!!!base64",
							Plugin:    "test_plugin",
						},
						"peer2": {
							PublicKey: peerPublicKey1,
							Plugin:    "test_plugin",
						},
					},
				},
			},
			wantPeerCount: 1, // Only peer2 should be included
			wantErr:       false,
		},
		{
			name:          "nonexistent device",
			deviceName:    "wg99",
			interfaces:    Interfaces{},
			wantPeerCount: 0,
			wantErr:       false,
		},
		{
			name:       "empty peers",
			deviceName: "wg0",
			interfaces: Interfaces{
				"wg0": Interface{
					Peers: map[string]Peer{},
				},
			},
			wantPeerCount: 0,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := &DeviceConfig{
				interfaces: tt.interfaces,
			}

			peers, err := dc.GetConfigPeers(ctx, tt.deviceName, localPublicKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetConfigPeers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(peers) != tt.wantPeerCount {
				t.Errorf("GetConfigPeers() returned %d peers, want %d", len(peers), tt.wantPeerCount)
			}

			if tt.checkFirstPeer != nil && len(peers) > 0 {
				tt.checkFirstPeer(t, peers)
			}
		})
	}
}

func TestDeviceConfig_GetConfigPeers_WithPlugin(t *testing.T) {
	ctx := context.Background()
	localPublicKey := make([]byte, 32)

	peerPublicKey := base64.StdEncoding.EncodeToString(make([]byte, 32))

	dc := &DeviceConfig{
		interfaces: Interfaces{
			"wg0": Interface{
				Peers: map[string]Peer{
					"peer1": {
						PublicKey: peerPublicKey,
						Plugin:    "cloudflare_plugin",
						Protocol:  "prefer_ipv6",
					},
				},
			},
		},
	}

	peers, err := dc.GetConfigPeers(ctx, "wg0", localPublicKey)
	if err != nil {
		t.Fatalf("GetConfigPeers() error = %v", err)
	}

	if len(peers) != 1 {
		t.Fatalf("GetConfigPeers() returned %d peers, want 1", len(peers))
	}

	peer := peers[0]

	if peer.Plugin() != "cloudflare_plugin" {
		t.Errorf("peer.Plugin() = %q, want %q", peer.Plugin(), "cloudflare_plugin")
	}

	if peer.Protocol() != "prefer_ipv6" {
		t.Errorf("peer.Protocol() = %q, want %q", peer.Protocol(), "prefer_ipv6")
	}
}

func TestNewDeviceConfig(t *testing.T) {
	cfg := &Config{
		Interfaces: Interfaces{
			"wg0": Interface{
				Protocol: "ipv4",
			},
		},
	}

	dc := NewDeviceConfig(cfg)

	if dc == nil {
		t.Fatal("NewDeviceConfig() returned nil")
		return
	}

	if dc.interfaces == nil {
		t.Error("DeviceConfig.interfaces is nil")
	}

	if len(dc.interfaces) != 1 {
		t.Errorf("DeviceConfig.interfaces length = %d, want 1", len(dc.interfaces))
	}
}

func TestPeer_GetProtocol_AllValidValues(t *testing.T) {
	validProtocols := []string{"ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6"}

	for _, proto := range validProtocols {
		t.Run(proto, func(t *testing.T) {
			peer := &Peer{Protocol: proto}
			got := peer.GetProtocol()
			if got != proto {
				t.Errorf("GetProtocol() = %q, want %q", got, proto)
			}
		})
	}
}

func TestInterface_GetProtocol_AllValidValues(t *testing.T) {
	validProtocols := []string{"ipv4", "ipv6", "dualstack"}

	for _, proto := range validProtocols {
		t.Run(proto, func(t *testing.T) {
			iface := &Interface{Protocol: proto}
			got := iface.GetProtocol()
			if got != proto {
				t.Errorf("GetProtocol() = %q, want %q", got, proto)
			}
		})
	}
}
