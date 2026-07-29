//go:build darwin || freebsd || windows

package routeprobe

import (
	"net"
	"net/netip"
)

// resolveRouteInterface resolves idx to its current *net.Interface and
// packages it with prefix into a Route. It is the interface-index-to-Route
// step shared by the darwin/freebsd and windows probes (Linux does not need
// it: /proc/net/route already gives the interface name directly). ok is
// false when the interface has disappeared since the route table was read
// (e.g. between the RIB dump / GetIpForwardTable2 snapshot and this lookup);
// callers should skip the route rather than fail the whole probe.
func resolveRouteInterface(prefix netip.Prefix, idx int) (Route, bool) {
	ifi, err := net.InterfaceByIndex(idx)
	if err != nil {
		return Route{}, false
	}
	return Route{Prefix: prefix, Interface: ifi.Name, Index: ifi.Index}, true
}
