//go:build linux

package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
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

// runFilter assembles the filter's raw instructions into a VM and runs it
// against pkt, mirroring what the kernel does with the BPF filter attached
// via SetBPF. A non-zero return means "accept" (stunBpfFilter always returns
// the fixed constant 262144 on accept, 0 on reject).
func runFilter(t *testing.T, raw []bpf.RawInstruction, pkt []byte) int {
	t.Helper()

	insts, ok := bpf.Disassemble(raw)
	if !ok {
		t.Fatalf("Disassemble left unrecognized raw instructions: %v", insts)
	}

	vm, err := bpf.NewVM(insts)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}

	n, err := vm.Run(pkt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	return n
}

// buildIPv4StunPacket builds a synthetic packet matching what stunBpfFilter
// sees for "ipv4": the full IP header (20 bytes, no options) is still
// present, because on Linux the kernel strips it only after the BPF filter
// runs.
func buildIPv4StunPacket(dstPort uint16, magic uint32) []byte {
	const ipHeaderLen = 20
	buf := make([]byte, ipHeaderLen+8+4+4) // IP + UDP + STUN header + magic cookie

	udpOff := ipHeaderLen
	binary.BigEndian.PutUint16(buf[udpOff:], 12345) // src port, arbitrary
	binary.BigEndian.PutUint16(buf[udpOff+2:], dstPort)

	magicOff := udpOff + 8 + 4
	binary.BigEndian.PutUint32(buf[magicOff:], magic)

	return buf
}

// buildIPv6StunPacket builds a synthetic packet matching what stunBpfFilter
// sees for "ipv6": no IP header, since the kernel strips it before the BPF
// filter runs for raw IPv6 sockets.
func buildIPv6StunPacket(dstPort uint16, magic uint32) []byte {
	buf := make([]byte, 8+4+4) // UDP + STUN header + magic cookie

	binary.BigEndian.PutUint16(buf[0:], 12345) // src port, arbitrary
	binary.BigEndian.PutUint16(buf[2:], dstPort)

	magicOff := 8 + 4
	binary.BigEndian.PutUint32(buf[magicOff:], magic)

	return buf
}

// TestStun_Read_ShortPacket covers Read's guard against a packet shorter
// than the 8-byte UDP header it always tries to skip. This is the
// application-layer counterpart to TestStunBpfFilter's "truncated before
// magic cookie" cases: those prove the BPF filter drops such packets before
// they ever reach userspace, this proves Read itself does not panic slicing
// past the buffer if one arrives anyway.
func TestStun_Read_ShortPacket(t *testing.T) {
	s := &Stun{packetChan: make(chan []byte, 1)}
	s.packetChan <- []byte{1, 2, 3, 4, 5, 6, 7} // 7 bytes, one short of the UDP header

	_, err := s.Read(t.Context())
	if err == nil {
		t.Fatal("expected an error for a packet shorter than the UDP header")
	}
}

// TestStun_Read_DecodeFailure covers the branch where a packet clears the
// 8-byte UDP-header skip but what follows is not a valid STUN message.
func TestStun_Read_DecodeFailure(t *testing.T) {
	s := &Stun{packetChan: make(chan []byte, 1)}
	// 8-byte UDP header + 4 bytes of garbage: long enough to pass the short-
	// packet guard, far short of STUN's 20-byte message header.
	buf := append(make([]byte, 8), 0xff, 0xff, 0xff, 0xff)
	s.packetChan <- buf

	_, err := s.Read(t.Context())
	if err == nil {
		t.Fatal("expected a STUN decode error")
	}
}

func TestStunBpfFilter(t *testing.T) {
	const (
		port  = uint16(51820)
		magic = uint32(0x2112A442)
	)

	tests := []struct {
		name     string
		protocol string
		build    func() []byte
		accept   bool
	}{
		{
			name:     "should accept ipv4 packet when port and magic cookie match",
			protocol: "ipv4",
			build:    func() []byte { return buildIPv4StunPacket(port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv4 packet when port differs",
			protocol: "ipv4",
			build:    func() []byte { return buildIPv4StunPacket(port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when magic cookie differs",
			protocol: "ipv4",
			build:    func() []byte { return buildIPv4StunPacket(port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when truncated before magic cookie",
			protocol: "ipv4",
			build: func() []byte {
				full := buildIPv4StunPacket(port, magic)
				return full[:len(full)-4] // drop the magic cookie bytes
			},
			accept: false,
		},
		{
			name:     "should accept ipv6 packet when port and magic cookie match",
			protocol: "ipv6",
			build:    func() []byte { return buildIPv6StunPacket(port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv6 packet when port differs",
			protocol: "ipv6",
			build:    func() []byte { return buildIPv6StunPacket(port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when magic cookie differs",
			protocol: "ipv6",
			build:    func() []byte { return buildIPv6StunPacket(port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when truncated before magic cookie",
			protocol: "ipv6",
			build: func() []byte {
				full := buildIPv6StunPacket(port, magic)
				return full[:len(full)-4]
			},
			accept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := stunBpfFilter(context.Background(), port, tt.protocol)
			if err != nil {
				t.Fatalf("stunBpfFilter: %v", err)
			}

			n := runFilter(t, raw, tt.build())

			if tt.accept && n == 0 {
				t.Errorf("expected packet to be accepted, got n=%d", n)
			}
			if !tt.accept && n != 0 {
				t.Errorf("expected packet to be rejected, got n=%d", n)
			}
		})
	}
}
