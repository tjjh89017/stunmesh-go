//go:build android || !(linux || darwin || freebsd || windows)

package dialer

import (
	"context"
	"errors"
	"syscall"
)

// control calls VpnService.protect on the socket via Escape.Protector, the
// escape Android provides in place of SO_MARK -- an app has no CAP_NET_ADMIN,
// so setsockopt is not an option. Only mobile/controller.go sets a Protector
// (see mobile/transport.go); everything else leaves it nil, in which case this
// is a no-op and plugin HTTP traffic stays in a covering tunnel's route. That
// is still this platform's default for any caller that never sets one.
func control(ctx context.Context, _, _ string, c syscall.RawConn) error {
	protector := escapeFrom(ctx).Protector
	if protector == nil {
		return nil
	}
	var ok bool
	if err := c.Control(func(fd uintptr) {
		ok = protector.Protect(int32(fd))
	}); err != nil {
		return err
	}
	if !ok {
		return errors.New("dialer: socket protect failed")
	}
	return nil
}
