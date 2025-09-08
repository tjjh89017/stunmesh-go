//go:build linux

package ctrl

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
)

// ICMPConn implements ICMP connection with SO_BINDTODEVICE support
type ICMPConn struct {
	conn       *ipv4.RawConn
	file       *os.File
	deviceName string
}

// NewICMPConn creates a new ICMP connection bound to the specified device
func NewICMPConn(deviceName string) (*ICMPConn, error) {
	// Create raw ICMP socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, fmt.Errorf("failed to create raw ICMP socket: %w", err)
	}

	// Apply SO_BINDTODEVICE if device name is provided
	if deviceName != "" {
		err = syscall.SetsockoptString(fd, syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, deviceName)
		if err != nil {
			if err2 := syscall.Close(fd); err2 != nil {
				return nil, fmt.Errorf("failed to close syscall socket %s: %w", deviceName, err2)
			}
			return nil, fmt.Errorf("failed to bind socket to device %s: %w (requires CAP_NET_RAW capability)", deviceName, err)
		}
	}

	// Convert to os.File
	file := os.NewFile(uintptr(fd), "icmp-socket")

	// Create net.PacketConn from file
	netConn, err := net.FilePacketConn(file)
	if err != nil {
		if err2 := file.Close(); err2 != nil {
			return nil, fmt.Errorf("failed to close filePacketConn: %w", err2)
		}
		return nil, fmt.Errorf("failed to create PacketConn from file: %w", err)
	}

	// Create IPv4 raw connection
	rawConn, err := ipv4.NewRawConn(netConn)
	if err != nil {
		if err2 := netConn.Close(); err2 != nil {
			return nil, fmt.Errorf("failed to close netConn: %w", err2)
		}
		if err2 := file.Close(); err2 != nil {
			return nil, fmt.Errorf("failed to close file: %w", err2)
		}
		return nil, fmt.Errorf("failed to create IPv4 raw connection: %w", err)
	}

	return &ICMPConn{
		conn:       rawConn,
		file:       file,
		deviceName: deviceName,
	}, nil
}

// Send sends an ICMP packet to the target address
func (c *ICMPConn) Send(data []byte, addr net.Addr) error {
	ipAddr, ok := addr.(*net.IPAddr)
	if !ok {
		return fmt.Errorf("address must be *net.IPAddr, got %T", addr)
	}

	// Create IPv4 header
	header := &ipv4.Header{
		Version:  ipv4.Version,
		Len:      ipv4.HeaderLen,
		TotalLen: ipv4.HeaderLen + len(data),
		TTL:      64,
		Protocol: 1, // ICMP
		Dst:      ipAddr.IP,
	}

	return c.conn.WriteTo(header, data, nil)
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

	// Read from raw connection
	header, payload, _, err := c.conn.ReadFrom(buffer)
	if err != nil {
		return 0, nil, err
	}

	// Copy payload to buffer
	copy(buffer, payload)

	// Create address from header
	addr = &net.IPAddr{IP: header.Src}

	return len(payload), addr, nil
}

// Close closes the connection
func (c *ICMPConn) Close() error {
	var err1, err2 error

	if c.conn != nil {
		err1 = c.conn.Close()
	}

	if c.file != nil {
		err2 = c.file.Close()
	}

	// Return first error encountered
	if err1 != nil {
		return err1
	}
	return err2
}

// SetReadDeadline sets the read deadline for the connection
func (c *ICMPConn) SetReadDeadline(t time.Time) error {
	if c.conn != nil {
		return c.conn.SetReadDeadline(t)
	}
	return fmt.Errorf("connection not initialized")
}
