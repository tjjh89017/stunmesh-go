//go:build windows

package stun

import (
	"context"
	"errors"
)

const PacketSize = 1500

// ErrNotImplemented is returned by the Windows STUN stub. Windows has no
// raw-socket/pcap port-sharing path; STUN will instead ride the UDP proxy's
// outer socket via a StunTransport (proxy-backed STUN arrives in Phase 3).
var ErrNotImplemented = errors.New("STUN is not implemented yet on windows; proxy-backed STUN arrives in Phase 3")

// Stun is a Phase 0 compile stub mirroring the exported surface of the
// linux/darwin/freebsd implementations. New always fails, so no instance
// is ever created at runtime.
type Stun struct{}

// New always returns ErrNotImplemented on Windows
func New(ctx context.Context, excludeInterface string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (*Stun, error) {
	return nil, ErrNotImplemented
}

// Start is unreachable on Windows since New never succeeds
func (s *Stun) Start(ctx context.Context) {}

// Stop is unreachable on Windows since New never succeeds
func (s *Stun) Stop() error {
	return nil
}

// Connect is unreachable on Windows since New never succeeds
func (s *Stun) Connect(ctx context.Context, stunAddr string) (string, int, error) {
	return "", 0, ErrNotImplemented
}
