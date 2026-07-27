//go:build linux

package wgproxy

import (
	"net"
	"os"
	"syscall"
	"testing"
)

// sockMark reads SO_MARK back off a UDP socket.
func sockMark(conn *net.UDPConn) (int, error) {
	rc, err := conn.SyscallConn()
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

// TestMarkSocket_AppliesFirewallMark covers the step that needs root: SO_MARK
// requires CAP_NET_ADMIN. Without it, nothing would catch markSocket silently
// failing open (a dropped mark looks exactly like a zero mark that was never
// requested).
func TestMarkSocket_AppliesFirewallMark(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: SO_MARK requires CAP_NET_ADMIN")
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	const wantMark = 0xca6c
	if err := markSocket(conn, wantMark); err != nil {
		t.Fatalf("markSocket: %v", err)
	}

	got, err := sockMark(conn)
	if err != nil {
		t.Fatalf("read back SO_MARK: %v", err)
	}
	if got != wantMark {
		t.Errorf("SO_MARK on the socket = %#x, want %#x", got, wantMark)
	}
}
