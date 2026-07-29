//go:build mobile && (linux || android)

package mobile

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// protectedContext carries protector as the escape internal/plugin/dialer's
// built-in plugins (cloudflare, opendht) use when they dial through
// dialer.Transport() -- the same http.Transport desktop's built-in plugins
// already use, unmodified. On every other platform Escape carries a
// mark/fib/interface to bind to; android has none of those (no CAP_NET_ADMIN,
// no fib, no stable interface index from inside an app sandbox), so
// dialer/control_default.go calls Escape.Protector -- VpnService.protect --
// instead, once one is present. This is the mobile half of that wiring; see
// internal/ctrl/publish.go's escapeFor for the desktop equivalent.
//
// A nil protector (SocketProtector was required by NewNode, so this should
// not happen once a Node is running) carries a nil Escape.Protector, which
// dialer/control_default.go already treats as a no-op -- the known,
// documented gap for a caller that never wires one up.
func protectedContext(ctx context.Context, protector SocketProtector) context.Context {
	return dialer.WithEscape(ctx, dialer.Escape{Protector: protector})
}
