//go:build android || !(linux || darwin || freebsd || windows)

package dialer

import (
	"context"
	"syscall"
)

// control does nothing here. Android cannot use SO_MARK -- it needs
// CAP_NET_ADMIN, which an app does not have -- and escapes through
// VpnService.protect, applied by the mobile core to the sockets it owns.
func control(_ context.Context, _, _ string, _ syscall.RawConn) error {
	return nil
}
