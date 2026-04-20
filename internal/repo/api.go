//go:generate mockgen -destination=./mock/mock_api.go -package=mock_repo . WireGuardClient

package repo

import (
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type WireGuardClient interface {
	Device(deviceName string) (*wg.DeviceInfo, error)
}
