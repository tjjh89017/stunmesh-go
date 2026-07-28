//go:build !(linux && !android)

package dialer

import (
	"context"
	"syscall"
)

// control does nothing outside Linux.
//
// darwin, windows and freebsd escape their outer socket by naming an interface
// or a routing table (IP_BOUND_IF, IP_UNICAST_IF, SO_SETFIB in
// internal/wgproxy), none of which a fwmark carries; wiring those here needs
// the same route probing wgproxy does, so a plugin on those platforms still
// follows a covering default route into the tunnel.
//
// Android cannot use SO_MARK at all: it needs CAP_NET_ADMIN, which an app does
// not have. Its escape is VpnService.protect, which the mobile core applies to
// its own sockets.
func control(_ context.Context, _, _ string, _ syscall.RawConn) error {
	return nil
}
