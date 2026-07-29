//go:generate mockgen -destination=./mock/mock_api.go -package=mock_ctrl . WireGuardClient

package ctrl

import (
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type WireGuardClient interface {
	Device(deviceName string) (*wg.DeviceInfo, error)
	UpdatePeerEndpoint(u wg.PeerEndpointUpdate) error
}
