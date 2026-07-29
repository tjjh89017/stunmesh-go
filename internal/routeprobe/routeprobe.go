// Package routeprobe answers one question: does a WireGuard-type interface
// currently install a route that would cover general egress traffic (a
// default route, or the 0.0.0.0/1 + 128.0.0.0/1 / ::/1 + 8000::/1 "half
// default" convention some full-tunnel configurations use)?
//
// Callers use this to decide whether a proxy outer socket needs to escape
// the tunnel instead of applying such options unconditionally. The escape
// mechanisms themselves (SO_MARK on Linux, IP_BOUND_IF/IPV6_BOUND_IF on
// darwin, IP_UNICAST_IF/IPV6_UNICAST_IF on Windows, SO_SETFIB on freebsd)
// live in internal/wgproxy's per-OS escape hooks; see wgproxy/escape.go.
package routeprobe

import "net/netip"

// Family selects which IP address family to inspect.
type Family int

const (
	IPv4 Family = iota
	IPv6
)

// Route is a minimal route record: a destination prefix and the name of the
// interface it is routed through. Per-OS code is responsible for producing
// these from whatever platform route table representation it reads.
type Route struct {
	Prefix    netip.Prefix
	Interface string
	// Index is the interface index, populated on platforms that resolve a
	// specific interface for binding (darwin/freebsd via DefaultRouteInterface).
	// Left zero where unused (Linux escape uses SO_MARK, not an interface index).
	Index int
}

var (
	halfV4Low  = netip.MustParsePrefix("0.0.0.0/1")
	halfV4High = netip.MustParsePrefix("128.0.0.0/1")
	halfV6Low  = netip.MustParsePrefix("::/1")
	halfV6High = netip.MustParsePrefix("8000::/1")
)

// HasCoveringDefault reports whether routes contains, for the given family, a
// /0 route or both halves of the /1+/1 pairing convention, where the
// matching route(s) go out an interface for which isTunnel returns true.
//
// The two halves of a /1 pair are not required to share the same interface:
// any tunnel interface carrying each half is sufficient. isTunnel is called
// with route.Interface for every route matching the requested family; a nil
// isTunnel treats no interface as a tunnel (always returns false).
func HasCoveringDefault(routes []Route, family Family, isTunnel func(name string) bool) bool {
	if isTunnel == nil {
		return false
	}

	var sawLowHalf, sawHighHalf bool

	for _, r := range routes {
		if !familyMatches(r.Prefix, family) {
			continue
		}
		if !isTunnel(r.Interface) {
			continue
		}

		if r.Prefix.Bits() == 0 {
			return true
		}

		p := r.Prefix.Masked()
		switch family {
		case IPv4:
			switch p {
			case halfV4Low:
				sawLowHalf = true
			case halfV4High:
				sawHighHalf = true
			}
		case IPv6:
			switch p {
			case halfV6Low:
				sawLowHalf = true
			case halfV6High:
				sawHighHalf = true
			}
		}

		if sawLowHalf && sawHighHalf {
			return true
		}
	}

	return false
}

// chooseDefaultInterface is the pure selection logic behind
// DefaultRouteInterface, split out for portable table-testing. It returns
// the first /0 route for family whose interface isTunnel does not accept.
// Full-tunnel setups using the /1+/1 half-default convention normally leave
// the original /0 route to the physical gateway in the table (just
// outranked by the halves via longest-prefix-match); this is what finds it.
// route table order approximates kernel priority order on the platforms
// routeprobe supports, so the first match is used rather than ranking by
// metric. ok is false when every /0 route present goes out a tunnel
// interface (or none exists) — callers must not fall back to a tunnel
// interface in that case.
func chooseDefaultInterface(routes []Route, family Family, isTunnel func(name string) bool) (Route, bool) {
	for _, r := range routes {
		if !familyMatches(r.Prefix, family) {
			continue
		}
		if r.Prefix.Bits() != 0 {
			continue
		}
		if isTunnel != nil && isTunnel(r.Interface) {
			continue
		}
		return r, true
	}
	return Route{}, false
}

func familyMatches(p netip.Prefix, family Family) bool {
	addr := p.Addr()
	switch family {
	case IPv4:
		return addr.Is4()
	case IPv6:
		return addr.Is6() && !addr.Is4In6()
	default:
		return false
	}
}
