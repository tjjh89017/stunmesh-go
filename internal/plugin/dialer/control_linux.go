//go:build linux && !android

package dialer

import (
	"context"
	"syscall"

	"golang.org/x/sys/unix"
)

// control mirrors the device's fwmark onto the socket, the same escape
// internal/stun applies: wg-quick installs a "not fwmark <mark>" rule beside a
// covering default route, so a marked socket takes the physical path. The mark
// is family-agnostic, so one setsockopt covers IPv4 and IPv6.
//
// SO_MARK needs CAP_NET_ADMIN, which stunmesh already requires, so a failure
// is reported rather than silently leaving the socket in the tunnel.
func control(ctx context.Context, _, _ string, c syscall.RawConn) error {
	mark := escapeFrom(ctx).FirewallMark
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
