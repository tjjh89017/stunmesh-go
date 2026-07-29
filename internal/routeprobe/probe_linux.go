//go:build linux

package routeprobe

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

const (
	procNetRoute     = "/proc/net/route"
	procNetIPv6Route = "/proc/net/ipv6_route"
)

func currentRoutes(family Family) ([]Route, error) {
	switch family {
	case IPv4:
		data, err := os.ReadFile(procNetRoute)
		if err != nil {
			return nil, fmt.Errorf("routeprobe: read %s: %w", procNetRoute, err)
		}
		return parseProcNetRouteV4(data)
	case IPv6:
		data, err := os.ReadFile(procNetIPv6Route)
		if err != nil {
			// IPv6 may be disabled entirely; that is not a detection
			// failure, just an empty route set.
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("routeprobe: read %s: %w", procNetIPv6Route, err)
		}
		return parseProcNetRouteV6(data)
	default:
		return nil, fmt.Errorf("routeprobe: unknown family %d", family)
	}
}

// parseProcNetRouteV4 parses the /proc/net/route table format:
//
//	Iface   Destination     Gateway         Flags   RefCnt  Use     Metric  Mask            MTU     Window  IRTT
//	wlan0   00000000        0101A8C0        0003    0       0       600     00000000        0       0       0
//
// Destination/Mask are 8 hex chars holding the address in little-endian
// (i.e. byte-reversed relative to dotted-quad network order).
func parseProcNetRouteV4(data []byte) ([]Route, error) {
	var routes []Route

	scanner := bufio.NewScanner(bytes.NewReader(data))
	first := true
	for scanner.Scan() {
		if first {
			// header line
			first = false
			continue
		}

		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}

		iface := fields[0]
		destAddr, err := decodeLEHexIPv4(fields[1])
		if err != nil {
			return nil, fmt.Errorf("routeprobe: parse destination %q: %w", fields[1], err)
		}
		maskAddr, err := decodeLEHexIPv4(fields[7])
		if err != nil {
			return nil, fmt.Errorf("routeprobe: parse mask %q: %w", fields[7], err)
		}

		ones := ipv4MaskOnes(maskAddr)
		prefix := netip.PrefixFrom(destAddr, ones)
		routes = append(routes, Route{Prefix: prefix, Interface: iface})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("routeprobe: scan %s: %w", procNetRoute, err)
	}

	return routes, nil
}

// parseProcNetRouteV6 parses the /proc/net/ipv6_route table format, a
// whitespace-separated line of:
//
//	dest(32 hex) dest_len(2 hex) src(32 hex) src_len(2 hex) next_hop(32 hex)
//	metric(8 hex) ref_cnt(8 hex) use(8 hex) flags(8 hex) iface
//
// dest is 16 raw address bytes in network order (not byte-reversed, unlike
// the IPv4 table).
func parseProcNetRouteV6(data []byte) ([]Route, error) {
	var routes []Route

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}

		destHex := fields[0]
		lenHex := fields[1]
		iface := fields[9]

		addrBytes, err := hex.DecodeString(destHex)
		if err != nil || len(addrBytes) != 16 {
			return nil, fmt.Errorf("routeprobe: parse destination %q: %w", destHex, err)
		}
		bits, err := strconv.ParseUint(lenHex, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("routeprobe: parse prefix length %q: %w", lenHex, err)
		}

		addr := netip.AddrFrom16([16]byte(addrBytes))
		prefix := netip.PrefixFrom(addr, int(bits))
		routes = append(routes, Route{Prefix: prefix, Interface: iface})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("routeprobe: scan %s: %w", procNetIPv6Route, err)
	}

	return routes, nil
}

func decodeLEHexIPv4(s string) (netip.Addr, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return netip.Addr{}, fmt.Errorf("invalid IPv4 hex field %q", s)
	}
	v := binary.LittleEndian.Uint32(b)
	var be [4]byte
	binary.BigEndian.PutUint32(be[:], v)
	return netip.AddrFrom4(be), nil
}

// ipv4MaskOnes returns the number of leading one bits in addr, treating
// it as a subnet mask. It does not validate contiguity; /proc/net/route only
// ever contains proper masks.
func ipv4MaskOnes(addr netip.Addr) (ones int) {
	b := addr.As4()
	for _, octet := range b {
		if octet == 0xff {
			ones += 8
			continue
		}
		for octet&0x80 != 0 {
			ones++
			octet <<= 1
		}
		break
	}
	return ones
}
