//go:build freebsd

package dialer

import (
	"context"
	"syscall"

	"golang.org/x/sys/unix"
)

// control puts the socket on the underlay FIB, the escape internal/wgproxy
// applies to its outer socket: freebsd has no per-socket bind-to-interface
// primitive, so escaping relies on an operator-provisioned second routing
// table holding the physical default route. SO_SETFIB is family-agnostic.
//
// A zero fib means proxy.fib was never configured, so there is no underlay
// table to move to and nothing is applied.
func control(ctx context.Context, _, _ string, c syscall.RawConn) error {
	fib := escapeFrom(ctx).Fib
	if fib == 0 {
		return nil
	}
	var setErr error
	if err := c.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SETFIB, fib)
	}); err != nil {
		return err
	}
	return setErr
}
