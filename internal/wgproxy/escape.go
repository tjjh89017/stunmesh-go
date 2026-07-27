// Tunnel-escape hook: applied to each outer socket once at creation so its
// packets bypass a covering WireGuard default route (full-tunnel mode). The
// mechanism is per-OS: escape_linux.go implements SO_MARK, a one-shot
// decision (shouldEscape below) since fwmark persists on the fd for its
// lifetime. escape_darwin.go implements IP_BOUND_IF/IPV6_BOUND_IF, which
// names a specific interface index rather than a routing-policy mark, so it
// has its own decision path (no fwmark involved) plus a route-change watcher
// that re-applies the binding — see escape_darwin.go. escape_windows.go
// mirrors darwin's approach with IP_UNICAST_IF/IPV6_UNICAST_IF and a
// NotifyIpInterfaceChange watcher instead of a PF_ROUTE socket.
// escape_freebsd.go implements SO_SETFIB: freebsd has no per-socket
// bind-to-interface primitive, so escape instead relies on an operator-
// provisioned second FIB (routing table) that holds the physical default
// route; SO_SETFIB persists on the fd like Linux's SO_MARK, so it too is a
// one-shot decision with no watcher. escape_default.go is a no-op
// placeholder for any other platform (e.g. android).
package wgproxy

import (
	"math/bits"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// escapeOptions carries what the per-OS hook needs to decide and apply an
// escape. Zero value (no tunnelIfaces) means "escape not configured" — New
// skips the probe entirely rather than reporting spurious warnings.
type escapeOptions struct {
	firewallMark int
	fib          int
	tunnelIfaces routeprobe.TunnelInterfaces
}

// Option configures optional Proxy construction behavior.
type Option func(*escapeOptions)

// WithEscape enables the tunnel-escape hook for every outer socket New
// creates. firewallMark is the WireGuard device's own fwmark (0 means none
// configured), used by the linux hook. fib is the freebsd analog: the
// operator-provisioned FIB number carrying the physical default route (0
// means none configured, matching firewallMark's convention). tunnelIfaces
// limits routeprobe detection to interfaces stunmesh manages.
func WithEscape(firewallMark int, fib int, tunnelIfaces routeprobe.TunnelInterfaces) Option {
	return func(o *escapeOptions) {
		o.firewallMark = firewallMark
		o.fib = fib
		o.tunnelIfaces = tunnelIfaces
	}
}

// routeprobeFamily maps a wgproxy outer-socket family to routeprobe's.
func routeprobeFamily(fam Family) routeprobe.Family {
	if fam == FamilyIPv6 {
		return routeprobe.IPv6
	}
	return routeprobe.IPv4
}

// shouldEscape decides whether an outer socket should be marked, given
// routeprobe's result for its family and the device's fwmark. A probe error
// is treated the same as "no covering default" — never as a reason to
// escape anyway.
func shouldEscape(covering bool, probeErr error, firewallMark int) bool {
	if probeErr != nil || !covering {
		return false
	}
	return firewallMark != 0
}

// shouldSetFib is escape_freebsd.go's analog of shouldEscape: given
// routeprobe's result for a family and the configured FIB, decides whether
// SO_SETFIB should be applied. A probe error is treated the same as "no
// covering default" — never as a reason to set the FIB anyway.
func shouldSetFib(covering bool, probeErr error, fib int) bool {
	if probeErr != nil || !covering {
		return false
	}
	return fib != 0
}

// windowsUnicastIfNetworkOrder converts an interface index to the byte order
// Windows' IP_UNICAST_IF socket option expects for IPv4: network (big-endian)
// order, unlike IPV6_UNICAST_IF which takes the index in host order — a
// well-documented gotcha (also handled this way by wireguard-windows). Pure
// bit manipulation kept portable (no windows build tag) so it can be unit
// tested on any platform.
func windowsUnicastIfNetworkOrder(index uint32) uint32 {
	return bits.ReverseBytes32(index)
}
