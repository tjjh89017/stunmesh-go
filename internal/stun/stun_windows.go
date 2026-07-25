//go:build windows

package stun

import (
	"context"
	"errors"
)

const PacketSize = 1500

// ErrProxyTransportRequired is returned by the Windows New: there is no
// raw-socket/pcap port-sharing path here, so STUN must ride the UDP proxy's
// outer socket. Wire injects NewProxyBackedFactory (closing over the proxy
// transport) via NewResolverWithFactory; this default is only reached when
// proxy mode is not wired.
var ErrProxyTransportRequired = errors.New("STUN on windows requires the wgproxy transport; use the proxy-backed client factory")

// Stun mirrors the exported surface of the socket-owning platform
// implementations. New always fails, so no instance is ever created at
// runtime; the real Windows client is ProxyBacked (proxy_client.go).
type Stun struct{}

// New always returns ErrProxyTransportRequired on Windows.
func New(ctx context.Context, excludeInterface string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (*Stun, error) {
	return nil, ErrProxyTransportRequired
}

// Start is unreachable on Windows since New never succeeds
func (s *Stun) Start(ctx context.Context) {}

// Stop is unreachable on Windows since New never succeeds
func (s *Stun) Stop() error {
	return nil
}

// Connect is unreachable on Windows since New never succeeds
func (s *Stun) Connect(ctx context.Context, stunAddr string) (string, int, error) {
	return "", 0, ErrProxyTransportRequired
}
