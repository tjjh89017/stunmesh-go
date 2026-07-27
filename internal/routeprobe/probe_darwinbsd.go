//go:build darwin || freebsd

package routeprobe

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"golang.org/x/net/route"
)

func currentRoutes(family Family) ([]Route, error) {
	af := syscall.AF_INET
	if family == IPv6 {
		af = syscall.AF_INET6
	}

	rib, err := route.FetchRIB(af, route.RIBTypeRoute, 0)
	if err != nil {
		return nil, fmt.Errorf("routeprobe: fetch route table: %w", err)
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return nil, fmt.Errorf("routeprobe: parse route table: %w", err)
	}

	return routesFromMessages(msgs, family), nil
}

func routesFromMessages(msgs []route.Message, family Family) []Route {
	var routes []Route
	for _, m := range msgs {
		rm, ok := m.(*route.RouteMessage)
		if !ok || rm.Err != nil {
			continue
		}

		prefix, ok := routeMessagePrefix(rm, family)
		if !ok {
			continue
		}

		// Interfaces can disappear between the RIB dump and this lookup;
		// skip the route rather than fail the whole probe.
		ifi, err := net.InterfaceByIndex(rm.Index)
		if err != nil {
			continue
		}

		routes = append(routes, Route{Prefix: prefix, Interface: ifi.Name})
	}
	return routes
}

// routeMessagePrefix derives a prefix from a RouteMessage's destination and
// netmask address slots. BSD route dumps commonly omit RTAX_NETMASK for
// default routes (dst 0.0.0.0/::, no host flag), which this treats as /0;
// RTF_HOST routes with no netmask are treated as full-length host routes.
func routeMessagePrefix(rm *route.RouteMessage, family Family) (netip.Prefix, bool) {
	if len(rm.Addrs) <= syscall.RTAX_DST || rm.Addrs[syscall.RTAX_DST] == nil {
		return netip.Prefix{}, false
	}

	var addr netip.Addr
	switch a := rm.Addrs[syscall.RTAX_DST].(type) {
	case *route.Inet4Addr:
		if family != IPv4 {
			return netip.Prefix{}, false
		}
		addr = netip.AddrFrom4(a.IP)
	case *route.Inet6Addr:
		if family != IPv6 {
			return netip.Prefix{}, false
		}
		addr = netip.AddrFrom16(a.IP)
	default:
		return netip.Prefix{}, false
	}

	bits := 0
	if rm.Flags&syscall.RTF_HOST != 0 {
		bits = addr.BitLen()
	}
	if len(rm.Addrs) > syscall.RTAX_NETMASK {
		switch m := rm.Addrs[syscall.RTAX_NETMASK].(type) {
		case *route.Inet4Addr:
			bits, _ = net.IPMask(m.IP[:]).Size()
		case *route.Inet6Addr:
			bits, _ = net.IPMask(m.IP[:]).Size()
		}
	}

	return netip.PrefixFrom(addr, bits), true
}
