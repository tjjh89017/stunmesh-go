//go:build windows

package stun

import "context"

// defaultClientFactory wraps the Windows stub New, which always errors. The
// real client is injected as NewProxyBackedFactory (closing over the proxy's
// StunTransport) via NewResolverWithFactory at wire time, so this default is
// only reached when no proxy is configured.
func defaultClientFactory(ctx context.Context, deviceName string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (StunClient, error) {
	return New(ctx, deviceName, port, protocol, firewallMark, listenInterfaces, listenDefaultRoute)
}
