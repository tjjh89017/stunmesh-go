// build +linux
package stun

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const PacketSize = 1500

type Stun struct {
	port       uint16
	protocol   string
	conn4      *ipv4.PacketConn
	conn6      *ipv6.PacketConn
	once       sync.Once
	packetChan chan []byte
	// done is closed by Stop to release a listener goroutine that is parked
	// trying to hand off a packet nobody is waiting for any more (a reply that
	// arrived after Read already timed out). Without it Stop's close of
	// packetChan would race that send and panic.
	done     chan struct{}
	stopOnce sync.Once
}

// markControl stamps firewallMark on the socket before it is bound, so the
// probe is routed exactly like the WireGuard traffic whose NAT mapping it
// measures. Without it a policy-routing rule keyed on the mark -- the one
// wg-quick installs to keep WireGuard's own packets out of its tunnel -- sends
// the probe down a different path, and the endpoint we discover is the wrong
// NAT's.
func markControl(firewallMark int) func(string, string, syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var setErr error
		if err := c.Control(func(fd uintptr) {
			setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, firewallMark)
		}); err != nil {
			return err
		}
		return setErr
	}
}

func createRawSocket(ctx context.Context, protocol string, firewallMark int, logger zerolog.Logger) (net.PacketConn, error) {
	var lc net.ListenConfig
	if firewallMark != 0 {
		lc.Control = markControl(firewallMark)
		logger.Debug().Int("fwmark", firewallMark).Msg("marking STUN socket to match the device")
	}

	if protocol == "ipv6" {
		logger.Debug().Msg("creating IPv6 raw socket")
		return lc.ListenPacket(ctx, "ip6:17", "")
	}
	logger.Debug().Msg("creating IPv4 raw socket")
	return lc.ListenPacket(ctx, "ip4:17", "0.0.0.0")
}

func setPacketConn(s *Stun, c net.PacketConn, filter []bpf.RawInstruction) error {
	if s.protocol == "ipv6" {
		conn6 := ipv6.NewPacketConn(c)
		if filter != nil {
			if err := conn6.SetBPF(filter); err != nil {
				return err
			}
		}
		// Enable kernel checksum calculation for UDP (checksum at offset 6)
		if err := conn6.SetChecksum(true, 6); err != nil {
			return err
		}
		s.conn6 = conn6
	} else {
		conn4 := ipv4.NewPacketConn(c)
		if filter != nil {
			if err := conn4.SetBPF(filter); err != nil {
				return err
			}
		}
		s.conn4 = conn4
	}
	return nil
}

// listenIgnoredOnce guards the one-time warning that Linux ignores the
// per-interface listen restriction. New runs once per refresh cycle, so a
// plain log would repeat every cycle; sync.Once fires it exactly once.
var listenIgnoredOnce sync.Once

func New(ctx context.Context, excludeInterface string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (*Stun, error) {
	logger := zerolog.Ctx(ctx)

	// Linux discovers via a system-wide raw socket with a BPF filter, so there
	// is no per-interface listen to restrict. Honor the config's spirit by
	// telling the user it has no effect here rather than silently dropping it.
	if len(listenInterfaces) > 0 || listenDefaultRoute {
		listenIgnoredOnce.Do(func() {
			logger.Warn().Msg("listen_interfaces/listen_default_route are ignored on Linux (system-wide raw socket); they apply to darwin/bsd only")
		})
	}

	c, err := createRawSocket(ctx, protocol, firewallMark, *logger)
	if err != nil {
		return nil, err
	}

	filter, err := stunBpfFilter(ctx, port, protocol)
	if err != nil {
		return nil, err
	}

	s := &Stun{
		port:       port,
		protocol:   protocol,
		packetChan: make(chan []byte),
		done:       make(chan struct{}),
	}

	if err := setPacketConn(s, c, filter); err != nil {
		return nil, err
	}

	return s, nil
}

// Stop releases the listener goroutine and closes the socket. packetChan is
// deliberately left open: Read already selects with a timeout, so closing it
// buys nothing and only opens a send-on-closed-channel race with a listener
// still holding a late reply. The channel is garbage collected with the Stun.
func (s *Stun) Stop() error {
	s.stopOnce.Do(func() { close(s.done) })
	if s.protocol == "ipv6" {
		return s.conn6.Close()
	}
	return s.conn4.Close()
}

func (s *Stun) readFrom(buf []byte) (int, error) {
	if s.protocol == "ipv6" {
		n, _, _, err := s.conn6.ReadFrom(buf)
		return n, err
	}
	n, _, _, err := s.conn4.ReadFrom(buf)
	return n, err
}

func (s *Stun) Start(ctx context.Context) {
	s.once.Do(func() {
		go func() {
			timeout := time.After(time.Duration(StunTimeout+5) * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.done:
					return
				case <-timeout:
					return
				default:
					buf := make([]byte, PacketSize)
					n, err := s.readFrom(buf)
					if err != nil {
						// Stop closed the socket out from under us; leave
						// rather than spin on a dead fd until timeout.
						select {
						case <-s.done:
							return
						default:
						}
						continue
					}
					select {
					case s.packetChan <- buf[:n]:
						return
					case <-ctx.Done():
						return
					case <-s.done:
						return
					}
				}
			}
		}()
	})
}

