//go:build darwin || freebsd

package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"reflect"
	"sort"
	"testing"

	pcap "github.com/packetcap/go-pcap"
	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
)

// ifaceList builds a fake net.Interfaces() returning the named interfaces, all
// up and non-loopback unless the name is "lo0".
func ifaceList(names ...string) func() ([]net.Interface, error) {
	return func() ([]net.Interface, error) {
		out := make([]net.Interface, 0, len(names))
		for i, n := range names {
			flags := net.FlagUp
			if n == "lo0" {
				flags = net.FlagUp | net.FlagLoopback
			}
			out = append(out, net.Interface{Index: i + 1, Name: n, Flags: flags})
		}
		return out, nil
	}
}

func constRoute(name string, err error) func(string) (string, error) {
	return func(string) (string, error) { return name, err }
}

func TestResolveListenInterfaces(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name         string
		protocol     string
		exclude      string
		listen       []string
		defaultRoute bool
		ifaces       func() ([]net.Interface, error)
		route        func(string) (string, error)
		wantNames    []string // order-insensitive
		wantRequired []string
		wantErr      bool
	}{
		{
			name:      "no selector opens all eligible, minus loopback and wg",
			exclude:   "wg0",
			ifaces:    ifaceList("em0", "em1", "lo0", "wg0"),
			route:     constRoute("", nil),
			wantNames: []string{"em0", "em1"},
		},
		{
			name:    "no selector, nothing eligible is an error",
			exclude: "wg0",
			ifaces:  ifaceList("lo0", "wg0"),
			route:   constRoute("", nil),
			wantErr: true,
		},
		{
			name:         "explicit list selects only those, marked required",
			exclude:      "wg0",
			listen:       []string{"em0"},
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "unknown interface name is skipped, not fatal",
			exclude:      "wg0",
			listen:       []string{"em0", "typo0"},
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:    "explicit list of only unknowns resolves to none is an error",
			exclude: "wg0",
			listen:  []string{"typo0"},
			ifaces:  ifaceList("em0", "wg0"),
			route:   constRoute("", nil),
			wantErr: true,
		},
		{
			name:         "naming the wg interface itself is skipped",
			exclude:      "wg0",
			listen:       []string{"wg0", "em0"},
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"}, // em0 is explicitly listed, so still required
		},
		{
			name:         "default route interface is added (best effort, not required)",
			exclude:      "wg0",
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em1", nil),
			wantNames:    []string{"em1"},
			wantRequired: nil,
		},
		{
			name:         "union of explicit list and default route, deduped",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em0", nil), // same as explicit -> dedup
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "union keeps both when default route differs",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em1", nil),
			wantNames:    []string{"em0", "em1"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "default route missing for protocol resolves to none is an error",
			exclude:      "wg0",
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", nil), // e.g. no IPv6 default route
			wantErr:      true,
		},
		{
			name:         "default route lookup error is skipped, not fatal, when list carries",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", errors.New("route table read failed")),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "default route pointing at wg interface is skipped",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("wg0", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, required, err := resolveListenInterfaces(&logger, tt.protocol, tt.exclude, tt.listen, tt.defaultRoute, tt.ifaces, tt.route)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got names=%v", names)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotNames := append([]string(nil), names...)
			wantNames := append([]string(nil), tt.wantNames...)
			sort.Strings(gotNames)
			sort.Strings(wantNames)
			if !reflect.DeepEqual(gotNames, wantNames) {
				t.Errorf("names = %v, want %v", names, tt.wantNames)
			}

			var gotRequired []string
			for k := range required {
				gotRequired = append(gotRequired, k)
			}
			wantRequired := append([]string(nil), tt.wantRequired...)
			sort.Strings(gotRequired)
			sort.Strings(wantRequired)
			if !reflect.DeepEqual(gotRequired, wantRequired) {
				t.Errorf("required = %v, want %v", gotRequired, tt.wantRequired)
			}
		})
	}
}

// TestResolveListenInterfaces_InterfaceListError surfaces an enumeration
// failure rather than treating it as "no interfaces".
func TestResolveListenInterfaces_InterfaceListError(t *testing.T) {
	logger := zerolog.Nop()
	boom := errors.New("cannot list interfaces")
	failing := func() ([]net.Interface, error) { return nil, boom }

	_, _, err := resolveListenInterfaces(&logger, "ipv4", "wg0", []string{"em0"}, false, failing, constRoute("", nil))
	if !errors.Is(err, boom) {
		t.Fatalf("expected the enumeration error, got %v", err)
	}
}

