// Package dialer gives the built-in plugins an outbound path that survives a
// tunnel stunmesh itself manages: a covering allowed-IPs route otherwise
// swallows the very call that would bring that tunnel up. internal/stun and
// internal/wgproxy already escape their sockets; the plugins were the one left.
//
// The escape travels in the context rather than being fixed at construction:
// plugins are built before any device is read, so none of it is known yet, and
// one instance may serve peers on different interfaces.
//
// exec and shell plugins are out of reach -- their requests happen in a child
// process whose sockets stunmesh never sees.
package dialer

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// Escape is what a socket needs to leave a covering tunnel. Each platform uses
// the part it has: Linux the mark, freebsd the fib, darwin and windows the
// interface the probe resolves. Mirrors wgproxy.WithEscape's arguments.
type Escape struct {
	// FirewallMark is the device's fwmark. Zero means the device has none,
	// which is the case without a covering route.
	FirewallMark int
	// Fib is the underlay routing table on freebsd, from proxy.fib. Zero means
	// unconfigured, so nothing is applied.
	Fib int
	// TunnelIfaces names the interfaces stunmesh manages, so the probe can
	// tell a covering tunnel route from the physical path.
	TunnelIfaces routeprobe.TunnelInterfaces
}

type contextKey struct{}

// WithEscape carries how outbound plugin traffic should leave the host.
func WithEscape(ctx context.Context, escape Escape) context.Context {
	return context.WithValue(ctx, contextKey{}, escape)
}

func escapeFrom(ctx context.Context) Escape {
	escape, _ := ctx.Value(contextKey{}).(Escape)
	return escape
}

// family reports which address family a dial is for. Control sees the network
// of the actual connection attempt ("tcp4"/"tcp6"), not the "tcp" the caller
// asked for, so this is decided per attempt -- which matters because the
// bind-to-interface options are per-family.
func family(network string) routeprobe.Family {
	if strings.HasSuffix(network, "6") {
		return routeprobe.IPv6
	}
	return routeprobe.IPv4
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
		// ControlContext, not Control: the escape rides in the context.
		ControlContext: control,
	}
	return d.DialContext(ctx, network, address)
}

// boundInterface resolves the physical default-route interface for platforms
// that escape by naming one, or returns ok=false when no covering tunnel route
// exists and there is nothing to escape.
func boundInterface(escape Escape, fam routeprobe.Family) (int, bool, error) {
	if len(escape.TunnelIfaces) == 0 {
		return 0, false, nil
	}
	covering, err := routeprobe.Probe(fam, escape.TunnelIfaces)
	if err != nil || !covering {
		return 0, false, err
	}
	route, ok, err := routeprobe.DefaultRouteInterface(fam, escape.TunnelIfaces)
	if err != nil || !ok || route.Index == 0 {
		return 0, false, err
	}
	return route.Index, true, nil
}
