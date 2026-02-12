//go:generate mockgen -destination=./mock/mock_api.go -package=mock_ctrl . WireGuardClient

package ctrl

import (
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardClient interface {
	Device(deviceName string) (*wgtypes.Device, error)
	ConfigureDevice(deviceName string, cfg wgtypes.Config) error
}
