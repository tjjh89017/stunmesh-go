//go:build !windows

package stun

import "context"

// defaultClientFactory wraps the platform's New (raw socket on Linux, pcap on
// darwin/bsd), preserving the pre-seam behavior exactly.
func defaultClientFactory(ctx context.Context, deviceName string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (StunClient, error) {
	return New(ctx, deviceName, port, protocol, firewallMark, listenInterfaces, listenDefaultRoute)
}
