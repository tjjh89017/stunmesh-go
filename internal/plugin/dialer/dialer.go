// Package dialer gives the built-in plugins an outbound path that survives a
// tunnel stunmesh itself manages: a covering allowed-IPs route otherwise
// swallows the very call that would bring that tunnel up. internal/stun and
// internal/wgproxy already escape their sockets; the plugins were the one left.
//
// The mark travels in the context rather than being fixed at construction:
// plugins are built before the device is read, so no mark exists yet, and one
// instance may serve peers on different interfaces.
//
// exec and shell plugins are out of reach -- their requests happen in a child
// process whose sockets stunmesh never sees.
package dialer

import (
	"context"
	"net"
	"net/http"
	"time"
)

type contextKey struct{}

// WithFirewallMark carries the device fwmark to mark outbound plugin traffic
// with. Zero means the device has none, which is the case without a covering
// route, so nothing is applied.
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

// Transport returns an http.Transport whose sockets escape a covering tunnel.
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
		// ControlContext, not Control: the mark to apply rides in the context.
		ControlContext: control,
	}
	return d.DialContext(ctx, network, address)
}
