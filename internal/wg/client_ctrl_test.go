//go:build !wgcli && (wgctrl || !freebsd)

package wg

import (
	"context"
	"errors"
	"net"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type fakeWgctrlBackend struct {
	deviceFn          func(name string) (*wgtypes.Device, error)
	configureDeviceFn func(name string, cfg wgtypes.Config) error

	configuredName string
	configuredCfg  wgtypes.Config
}

func (f *fakeWgctrlBackend) Device(name string) (*wgtypes.Device, error) {
	return f.deviceFn(name)
}

func (f *fakeWgctrlBackend) ConfigureDevice(name string, cfg wgtypes.Config) error {
	f.configuredName = name
	f.configuredCfg = cfg
	return f.configureDeviceFn(name, cfg)
}

func (f *fakeWgctrlBackend) Close() error {
	return nil
}

func TestCtrlClient_Device_MapsFields(t *testing.T) {
	var priv, pub, peerKey wgtypes.Key
	copy(priv[:], bytes32(0x01))
	copy(pub[:], bytes32(0x02))
	copy(peerKey[:], bytes32(0x03))

	backend := &fakeWgctrlBackend{
		deviceFn: func(name string) (*wgtypes.Device, error) {
			if name != "testdev" {
				t.Fatalf("Device called with name = %q, want %q", name, "testdev")
			}
			return &wgtypes.Device{
				Name:         name,
				ListenPort:   51820,
				PrivateKey:   priv,
				PublicKey:    pub,
				FirewallMark: 0xca6c,
				Peers: []wgtypes.Peer{
					{PublicKey: peerKey},
				},
			}, nil
		},
	}
	c := &ctrlClient{c: backend}

	info, err := c.Device(context.Background(), "testdev")
	if err != nil {
		t.Fatalf("Device: unexpected error: %v", err)
	}

	if info.Name != "testdev" {
		t.Errorf("Name = %q, want %q", info.Name, "testdev")
	}
	if info.ListenPort != 51820 {
		t.Errorf("ListenPort = %d, want 51820", info.ListenPort)
	}
	if info.PrivateKey != Key(priv) {
		t.Errorf("PrivateKey mismatch")
	}
	if info.PublicKey != Key(pub) {
		t.Errorf("PublicKey mismatch")
	}
	if info.FirewallMark != 0xca6c {
		t.Errorf("FirewallMark = %#x, want %#x", info.FirewallMark, 0xca6c)
	}
	if len(info.PeerKeys) != 1 || info.PeerKeys[0] != Key(peerKey) {
		t.Errorf("PeerKeys = %v, want [%v]", info.PeerKeys, peerKey)
	}
}

func TestCtrlClient_Device_ErrorPassesThroughElevationHint(t *testing.T) {
	backendErr := errors.New("device not found")
	backend := &fakeWgctrlBackend{
		deviceFn: func(name string) (*wgtypes.Device, error) {
			return nil, backendErr
		},
	}
	c := &ctrlClient{c: backend}

	_, err := c.Device(context.Background(), "testdev")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// elevationHint only rewrites access-denied errors; every other error,
	// including this one, must flow through unchanged.
	if !errors.Is(err, backendErr) {
		t.Errorf("Device error = %v, want it to wrap %v", err, backendErr)
	}
	if errors.Is(err, ErrElevationRequired) {
		t.Errorf("ordinary error must not be marked ErrElevationRequired")
	}
}

func TestCtrlClient_UpdatePeerEndpoint_ConfiguresDevice(t *testing.T) {
	var pk Key
	copy(pk[:], bytes32(0x07))

	backend := &fakeWgctrlBackend{
		configureDeviceFn: func(name string, cfg wgtypes.Config) error {
			return nil
		},
	}
	c := &ctrlClient{c: backend}

	err := c.UpdatePeerEndpoint(context.Background(), PeerEndpointUpdate{
		DeviceName: "testdev",
		PublicKey:  pk,
		Host:       "1.2.3.4",
		Port:       5678,
	})
	if err != nil {
		t.Fatalf("UpdatePeerEndpoint: unexpected error: %v", err)
	}

	if backend.configuredName != "testdev" {
		t.Errorf("configured device name = %q, want %q", backend.configuredName, "testdev")
	}
	if len(backend.configuredCfg.Peers) != 1 {
		t.Fatalf("configured peers len = %d, want 1", len(backend.configuredCfg.Peers))
	}
	peerCfg := backend.configuredCfg.Peers[0]
	if peerCfg.PublicKey != wgtypes.Key(pk) {
		t.Errorf("configured peer public key mismatch")
	}
	if peerCfg.UpdateOnly != UpdateOnly {
		t.Errorf("UpdateOnly = %v, want %v (package constant)", peerCfg.UpdateOnly, UpdateOnly)
	}
	wantEndpoint := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5678}
	if peerCfg.Endpoint == nil || !peerCfg.Endpoint.IP.Equal(wantEndpoint.IP) || peerCfg.Endpoint.Port != wantEndpoint.Port {
		t.Errorf("configured peer endpoint = %v, want %v", peerCfg.Endpoint, wantEndpoint)
	}
}

func TestCtrlClient_UpdatePeerEndpoint_ErrorPassesThroughElevationHint(t *testing.T) {
	backendErr := errors.New("configure failed")
	backend := &fakeWgctrlBackend{
		configureDeviceFn: func(name string, cfg wgtypes.Config) error {
			return backendErr
		},
	}
	c := &ctrlClient{c: backend}

	err := c.UpdatePeerEndpoint(context.Background(), PeerEndpointUpdate{DeviceName: "testdev"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, backendErr) {
		t.Errorf("UpdatePeerEndpoint error = %v, want it to wrap %v", err, backendErr)
	}
	if errors.Is(err, ErrElevationRequired) {
		t.Errorf("ordinary error must not be marked ErrElevationRequired")
	}
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}