// TestDefaultRouteInterface_Smoke exercises the real x/net/route parsing path on
// the platform it targets (it only runs where the build tag lets it compile).
// It cannot assert a specific interface -- the CI VM's routing table is not
// fixed -- but it pins that the RIB dump parses without error and that any name
// returned is a real interface.
func TestDefaultRouteInterface_Smoke(t *testing.T) {
	for _, protocol := range []string{"ipv4", "ipv6"} {
		name, err := defaultRouteInterface(protocol)
		if err != nil {
			t.Fatalf("defaultRouteInterface(%q): %v", protocol, err)
		}
		if name == "" {
			continue // no default route for this family is legitimate
		}
		if _, err := net.InterfaceByName(name); err != nil {
			t.Errorf("defaultRouteInterface(%q) returned %q which is not a real interface: %v", protocol, name, err)
		}
	}
}

// TestDecodeStunPacket_ShortPacket covers the guard against a buffer shorter
// than the payload offset. On darwin/bsd this replaces what Read itself does
// on Linux: Read here only receives an already-decoded *stun.Message off
// packetChan (see TestStun_Read_ReceivesDecodedMessage), so the short-packet
// and decode-failure logic lives in the per-interface Start goroutine instead
// -- decodeStunPacket is that logic, pulled out so it is testable without a
// real pcap handle.
func TestDecodeStunPacket_ShortPacket(t *testing.T) {
	buf := []byte{1, 2, 3, 4, 5, 6, 7} // shorter than the smallest realistic payloadOff (Null+IPv4: 4+20+8=32)

	_, err := decodeStunPacket(buf, 32)
	if err == nil {
		t.Fatal("expected an error for a packet shorter than the payload offset")
	}
	if !errors.Is(err, errShortPacket) {
		t.Errorf("expected errShortPacket, got %v", err)
	}
}

// TestDecodeStunPacket_DecodeFailure covers the branch where buf clears the
// payload-offset guard but what follows is not a valid STUN message.
func TestDecodeStunPacket_DecodeFailure(t *testing.T) {
	const payloadOff = 32 // Null header + IPv4 header + UDP header
	// payloadOff bytes of leading header + 4 bytes of garbage: long enough to
	// pass the short-packet guard, far short of STUN's 20-byte message header.
	buf := append(make([]byte, payloadOff), 0xff, 0xff, 0xff, 0xff)

	_, err := decodeStunPacket(buf, payloadOff)
	if err == nil {
		t.Fatal("expected a STUN decode error")
	}
	if errors.Is(err, errShortPacket) {
		t.Errorf("expected a decode error distinct from errShortPacket, got %v", err)
	}
}

// TestStun_Read_ReceivesDecodedMessage documents that, unlike Linux,
// packetChan here carries an already-decoded *stun.Message: Read is a plain
// passthrough with no parsing of its own to test.
func TestStun_Read_ReceivesDecodedMessage(t *testing.T) {
	s := &Stun{packetChan: make(chan *stun.Message, 1)}
	want := &stun.Message{}
	s.packetChan <- want

	got, err := s.Read(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Read returned %p, want the exact message sent on packetChan (%p)", got, want)
	}
}

func TestCalculatePayloadOffset(t *testing.T) {
	tests := []struct {
		name     string
		linkType uint32
		protocol string
		want     uint32
	}{
		{name: "null header, ipv4", linkType: pcap.LinkTypeNull, protocol: "ipv4", want: 4 + 20 + 8},
		{name: "null header, ipv6", linkType: pcap.LinkTypeNull, protocol: "ipv6", want: 4 + 40 + 8},
		{name: "ethernet header, ipv4", linkType: pcap.LinkTypeEthernet, protocol: "ipv4", want: 14 + 20 + 8},
		{name: "ethernet header, ipv6", linkType: pcap.LinkTypeEthernet, protocol: "ipv6", want: 14 + 40 + 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePayloadOffset(tt.linkType, tt.protocol)
			if got != tt.want {
				t.Errorf("calculatePayloadOffset(%v, %q) = %d, want %d", tt.linkType, tt.protocol, got, tt.want)
			}
		})
	}
}

// runFilter assembles the filter's raw instructions into a VM and runs it
// against pkt, mirroring what pcap's SetRawBPFFilter does with the same
// filter. A non-zero return means "accept" (both filters return the fixed
// constant 262144 on accept, 0 on reject).
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

// buildNullStunPacket builds a synthetic packet matching what
// stunNullBpfFilter sees on a BSD Null/loopback interface: a 4-byte protocol
// family header (host byte order, hence the odd-looking big-endian constants
// in the filter itself), followed by the IP header, UDP header and STUN
// payload.
func buildNullStunPacket(protocolFamily uint32, ipHeaderLen int, dstPort uint16, magic uint32) []byte {
	udpOff := 4 + ipHeaderLen
	buf := make([]byte, udpOff+8+4+4) // null header + IP + UDP + STUN header + magic cookie

	binary.BigEndian.PutUint32(buf[0:], protocolFamily)
	binary.BigEndian.PutUint16(buf[udpOff+2:], dstPort)

	magicOff := udpOff + 8 + 4
	binary.BigEndian.PutUint32(buf[magicOff:], magic)

	return buf
}

