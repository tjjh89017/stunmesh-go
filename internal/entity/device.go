package entity

import "errors"

var (
	ErrDeviceNotFound = errors.New("device not found")
)

type DeviceId string

type Device struct {
	name         DeviceId
	listenPort   int
	privateKey   []byte
	protocol     string
	firewallMark int
}

func NewDevice(name DeviceId, listenPort int, privateKey []byte, protocol string, firewallMark int) *Device {
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

func (d *Device) PrivateKey() [32]byte {
	var key [32]byte
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
