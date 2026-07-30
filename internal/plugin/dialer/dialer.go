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
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// Escape is what a socket needs to leave a covering tunnel. Each platform uses
// the part it has: Linux the mark, freebsd the fib, darwin and windows the
// interface the probe resolves, android the protector. Mirrors
// wgproxy.WithEscape's arguments.
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
	// Protector excludes a socket from the tunnel via Android's
	// VpnService.protect, for platforms with no mark/fib/interface primitive.
	// Nil means nothing to call, which is every caller except mobile.
	Protector Protector
	// DNSServers lists the nameservers ("host" or "host:port", port
	// defaulting to 53) hostname lookups dial instead of the ones Go reads
	// from /etc/resolv.conf. Android has no resolv.conf, so the pure-Go
	// resolver otherwise falls back to localhost and every lookup dies with
	// a connection refused; mobile fills this in. Empty trusts resolv.conf,
	// which is every other platform.
	DNSServers []string
}

// Protector is the fd-level escape Android provides in place of a
// mark/fib/interface primitive: mobile/api.go's SocketProtector, structurally,
// kept as its own type here so this package does not import mobile.
type Protector interface {
	// Protect returns true when the socket was excluded from the tunnel.
	Protect(fd int32) bool
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

// EscapeFrom exposes the Escape a context carries, for callers (tests, mainly)
// that need to verify a caller wired WithEscape correctly without duplicating
// dialer's internal Control logic.
func EscapeFrom(ctx context.Context) Escape {
	return escapeFrom(ctx)
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
	return newDialer().DialContext(ctx, network, address)
}

// newDialer builds the Dialer DialContext uses. Its Resolver.Dial routes
// back through DialContext so hostname lookups reuse the same protected
// socket path as the eventual TCP connect, instead of falling through to
// net.DefaultResolver -- on android that resolves via Bionic getaddrinfo/
// netd, which cannot be protected, so a DNS query would fail outright
// whenever the covering tunnel it must escape is down. PreferGo forces the
// Go resolver so Dial is actually consulted (the cgo resolver ignores it).
// Dial only ever connects to an address that is already an IP literal --
// resolv.conf-derived, or an Escape.DNSServers entry pickDNSServer vetted --
// so it does not recurse into hostname resolution.
func newDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		// ControlContext, not Control: the escape rides in the context.
		ControlContext: control,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial:     dialDNS,
		},
	}
}

// dnsRotation spreads successive lookups across Escape.DNSServers, so the
// resolver's retries for one lookup (each of which lands here as a fresh
// Dial call) move on to the next server instead of hammering the first.
var dnsRotation atomic.Uint32

// dialDNS is the Resolver.Dial hook. address is the nameserver Go derived
// from /etc/resolv.conf; when the Escape carries usable DNSServers -- the
// android case, where no resolv.conf exists and Go's fallback is a localhost
// address nothing answers -- the target is swapped for one of those instead.
// The ctx is the lookup's own, so the Escape rides in the same way it does
// for the connection the lookup is for.
func dialDNS(ctx context.Context, network, address string) (net.Conn, error) {
	if server, ok := pickDNSServer(escapeFrom(ctx).DNSServers); ok {
		address = server
	}
	return DialContext(ctx, network, address)
}

// pickDNSServer rotates across servers, skipping entries that are not IP
// literals: dialing a hostname would recurse into this very hook to resolve
// it, with the same list in the Escape, and never terminate. ok=false means
// no usable entry, leaving the resolv.conf-derived address in place.
func pickDNSServer(servers []string) (string, bool) {
	if len(servers) == 0 {
		return "", false
	}
	start := int(dnsRotation.Add(1) - 1)
	for i := range servers {
		if addr, ok := normalizeDNSAddr(servers[(start+i)%len(servers)]); ok {
			return addr, true
		}
	}
	return "", false
}

// normalizeDNSAddr accepts an IP literal as "host", "[host]" or "host:port"
// and returns "host:port" with the port defaulting to 53, so
// Escape.DNSServers can carry addresses the way platforms report them (bare
// IPs, IPv6 included). ok=false rejects anything whose host is not an IP
// literal -- see pickDNSServer for why hostnames cannot serve here.
func normalizeDNSAddr(s string) (string, bool) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host, port = strings.Trim(s, "[]"), ""
	}
	if port == "" {
		port = "53"
	}
	if _, err := netip.ParseAddr(host); err != nil {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// ValidNameserver reports whether s is usable as an Escape.DNSServers entry:
// an IP literal, optionally bracketed or carrying a port. Callers building a
// list from platform-reported strings filter with this so a stray hostname
// is dropped (a hostname needs resolving, which needs a nameserver) rather
// than silently poisoning the list.
func ValidNameserver(s string) bool {
	_, ok := normalizeDNSAddr(s)
	return ok
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
