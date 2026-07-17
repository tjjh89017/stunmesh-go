//go:build linux

package stun

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
)

// sockMark reads SO_MARK back off a socket.
func sockMark(c net.PacketConn) (int, error) {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return 0, fmt.Errorf("%T does not expose its fd", c)
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return 0, err
	}

	var mark int
	var getErr error
	if err := rc.Control(func(fd uintptr) {
		mark, getErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK)
	}); err != nil {
		return 0, err
	}
	return mark, getErr
}

// TestCreateRawSocket_AppliesFirewallMark covers the one step the resolver
// tests cannot reach: that the mark actually lands on the socket. It needs
// root, because a raw socket needs CAP_NET_RAW and SO_MARK needs
// CAP_NET_ADMIN. Without it, nothing would catch createRawSocket silently
// dropping the mark -- SO_MARK failing open looks exactly like success.
func TestCreateRawSocket_AppliesFirewallMark(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: raw socket requires CAP_NET_RAW and SO_MARK requires CAP_NET_ADMIN")
	}

	tests := []struct {
		name     string
		protocol string
		mark     int
	}{
		{name: "ipv4 marked", protocol: "ipv4", mark: 0xca6c},
		{name: "ipv6 marked", protocol: "ipv6", mark: 0xca6c},
		// The unmarked cases pin the no-op promise: with no fwmark on the
		// device we must leave the socket exactly as it was.
		{name: "ipv4 unmarked", protocol: "ipv4", mark: 0},
		{name: "ipv6 unmarked", protocol: "ipv6", mark: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()

			c, err := createRawSocket(t.Context(), tt.protocol, tt.mark, logger)
			if err != nil {
				// A kernel built without IPv6 cannot open the socket at all;
				// that is the environment's answer, not a failure of ours.
				if tt.protocol == "ipv6" && (errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT)) {
					t.Skipf("no IPv6 support in this kernel: %v", err)
				}
				t.Fatalf("createRawSocket: %v", err)
			}
			defer func() {
				if err := c.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			}()

			got, err := sockMark(c)
			if err != nil {
				t.Fatalf("read back SO_MARK: %v", err)
			}
			if got != tt.mark {
				t.Errorf("SO_MARK on the socket = %#x, want %#x", got, tt.mark)
			}
		})
	}
}
