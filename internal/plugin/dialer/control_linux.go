//go:build linux && !android

package dialer

import (
	"context"
	"syscall"

	"golang.org/x/sys/unix"
)

// control marks the socket with the device's fwmark, the same escape
// internal/stun applies to its probe socket: wg-quick installs a
// "not fwmark <mark>" rule alongside a covering default route, so a marked
// socket takes the physical path instead of the tunnel. Without a covering
// route there is no mark and nothing to do.
//
// SO_MARK needs CAP_NET_ADMIN. stunmesh already requires it to manage
// WireGuard, so a failure here is a real error rather than a missing
// capability, and is reported instead of silently leaving the socket in the
// tunnel.
func control(ctx context.Context, _, _ string, c syscall.RawConn) error {
	mark := firewallMark(ctx)
	if mark == 0 {
		return nil
	}
	var setErr error
	if err := c.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
	}); err != nil {
		return err
	}
	return setErr
}
