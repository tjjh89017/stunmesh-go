//go:generate mockgen -destination=./mock/mock_api.go -package=mock_repo . WireGuardClient

package repo

import (
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardClient interface {
	Device(deviceName string) (*wgtypes.Device, error)
}
