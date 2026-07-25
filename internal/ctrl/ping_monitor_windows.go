//go:build windows

package ctrl

import (
	"errors"
	"net"
	"time"
)

// ErrICMPNotImplemented signals that ping monitoring is not available on
// Windows yet. NewICMPConn returning it makes the ping monitor controller
// take its graceful-degradation path: it logs the error and continues
// without ping monitoring for the device.
var ErrICMPNotImplemented = errors.New("ICMP ping monitoring is not implemented on windows yet")

// ICMPConn is a placeholder ICMP connection for Windows. The real
// implementation (IcmpSendEcho2 via iphlpapi) is deferred; until then
// NewICMPConn always fails and no ICMPConn instance is ever created.
type ICMPConn struct {
	deviceName string
}

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
