//go:build windows

package ctrl

import (
	"errors"
	"net"
	"time"
)

// ErrICMPNotImplemented makes the ping monitor controller log and continue
// without ping monitoring for the device.
var ErrICMPNotImplemented = errors.New("ICMP ping monitoring is not implemented on windows yet")

// ICMPConn is a Windows placeholder (real impl: IcmpSendEcho2 via iphlpapi,
// deferred); NewICMPConn always fails, so no instance is ever created.
type ICMPConn struct{}

// compile-time guarantee that the placeholder keeps the full platform surface
var _ ICMPConnection = (*ICMPConn)(nil)

// NewICMPConn always returns ErrICMPNotImplemented on Windows
func NewICMPConn(deviceName string) (*ICMPConn, error) {
	return nil, ErrICMPNotImplemented
}

// Send is unreachable on Windows since NewICMPConn never succeeds
func (c *ICMPConn) Send(data []byte, addr net.Addr) error {
	return ErrICMPNotImplemented
}

// Recv is unreachable on Windows since NewICMPConn never succeeds
func (c *ICMPConn) Recv(buffer []byte, timeout time.Duration) (n int, addr net.Addr, err error) {
	return 0, nil, ErrICMPNotImplemented
}

// Close closes the connection (nothing to release)
func (c *ICMPConn) Close() error {
	return nil
}

// SetReadDeadline is unreachable on Windows since NewICMPConn never succeeds
func (c *ICMPConn) SetReadDeadline(t time.Time) error {
	return ErrICMPNotImplemented
}
