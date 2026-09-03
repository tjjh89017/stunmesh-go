//go:generate mockgen -destination=./mock/mock_api.go -package=mock_repo . WireGuardClient

package repo

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type WireGuardClient interface {
	Device(ctx context.Context, deviceName string) (*wg.DeviceInfo, error)
}
