//go:build windows

package routeprobe

import (
	"fmt"
	"net"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// currentRoutes reads the live IPv4 or IPv6 forwarding table via
// GetIpForwardTable2 (netioapi.h), the API wireguard-windows and winipcfg
// tooling use for the same purpose. Interface names are resolved from each
// row's InterfaceIndex via net.InterfaceByIndex, matching how the
// darwin/freebsd probe resolves its route.Index; a row whose interface has
// disappeared since the table snapshot was taken is skipped rather than
// failing the whole probe.
func currentRoutes(family Family) ([]Route, error) {
	af := uint16(windows.AF_INET)
	if family == IPv6 {
		af = windows.AF_INET6
	}

	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(af, &table); err != nil {
		return nil, fmt.Errorf("routeprobe: GetIpForwardTable2: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	rows := table.Rows()
	routes := make([]Route, 0, len(rows))
	for _, row := range rows {
		prefix, ok := forwardPrefix(row.DestinationPrefix, family)
		if !ok {
			continue
		}

		ifi, err := net.InterfaceByIndex(int(row.InterfaceIndex))
		if err != nil {
			continue
		}

		routes = append(routes, Route{Prefix: prefix, Interface: ifi.Name, Index: ifi.Index})
	}

	return routes, nil
}

// forwardPrefix converts a MIB_IPFORWARD_ROW2's DestinationPrefix — a
// SOCKADDR_INET union tagged by its embedded Family field — into a
// netip.Prefix, filtering out rows that don't match the requested family.
func forwardPrefix(p windows.IpAddressPrefix, family Family) (netip.Prefix, bool) {
	switch p.Prefix.Family {
	case windows.AF_INET:
		if family != IPv4 {
			return netip.Prefix{}, false
		}
		raw := (*windows.RawSockaddrInet4)(unsafe.Pointer(&p.Prefix))
		return netip.PrefixFrom(netip.AddrFrom4(raw.Addr), int(p.PrefixLength)), true
	case windows.AF_INET6:
		if family != IPv6 {
			return netip.Prefix{}, false
		}
		raw := (*windows.RawSockaddrInet6)(unsafe.Pointer(&p.Prefix))
		return netip.PrefixFrom(netip.AddrFrom16(raw.Addr), int(p.PrefixLength)), true
	default:
		return netip.Prefix{}, false
	}
}
