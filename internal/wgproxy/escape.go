// Tunnel-escape hook: applied to each outer socket once at creation so its
// packets bypass a covering WireGuard default route (full-tunnel mode). The
// mechanism is per-OS: escape_linux.go implements SO_MARK, a one-shot
// decision (shouldEscape below) since fwmark persists on the fd for its
// lifetime. escape_darwin.go implements IP_BOUND_IF/IPV6_BOUND_IF, which
// names a specific interface index rather than a routing-policy mark, so it
// has its own decision path (no fwmark involved) plus a route-change watcher
// that re-applies the binding — see escape_darwin.go. escape_default.go is a
// no-op placeholder for freebsd/windows, filled in by later work.
package wgproxy

import "github.com/tjjh89017/stunmesh-go/internal/routeprobe"

// escapeOptions carries what the per-OS hook needs to decide and apply an
// escape. Zero value (no tunnelIfaces) means "escape not configured" — New
// skips the probe entirely rather than reporting spurious warnings.
type escapeOptions struct {
	firewallMark int
	tunnelIfaces routeprobe.TunnelInterfaces
}

// Option configures optional Proxy construction behavior.
type Option func(*escapeOptions)

// WithEscape enables the tunnel-escape hook for every outer socket New
// creates. firewallMark is the WireGuard device's own fwmark (0 means none
// configured); tunnelIfaces limits routeprobe detection to interfaces
// stunmesh manages.
func WithEscape(firewallMark int, tunnelIfaces routeprobe.TunnelInterfaces) Option {
	return func(o *escapeOptions) {
		o.firewallMark = firewallMark
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
