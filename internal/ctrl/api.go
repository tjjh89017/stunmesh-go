//go:generate mockgen -destination=./mock/mock_api.go -package=mock_ctrl . WireGuardClient

package ctrl

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type WireGuardClient interface {
	Device(ctx context.Context, deviceName string) (*wg.DeviceInfo, error)
	UpdatePeerEndpoint(ctx context.Context, u wg.PeerEndpointUpdate) error
}
