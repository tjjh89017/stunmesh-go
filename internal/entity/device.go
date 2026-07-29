package entity

import (
	"errors"
)

var (
	ErrDeviceNotFound = errors.New("device not found")
)

// deviceLogger warns about a privateKey passed to NewDevice that doesn't
// match WireGuard's 32-byte key length. Entities aren't wired with a
// logger of their own, so this mirrors config.deviceConfigLogger's
// throwaway startup-warning logger.
var deviceLogger = NewStartupLogger()

type DeviceId string

type Device struct {
	name         DeviceId
	listenPort   int
	privateKey   []byte
	protocol     string
	firewallMark int
}

// NewDevice constructs a Device. privateKey should be exactly
// PeerKeyLength (32) bytes, matching WireGuard's key size; a mismatched
// length is logged as a warning rather than rejected, since PrivateKey()
// already copies it into a fixed-size [32]byte array safely (zero-padding
// or truncating), and callers such as repo tests construct devices with
// placeholder keys of other lengths.
func NewDevice(name DeviceId, listenPort int, privateKey []byte, protocol string, firewallMark int) *Device {
	if len(privateKey) != PeerKeyLength {
		deviceLogger.Warn().
			Str("device", string(name)).
			Int("length", len(privateKey)).
			Int("expected", PeerKeyLength).
			Msg("device privateKey is not the expected WireGuard key length")
	}

	return &Device{
		name:         name,
		listenPort:   listenPort,
		privateKey:   privateKey,
		protocol:     protocol,
		firewallMark: firewallMark,
	}
}

func (d *Device) Name() DeviceId {
	return d.name
}

func (d *Device) PrivateKey() PrivateKey {
	var key PrivateKey
	copy(key[:], d.privateKey)
	return key
}

func (d *Device) ListenPort() int {
	return d.listenPort
}

func (d *Device) Protocol() string {
	return d.protocol
}

func (d *Device) FirewallMark() int {
	return d.firewallMark
}
