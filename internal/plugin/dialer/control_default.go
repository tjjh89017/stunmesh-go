//go:build !(linux && !android)

package dialer

import (
	"context"
	"syscall"
)

// control does nothing yet outside Linux. darwin, windows and freebsd name an
// interface or routing table rather than carry a mark (IP_BOUND_IF,
// IP_UNICAST_IF, SO_SETFIB in internal/wgproxy), which needs the same route
// probing wgproxy does. Android cannot use SO_MARK at all -- it needs
// CAP_NET_ADMIN -- and escapes through VpnService.protect instead.
func control(_ context.Context, _, _ string, _ syscall.RawConn) error {
	return nil
}
