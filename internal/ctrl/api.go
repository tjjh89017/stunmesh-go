//go:generate mockgen -destination=./mock/mock_api.go -package=mock_ctrl . WireGuardClient,ICMPConnection

package ctrl

import (
	"net"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

type WireGuardClient interface {
	Device(deviceName string) (*wg.DeviceInfo, error)
	UpdatePeerEndpoint(u wg.PeerEndpointUpdate) error
}

// ICMPConnection defines the interface for ICMP connections
type ICMPConnection interface {
	Send(data []byte, addr net.Addr) error
	Recv(buffer []byte, timeout time.Duration) (n int, addr net.Addr, err error)
	Close() error
	SetReadDeadline(t time.Time) error
}
