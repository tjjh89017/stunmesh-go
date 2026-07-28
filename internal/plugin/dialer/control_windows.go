//go:build windows

package dialer

import (
	"context"
	"syscall"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
	"golang.org/x/sys/windows"
)

// Option values absent from x/sys/windows, matching internal/wgproxy.
const (
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

// control binds the socket to the physical default-route interface, the escape
// internal/wgproxy applies to its outer socket: windows has no fwmark, so the
// way out of a covering tunnel route is to name the interface. The IPv4 option
// takes the index in network byte order and the IPv6 one in host order, so the
// family of this attempt decides both which option and which encoding.
func control(ctx context.Context, network, _ string, c syscall.RawConn) error {
	fam := family(network)
	index, ok, err := boundInterface(escapeFrom(ctx), fam)
	if err != nil || !ok {
		return err
	}
	var setErr error
	if ctrlErr := c.Control(func(fd uintptr) {
		h := windows.Handle(fd)
		if fam == routeprobe.IPv6 {
			setErr = windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIf, index)
		} else {
			setErr = windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf,
				int(routeprobe.UnicastIfNetworkOrder(uint32(index))))
		}
	}); ctrlErr != nil {
		return ctrlErr
	}
	return setErr
}
