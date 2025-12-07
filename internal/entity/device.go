package entity

import "errors"

var (
	ErrDeviceNotFound = errors.New("device not found")
)

type DeviceId string

type Device struct {
	name       DeviceId
	listenPort int
	privateKey []byte
	protocol   string
}

func NewDevice(name DeviceId, listenPort int, privateKey []byte, protocol string) *Device {
	return &Device{
		name:       name,
		listenPort: listenPort,
		privateKey: privateKey,
		protocol:   protocol,
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
