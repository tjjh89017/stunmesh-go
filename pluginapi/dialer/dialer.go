// Package dialer gives Go plugins the outbound path the built-in plugins use,
// one that escapes a tunnel stunmesh itself manages.
//
// Always pass the ctx received from Store.Get or Store.Set: the escape travels
// in it, and a fresh context dials straight into the tunnel.
package dialer

import (
	"context"
	"net"
	"net/http"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// Transport returns an http.Transport whose sockets escape a covering tunnel.
func Transport() *http.Transport {
	return dialer.Transport()
}

// DialContext dials with the escape ctx carries.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return dialer.DialContext(ctx, network, address)
}

// Resolver returns a resolver whose lookups escape a covering tunnel.
func Resolver() *net.Resolver {
	return dialer.Resolver()
}
