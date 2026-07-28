// Package dialer gives the built-in plugins an outbound path that survives a
// tunnel stunmesh itself manages.
//
// A peer with a covering allowed-IPs route (full tunnel) captures every socket
// the process opens, including the ones a plugin uses to fetch the endpoint
// that would bring that tunnel up -- so the plugin call is routed into a
// tunnel that cannot carry it until the call succeeds. The outer sockets
// already avoid this: internal/stun mirrors the device's fwmark onto its probe
// socket, and internal/wgproxy applies a per-OS escape to the relay socket.
// The plugins were the one socket left without a way out.
//
// The escape needs the device's fwmark, which is only known once the device
// has been read, well after the plugins are constructed. It therefore travels
// in the context the controllers already pass to Store.Get and Store.Set, and
// is applied at dial time -- which also means a plugin instance shared by two
// interfaces escapes with the right mark for whichever peer is being served.
//
// exec and shell plugins are out of reach: their requests happen in a child
// process whose sockets stunmesh never sees. Escaping those is the operator's
// job (a VRF, a cgroup, or their own marking).
package dialer

import (
	"context"
	"net"
	"net/http"
	"time"
)

type contextKey struct{}

// WithFirewallMark returns a context carrying the device fwmark that outbound
// plugin traffic should be marked with. A zero mark means the device has none,
// which is the case whenever no covering route exists, so nothing is applied.
func WithFirewallMark(ctx context.Context, mark int) context.Context {
	if mark == 0 {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, mark)
}

func firewallMark(ctx context.Context) int {
	mark, _ := ctx.Value(contextKey{}).(int)
	return mark
}

// Transport returns an http.Transport whose sockets escape a covering tunnel,
// for a built-in plugin to use in place of one of its own.
func Transport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
		DialContext:         DialContext,
	}
}

// DialContext dials with the escape the platform provides.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		// ControlContext rather than Control: the mark to apply rides in the
		// context, so the hook needs it.
		ControlContext: control,
	}
	return d.DialContext(ctx, network, address)
}
