//go:build windows

package stun

import "context"

// defaultClientFactory wraps the Windows stub New, which always errors; the
// proxy-backed factory is injected at wire time.
func defaultClientFactory(ctx context.Context, deviceName string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (StunClient, error) {
	return New(ctx, deviceName, port, protocol, firewallMark, listenInterfaces, listenDefaultRoute)
}