// buildEthernetStunPacket builds a synthetic packet matching what
// stunEthernetBpfFilter sees on an Ethernet interface: a 14-byte Ethernet
// header (EtherType at offset 12), followed by the IP header, UDP header and
// STUN payload.
func buildEthernetStunPacket(etherType uint16, ipHeaderLen int, dstPort uint16, magic uint32) []byte {
	const ethernetHeaderLen = 14
	udpOff := ethernetHeaderLen + ipHeaderLen
	buf := make([]byte, udpOff+8+4+4)

	binary.BigEndian.PutUint16(buf[12:], etherType)
	binary.BigEndian.PutUint16(buf[udpOff+2:], dstPort)

	magicOff := udpOff + 8 + 4
	binary.BigEndian.PutUint32(buf[magicOff:], magic)

	return buf
}

func TestStunNullBpfFilter(t *testing.T) {
	const (
		port  = uint16(51820)
		magic = uint32(0x2112A442)

		nullIPv4 = 0x02000000
		// BSD Null/loopback headers observed for IPv6 across platforms; the
		// filter accepts any of the three so it works regardless of which
		// AF_INET6 value the running kernel uses.
		nullIPv6A = 0x18000000
		nullIPv6B = 0x1C000000
		nullIPv6C = 0x1E000000
	)

	tests := []struct {
		name     string
		protocol string
		build    func() []byte
		accept   bool
	}{
		{
			name:     "should accept ipv4 packet when family, port and magic cookie match",
			protocol: "ipv4",
			build:    func() []byte { return buildNullStunPacket(nullIPv4, 20, port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv4 packet when protocol family is not AF_INET",
			protocol: "ipv4",
			build:    func() []byte { return buildNullStunPacket(nullIPv6A, 20, port, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when port differs",
			protocol: "ipv4",
			build:    func() []byte { return buildNullStunPacket(nullIPv4, 20, port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when magic cookie differs",
			protocol: "ipv4",
			build:    func() []byte { return buildNullStunPacket(nullIPv4, 20, port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when truncated before magic cookie",
			protocol: "ipv4",
			build: func() []byte {
				full := buildNullStunPacket(nullIPv4, 20, port, magic)
				return full[:len(full)-4]
			},
			accept: false,
		},
		{
			name:     "should accept ipv6 packet when family value is 24, port and magic cookie match",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv6A, 40, port, magic) },
			accept:   true,
		},
		{
			name:     "should accept ipv6 packet when family value is 28, port and magic cookie match",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv6B, 40, port, magic) },
			accept:   true,
		},
		{
			name:     "should accept ipv6 packet when family value is 30, port and magic cookie match",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv6C, 40, port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv6 packet when protocol family is AF_INET",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv4, 40, port, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when port differs",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv6A, 40, port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when magic cookie differs",
			protocol: "ipv6",
			build:    func() []byte { return buildNullStunPacket(nullIPv6A, 40, port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when truncated before magic cookie",
			protocol: "ipv6",
			build: func() []byte {
				full := buildNullStunPacket(nullIPv6A, 40, port, magic)
				return full[:len(full)-4]
			},
			accept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := stunNullBpfFilter(context.Background(), port, tt.protocol)
			if err != nil {
				t.Fatalf("stunNullBpfFilter: %v", err)
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

func TestStunEthernetBpfFilter(t *testing.T) {
	const (
		port  = uint16(51820)
		magic = uint32(0x2112A442)

		etherTypeIPv4 = 0x0800
		etherTypeIPv6 = 0x86DD
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
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv4, 20, port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv4 packet when port differs",
			protocol: "ipv4",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv4, 20, port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when magic cookie differs",
			protocol: "ipv4",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv4, 20, port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv4 packet when truncated before magic cookie",
			protocol: "ipv4",
			build: func() []byte {
				full := buildEthernetStunPacket(etherTypeIPv4, 20, port, magic)
				return full[:len(full)-4]
			},
			accept: false,
		},
		{
			name:     "should accept ipv6 packet when ethertype, port and magic cookie match",
			protocol: "ipv6",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv6, 40, port, magic) },
			accept:   true,
		},
		{
			name:     "should reject ipv6 packet when ethertype is not 0x86DD",
			protocol: "ipv6",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv4, 40, port, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when port differs",
			protocol: "ipv6",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv6, 40, port+1, magic) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when magic cookie differs",
			protocol: "ipv6",
			build:    func() []byte { return buildEthernetStunPacket(etherTypeIPv6, 40, port, 0xDEADBEEF) },
			accept:   false,
		},
		{
			name:     "should reject ipv6 packet when truncated before magic cookie",
			protocol: "ipv6",
			build: func() []byte {
				full := buildEthernetStunPacket(etherTypeIPv6, 40, port, magic)
				return full[:len(full)-4]
			},
			accept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := stunEthernetBpfFilter(context.Background(), port, tt.protocol)
			if err != nil {
				t.Fatalf("stunEthernetBpfFilter: %v", err)
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
