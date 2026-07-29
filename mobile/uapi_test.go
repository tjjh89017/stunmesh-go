//go:build mobile

package mobile

import (
	"strings"
	"testing"
)

func TestBuildUAPI_MinimalInterfaceNoPeers(t *testing.T) {
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1)},
	}

	got, err := buildUAPI(cfg)
	if err != nil {
		t.Fatalf("buildUAPI returned error: %v", err)
	}

	privHex, _ := keyToHex(cfg.Interface.PrivateKey)
	if !strings.Contains(got, "private_key="+privHex+"\n") {
		t.Errorf("output missing private_key line: %q", got)
	}
	if strings.Contains(got, "listen_port=") {
		t.Errorf("output should omit listen_port when unset: %q", got)
	}
	if !strings.Contains(got, "replace_peers=true\n") {
		t.Errorf("output missing replace_peers=true: %q", got)
	}
}

func TestBuildUAPI_ListenPortIncludedWhenSet(t *testing.T) {
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1), ListenPort: 51820},
	}

	got, err := buildUAPI(cfg)
	if err != nil {
		t.Fatalf("buildUAPI returned error: %v", err)
	}
	if !strings.Contains(got, "listen_port=51820\n") {
		t.Errorf("output missing listen_port line: %q", got)
	}
}

func TestBuildUAPI_PeerWithAllOptionalFields(t *testing.T) {
	pub := validKeyB64(2)
	psk := validKeyB64(3)
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1)},
		Peers: []peerConfig{
			{
				PublicKey:           pub,
				PresharedKey:        psk,
				Endpoint:            "1.2.3.4:51820",
				AllowedIPs:          []string{"10.0.0.0/24", "10.0.1.0/24"},
				PersistentKeepalive: 25,
			},
		},
	}

	got, err := buildUAPI(cfg)
	if err != nil {
		t.Fatalf("buildUAPI returned error: %v", err)
	}

	pubHex, _ := keyToHex(pub)
	pskHex, _ := keyToHex(psk)
	for _, want := range []string{
		"public_key=" + pubHex + "\n",
		"preshared_key=" + pskHex + "\n",
		"endpoint=1.2.3.4:51820\n",
		"persistent_keepalive_interval=25\n",
		"replace_allowed_ips=true\n",
		"allowed_ip=10.0.0.0/24\n",
		"allowed_ip=10.0.1.0/24\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got: %q", want, got)
		}
	}
}

func TestBuildUAPI_PeerOmitsOptionalFieldsWhenUnset(t *testing.T) {
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1)},
		Peers:     []peerConfig{{PublicKey: validKeyB64(2)}},
	}

	got, err := buildUAPI(cfg)
	if err != nil {
		t.Fatalf("buildUAPI returned error: %v", err)
	}
	if strings.Contains(got, "preshared_key=") {
		t.Errorf("output should omit preshared_key when unset: %q", got)
	}
	if strings.Contains(got, "endpoint=") {
		t.Errorf("output should omit endpoint when unset: %q", got)
	}
	if strings.Contains(got, "persistent_keepalive_interval=") {
		t.Errorf("output should omit persistent_keepalive_interval when unset: %q", got)
	}
}

func TestBuildUAPI_InvalidInterfacePrivateKey(t *testing.T) {
	cfg := &tunnelConfig{Interface: ifaceConfig{PrivateKey: "not-base64!!"}}
	if _, err := buildUAPI(cfg); err == nil {
		t.Fatal("expected error for invalid private key")
	}
}

func TestBuildUAPI_InvalidPeerPublicKey(t *testing.T) {
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1)},
		Peers:     []peerConfig{{PublicKey: "garbage"}},
	}
	if _, err := buildUAPI(cfg); err == nil {
		t.Fatal("expected error for invalid peer public key")
	}
}

func TestBuildUAPI_InvalidPeerPresharedKey(t *testing.T) {
	cfg := &tunnelConfig{
		Interface: ifaceConfig{PrivateKey: validKeyB64(1)},
		Peers:     []peerConfig{{PublicKey: validKeyB64(2), PresharedKey: "garbage"}},
	}
	if _, err := buildUAPI(cfg); err == nil {
		t.Fatal("expected error for invalid peer preshared key")
	}
}

func TestBuildPeerEndpointUAPI(t *testing.T) {
	pub := validKeyB64(4)
	got, err := buildPeerEndpointUAPI(pub, "5.6.7.8:51820")
	if err != nil {
		t.Fatalf("buildPeerEndpointUAPI returned error: %v", err)
	}
	pubHex, _ := keyToHex(pub)
	want := "public_key=" + pubHex + "\nupdate_only=true\nendpoint=5.6.7.8:51820\n"
	if got != want {
		t.Errorf("buildPeerEndpointUAPI = %q, want %q", got, want)
	}
}

func TestBuildPeerEndpointUAPI_InvalidPublicKey(t *testing.T) {
	if _, err := buildPeerEndpointUAPI("garbage", "1.2.3.4:1"); err == nil {
		t.Fatal("expected error for invalid public key")
	}
}
