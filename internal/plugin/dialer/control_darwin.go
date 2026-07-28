//go:build darwin

package dialer

import (
	"context"
	"syscall"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
	"golang.org/x/sys/unix"
)

// control binds the socket to the physical default-route interface, the escape
// internal/wgproxy applies to its outer socket: darwin has no fwmark, so the
// way out of a covering tunnel route is to name the interface. IP_BOUND_IF and
// IPV6_BOUND_IF are separate options, so the family of this attempt decides.
func control(ctx context.Context, network, _ string, c syscall.RawConn) error {
	fam := family(network)
	index, ok, err := boundInterface(escapeFrom(ctx), fam)
	if err != nil || !ok {
		return err
	}
	var setErr error
	if ctrlErr := c.Control(func(fd uintptr) {
		if fam == routeprobe.IPv6 {
			setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, index)
		} else {
			setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
		}
	}); ctrlErr != nil {
		return ctrlErr
	}
	return setErr
}
