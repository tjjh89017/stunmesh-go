//go:build mobile

package mobile

import (
	"encoding/base64"
	"strings"
	"testing"
)

// validKeyB64 returns a syntactically valid base64-encoded 32-byte WG key.
func validKeyB64(fill byte) string {
	var raw [32]byte
	for i := range raw {
		raw[i] = fill
	}
	return base64.StdEncoding.EncodeToString(raw[:])
}

func minimalConfigJSON(privateKey, peerPublicKey string) string {
	return `{
		"name": "wg0",
		"interface": {"private_key": "` + privateKey + `"},
		"peers": [{"name": "peer1", "public_key": "` + peerPublicKey + `"}]
	}`
}

func TestParseConfig_ValidMinimal(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)

	cfg, err := parseConfig(minimalConfigJSON(priv, pub))
	if err != nil {
		t.Fatalf("parseConfig returned error for valid config: %v", err)
	}
	if cfg.Interface.PrivateKey != priv {
		t.Errorf("private key = %q, want %q", cfg.Interface.PrivateKey, priv)
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != pub {
		t.Errorf("peers = %+v, want one peer with public key %q", cfg.Peers, pub)
	}
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	_, err := parseConfig("{not valid json")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestParseConfig_MissingPrivateKey(t *testing.T) {
	_, err := parseConfig(`{"interface": {}, "peers": []}`)
	if err == nil {
		t.Fatal("expected error when interface.private_key is missing")
	}
	if !strings.Contains(err.Error(), "private_key") {
		t.Errorf("error = %v, want mention of private_key", err)
	}
}

func TestParseConfig_InvalidPrivateKey(t *testing.T) {
	_, err := parseConfig(minimalConfigJSON("not-base64!!", validKeyB64(2)))
	if err == nil {
		t.Fatal("expected error for malformed private key")
	}
}

func TestParseConfig_WrongLengthPrivateKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := parseConfig(minimalConfigJSON(short, validKeyB64(2)))
	if err == nil {
		t.Fatal("expected error for wrong-length private key")
	}
}

func TestParseConfig_InvalidPeerPublicKey(t *testing.T) {
	_, err := parseConfig(minimalConfigJSON(validKeyB64(1), "garbage"))
	if err == nil {
		t.Fatal("expected error for malformed peer public key")
	}
	if !strings.Contains(err.Error(), "peer 0 public_key") {
		t.Errorf("error = %v, want mention of peer index and field", err)
	}
}

func TestParseConfig_InvalidPeerPresharedKey(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `"},
		"peers": [{"public_key": "` + pub + `", "preshared_key": "bad"}]
	}`
	_, err := parseConfig(json)
	if err == nil {
		t.Fatal("expected error for malformed preshared key")
	}
	if !strings.Contains(err.Error(), "preshared_key") {
		t.Errorf("error = %v, want mention of preshared_key", err)
	}
}

func TestParseConfig_EmptyPeerPresharedKeyAllowed(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `"},
		"peers": [{"public_key": "` + pub + `", "preshared_key": ""}]
	}`
	cfg, err := parseConfig(json)
	if err != nil {
		t.Fatalf("parseConfig returned error for empty (absent) preshared key: %v", err)
	}
	if cfg.Peers[0].PresharedKey != "" {
		t.Errorf("preshared key = %q, want empty", cfg.Peers[0].PresharedKey)
	}
}

func TestParseConfig_ProtocolDefaultsToIPv4(t *testing.T) {
	cfg, err := parseConfig(minimalConfigJSON(validKeyB64(1), validKeyB64(2)))
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Interface.Protocol != "ipv4" {
		t.Errorf("Interface.Protocol = %q, want default %q", cfg.Interface.Protocol, "ipv4")
	}
}

func TestParseConfig_ProtocolPreservedWhenSet(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `", "protocol": "dualstack"},
		"peers": [{"public_key": "` + pub + `"}]
	}`
	cfg, err := parseConfig(json)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Interface.Protocol != "dualstack" {
		t.Errorf("Interface.Protocol = %q, want %q", cfg.Interface.Protocol, "dualstack")
	}
}

// parseConfig whitelists interface/peer protocol strings, mirroring
// internal/config's GetInterfaceProtocol/GetProtocol.
func TestParseConfig_UnrecognizedInterfaceProtocolRejected(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `", "protocol": "not-a-real-protocol"},
		"peers": [{"public_key": "` + pub + `"}]
	}`
	if _, err := parseConfig(json); err == nil {
		t.Fatal("parseConfig returned no error for unrecognized interface protocol")
	}
}

func TestParseConfig_UnrecognizedPeerProtocolRejected(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `"},
		"peers": [{"public_key": "` + pub + `", "protocol": "also-not-real"}]
	}`
	if _, err := parseConfig(json); err == nil {
		t.Fatal("parseConfig returned no error for unrecognized peer protocol")
	}
}

func TestParseConfig_DefaultsAppliedForMTUStunAndRefreshInterval(t *testing.T) {
	cfg, err := parseConfig(minimalConfigJSON(validKeyB64(1), validKeyB64(2)))
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Interface.MTU != 1420 {
		t.Errorf("MTU = %d, want default 1420", cfg.Interface.MTU)
	}
	if len(cfg.Stun.Addresses) != 1 || cfg.Stun.Addresses[0] != defaultStunServer {
		t.Errorf("Stun.Addresses = %v, want [%q]", cfg.Stun.Addresses, defaultStunServer)
	}
	if cfg.RefreshIntervalSeconds != 600 {
		t.Errorf("RefreshIntervalSeconds = %d, want default 600", cfg.RefreshIntervalSeconds)
	}
}

func TestParseConfig_ExplicitValuesNotOverwrittenByDefaults(t *testing.T) {
	priv := validKeyB64(1)
	pub := validKeyB64(2)
	json := `{
		"interface": {"private_key": "` + priv + `", "mtu": 1280},
		"peers": [{"public_key": "` + pub + `"}],
		"stun": {"addresses": ["stun.example.com:3478"]},
		"refresh_interval_seconds": 30
	}`
	cfg, err := parseConfig(json)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Interface.MTU != 1280 {
		t.Errorf("MTU = %d, want explicit 1280", cfg.Interface.MTU)
	}
	if len(cfg.Stun.Addresses) != 1 || cfg.Stun.Addresses[0] != "stun.example.com:3478" {
		t.Errorf("Stun.Addresses = %v, want explicit value preserved", cfg.Stun.Addresses)
	}
	if cfg.RefreshIntervalSeconds != 30 {
		t.Errorf("RefreshIntervalSeconds = %d, want explicit 30", cfg.RefreshIntervalSeconds)
	}
}

func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid 32 byte key", validKeyB64(7), false},
		{"invalid base64", "not-base64!!", true},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("tooshort")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := keyToBytes(tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			raw, _ := base64.StdEncoding.DecodeString(tt.key)
			var want [32]byte
			copy(want[:], raw)
			if out != want {
				t.Errorf("keyToBytes(%q) = %v, want %v", tt.key, out, want)
			}
		})
	}
}

func TestKeyToHex(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{
			name:    "all zero bytes",
			key:     base64.StdEncoding.EncodeToString(make([]byte, 32)),
			want:    strings.Repeat("00", 32),
			wantErr: false,
		},
		{
			name:    "all 0xff bytes",
			key:     validKeyB64(0xff),
			want:    strings.Repeat("ff", 32),
			wantErr: false,
		},
		{"invalid base64", "not-base64!!", "", true},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("tooshort")), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := keyToHex(tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("keyToHex(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
