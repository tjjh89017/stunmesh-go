package node

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// protectedContext carries the socket escape for plugin and resolver sockets
// that dial through internal/plugin/dialer. On android, where an app has no
// mark, fib or stable interface to bind to, dialer/control_default.go calls
// Escape.Protector (VpnService.protect) instead. A nil protector is carried
// as a nil Escape.Protector, which the dialer treats as a no-op.
//
// dnsServers rides along as Escape.DNSServers. Empty leaves the dialer on
// the system resolver configuration; android has no /etc/resolv.conf, so a
// node there must be given an explicit list.
func protectedContext(ctx context.Context, protector SocketProtector, dnsServers []string) context.Context {
	return dialer.WithEscape(ctx, dialer.Escape{Protector: protector, DNSServers: dnsServers})
}
