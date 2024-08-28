package entity

import "errors"

var (
	ErrDeviceNotFound = errors.New("device not found")
)

type DeviceId string

type Device struct {
	name       DeviceId
	privateKey []byte
}

func NewDevice(name DeviceId, privateKey []byte) *Device {
	return &Device{
		name:       name,
		privateKey: privateKey,
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
