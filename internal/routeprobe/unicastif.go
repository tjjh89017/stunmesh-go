package routeprobe

import "math/bits"

// UnicastIfNetworkOrder converts an interface index to the byte order Windows'
// IP_UNICAST_IF socket option expects for IPv4: network (big-endian) order,
// unlike IPV6_UNICAST_IF which takes the index in host order -- a
// well-documented gotcha (also handled this way by wireguard-windows). Pure
// bit manipulation kept portable (no windows build tag) so it can be unit
// tested on any platform.
//
// It lives here rather than beside one caller because both the wgproxy outer
// socket and the plugin dialer bind by interface index and would otherwise
// each carry a copy of a subtlety that fails silently when it drifts.
func UnicastIfNetworkOrder(index uint32) uint32 {
	return bits.ReverseBytes32(index)
}