func (s *Stun) getUDPAddressFamily() string {
	if s.protocol == "ipv6" {
		return "udp6"
	}
	return "udp4"
}

func (s *Stun) writeTo(packet []byte, addr net.Addr) (int, error) {
	// For raw IP sockets, convert UDPAddr to IPAddr
	// because the UDP header (including ports) is already in our packet
	destAddr := addr
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		destAddr = &net.IPAddr{
			IP:   udpAddr.IP,
			Zone: udpAddr.Zone,
		}
	}

	if s.protocol == "ipv6" {
		return s.conn6.WriteTo(packet, nil, destAddr)
	}
	return s.conn4.WriteTo(packet, nil, destAddr)
}

func (s *Stun) Connect(ctx context.Context, stunAddr string) (string, int, error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("connecting to STUN server: %s", stunAddr)

	addr, err := net.ResolveUDPAddr(s.getUDPAddressFamily(), stunAddr)
	if err != nil {
		return "", 0, err
	}

	packet, err := createStunBindingPacket(s.port, uint16(addr.Port))
	if err != nil {
		return "", 0, err
	}

	if _, err = s.writeTo(packet, addr); err != nil {
		return "", 0, err
	}

	reply, err := s.Read(ctx)
	if err != nil {
		return "", 0, err
	}

	// Parse returns nil when the reply carries no XOR-MAPPED-ADDRESS; the
	// resolver treats the error as "this server failed" and moves to the next.
	replyAddr := Parse(ctx, reply)
	if replyAddr == nil {
		return "", 0, ErrNoMappedAddress
	}

	return replyAddr.IP.String(), replyAddr.Port, nil
}

func (s *Stun) Read(ctx context.Context) (*stun.Message, error) {
	select {
	case buf := <-s.packetChan:
		// Linux kernel strips IP headers for both IPv4 and IPv6 raw sockets
		// We only receive: UDP header (8 bytes) + STUN payload
		// Note: BPF filter runs before IP header stripping, so it needs different offsets
		if len(buf) < 8 {
			return nil, fmt.Errorf("short packet: %d bytes, need at least a UDP header", len(buf))
		}
		m := &stun.Message{
			Raw: buf[8:], // Skip UDP header
		}

		if err := m.Decode(); err != nil {
			return nil, err
		}

		return m, nil
	case <-time.After(time.Duration(StunTimeout) * time.Second):
		return nil, ErrTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func createStunBindingPacket(srcPort, dstPort uint16) ([]byte, error) {
	// stun.TransactionID setter automatically generates a random transaction ID
	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return nil, err
	}

	packetLength := uint16(BindingPacketHeaderSize + len(msg.Raw))
	checksum := uint16(0)

	buf := make([]byte, BindingPacketHeaderSize)
	binary.BigEndian.PutUint16(buf[0:], srcPort)
	binary.BigEndian.PutUint16(buf[2:], dstPort)
	binary.BigEndian.PutUint16(buf[4:], packetLength)
	binary.BigEndian.PutUint16(buf[6:], checksum)

	return append(buf, msg.Raw...), nil
}

func stunBpfFilter(ctx context.Context, port uint16, protocol string) ([]bpf.RawInstruction, error) {
	logger := zerolog.Ctx(ctx)

	const stunMagicCookie = 0x2112A442

	var (
		ipOff              uint32
		udpOff             uint32
		payloadOff         uint32
		stunMagicCookieOff uint32
	)

	if protocol == "ipv6" {
		// For raw IPv6 sockets (ip6:17), BPF filter sees packets without IP header
		// (kernel strips it before BPF filter runs for IPv6)
		udpOff = 0                          // UDP header starts at offset 0
		payloadOff = udpOff + 8             // UDP header: always 8 bytes
		stunMagicCookieOff = payloadOff + 4 // STUN magic cookie is at payload + 4
	} else {
		// For raw IPv4 sockets (ip4:17), BPF filter sees full packet with IP header
		// (kernel strips IP header AFTER BPF filter runs for IPv4)
		ipOff = 0
		udpOff = ipOff + 20                 // IPv4 header: 20 bytes (no options)
		payloadOff = udpOff + 8             // UDP header: always 8 bytes
		stunMagicCookieOff = payloadOff + 4 // STUN transaction ID offset
	}

	r, e := bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute{
			// A = dst port
			Off:  udpOff + 2,
			Size: 2,
		},
		bpf.JumpIf{
			// if A == `port`
			Cond:      bpf.JumpEqual,
			Val:       uint32(port),
			SkipFalse: 3,
		},
		bpf.LoadAbsolute{
			// A = stun magic part
			Off:  stunMagicCookieOff,
			Size: 4,
		},
		bpf.JumpIf{
			// if A == stun magic value
			Cond:      bpf.JumpEqual,
			Val:       stunMagicCookie,
			SkipFalse: 1,
		},
		// we need
		bpf.RetConstant{
			Val: 262144,
		},
		// port and stun are not we need
		bpf.RetConstant{
			Val: 0,
		},
	})

	if e != nil {
		logger.Error().Err(e).Msg("failed to assemble BPF filter")
		return nil, e
	}

	return r, nil
}
