//go:build darwin || freebsd

package ctrl

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/icmp"
)

// ICMPConn implements ICMP connection using the standard icmp package
type ICMPConn struct {
	conn       *icmp.PacketConn
	deviceName string
}

// NewICMPConn creates a new ICMP connection (device binding not supported)
func NewICMPConn(deviceName string) (*ICMPConn, error) {
	// Create standard ICMP connection
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to create ICMP connection: %w", err)
	}
	
	// Note: Device binding is not supported on this platform
	// deviceName is ignored
	
	return &ICMPConn{
		conn:       conn,
		deviceName: deviceName,
	}, nil
}

// Send sends an ICMP packet to the target address
func (c *ICMPConn) Send(data []byte, addr net.Addr) error {
	_, err := c.conn.WriteTo(data, addr)
	return err
}

// Recv receives an ICMP packet with a timeout
func (c *ICMPConn) Recv(buffer []byte, timeout time.Duration) (n int, addr net.Addr, err error) {
	// Set read deadline
	if timeout > 0 {
		deadline := time.Now().Add(timeout)
		if err := c.SetReadDeadline(deadline); err != nil {
			return 0, nil, err
		}
	}

	// Read from connection
	n, addr, err = c.conn.ReadFrom(buffer)
	return n, addr, err
}

// Close closes the connection
func (c *ICMPConn) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetReadDeadline sets the read deadline for the connection
func (c *ICMPConn) SetReadDeadline(t time.Time) error {
	if c.conn != nil {
		return c.conn.SetReadDeadline(t)
	}
	return fmt.Errorf("connection not initialized")
}

