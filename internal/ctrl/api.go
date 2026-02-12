//go:generate mockgen -destination=./mock/mock_api.go -package=mock_ctrl . WireGuardClient,ICMPConnection

package ctrl

import (
	"net"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardClient interface {
	Device(deviceName string) (*wgtypes.Device, error)
	ConfigureDevice(deviceName string, cfg wgtypes.Config) error
}

// ICMPConnection defines the interface for ICMP connections
type ICMPConnection interface {
	Send(data []byte, addr net.Addr) error
	Recv(buffer []byte, timeout time.Duration) (n int, addr net.Addr, err error)
	Close() error
	SetReadDeadline(t time.Time) error
}
