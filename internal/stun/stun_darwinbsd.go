//go:build darwin || freebsd

package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	pcap "github.com/packetcap/go-pcap"
	"github.com/pion/stun"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/route"
)

const PacketSize = 1600

type Stun struct {
	port       uint16
	payloadOff uint32
	conn       *ipv4.PacketConn
	handle     *pcap.Handle
	once       sync.Once
	packetChan chan *stun.Message
}

func New(ctx context.Context, port uint16) (*Stun, error) {

	// use default route to be the interface to listen stun
	defaultRouteInterface, err := getDefaultRouteInterface()
	if err != nil {
		return nil, err
	}

	handle, err := pcap.OpenLive(defaultRouteInterface, PacketSize, false, 0, pcap.DefaultSyscalls)
	if err != nil {
		return nil, err
	}

	//filter := fmt.Sprintf("udp dst port %d and udp[12:4] == 0x2112A442", port)
	//if err := handle.SetBPFFilter(filter); err != nil {
	//	return nil, err
	//}
	intf, err := net.InterfaceByName(defaultRouteInterface)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface by name %s: %w", defaultRouteInterface, err)
	}

	payloadOff := uint32(0)
	if intf.Flags&net.FlagLoopback != 0 {
		// TODO Loopback
		filter, err := stunLoopbackBpfFilter(ctx, port)
		if err != nil {
			return nil, fmt.Errorf("failed to create BPF filter for loopback: %w", err)
		}
		if err := handle.SetRawBPFFilter(filter); err != nil {
			return nil, fmt.Errorf("failed to set raw BPF filter: %w", err)
		}
		payloadOff = 0 + 4 + 5*4 + 2*4 // Null header + IPv4 header + UDP header
	} else {
		if err := handle.SetBPFFilter(fmt.Sprintf("udp dst port %d and udp[12:4] == 0x2112A442", port)); err != nil {
			return nil, fmt.Errorf("failed to set BPF filter: %w", err)
		}
		payloadOff = 0 + 14 + 5*4 + 2*4 // Ethernet header + IPv4 header + UDP header
	}

	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		return nil, err
	}

	conn := ipv4.NewPacketConn(c)

	return &Stun{
		port:       port,
		payloadOff: payloadOff,
		conn:       conn,
		handle:     handle,
		packetChan: make(chan *stun.Message),
	}, nil
}

func (s *Stun) Stop() error {
	close(s.packetChan)
	s.handle.Close()
	return s.conn.Close()
}

func (s *Stun) Start(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("starting to listen stun response")
	s.once.Do(func() {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(StunTimeout) * time.Second):
					return
				default:
					var (
						buf []byte
						err error
					)
					buf, _, err = s.handle.ReadPacketData()
					if err != nil {
						logger.Debug().Msgf("fail to read packet data")
						continue
					}
					// decode STUN
					m := &stun.Message{
						Raw: buf[s.payloadOff:],
					}
					if err := m.Decode(); err != nil {
						logger.Debug().Msgf("fail to decode stun msg")
						continue
					}
					s.packetChan <- m
					return
				}
			}
		}()
	})
}

func (s *Stun) Connect(ctx context.Context, stunAddr string) (_ string, _ int, err error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("connecting to STUN server: %s", stunAddr)
	addr, err := net.ResolveUDPAddr("udp4", stunAddr)
	if err != nil {
		return "", 0, err
	}

	packet, err := createStunBindingPacket(s.port, uint16(addr.Port))
	if err != nil {
		return "", 0, err
	}

	_, err = s.conn.WriteTo(packet, nil, addr)
	if err != nil {
		return "", 0, err
	}

	reply, err := s.Read(ctx)
	if err != nil {
		return "", 0, err
	}

	replyAddr := Parse(ctx, reply)

	return replyAddr.IP.String(), replyAddr.Port, nil
}

func (s *Stun) Read(ctx context.Context) (*stun.Message, error) {
	select {
	case m := <-s.packetChan:
		return m, nil
	case <-time.After(time.Duration(StunTimeout) * time.Second):
		return nil, ErrTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func createStunBindingPacket(srcPort, dstPort uint16) ([]byte, error) {
	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return nil, err
	}
	_ = msg.NewTransactionID()

	packetLength := uint16(BindingPacketHeaderSize + len(msg.Raw))
	checksum := uint16(0)

	buf := make([]byte, BindingPacketHeaderSize)
	binary.BigEndian.PutUint16(buf[0:], srcPort)
	binary.BigEndian.PutUint16(buf[2:], dstPort)
	binary.BigEndian.PutUint16(buf[4:], packetLength)
	binary.BigEndian.PutUint16(buf[6:], checksum)

	return append(buf, msg.Raw...), nil
}

func getDefaultRouteInterface() (string, error) {
	// Fetch the routing information base (RIB) for routes.
	rib, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		fmt.Println("Error fetching RIB:", err)
		return "", err
	}

	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		fmt.Println("Error parsing RIB:", err)
		return "", err
	}

	var dstIP string
	var netmask string
	var ifaceName string

	for _, msg := range msgs {
		switch m := msg.(type) {
		case *route.RouteMessage:
			// Extract destination address.
			if len(m.Addrs) > syscall.RTAX_NETMASK {
				switch addr := m.Addrs[syscall.RTAX_DST].(type) {
				case *route.Inet4Addr:
					dstIP = net.IPv4(addr.IP[0], addr.IP[1], addr.IP[2], addr.IP[3]).String()
				}
				switch addr := m.Addrs[syscall.RTAX_NETMASK].(type) {
				case *route.Inet4Addr:
					netmask = net.IPv4Mask(addr.IP[0], addr.IP[1], addr.IP[2], addr.IP[3]).String()
				}
			}

			// Extract interface index and name using the Index field.
			ifaceIndex := m.Index
			if ifaceIndex != 0 {
				iface, err := net.InterfaceByIndex(ifaceIndex)
				if err == nil {
					ifaceName = iface.Name
				} else {
					ifaceName = fmt.Sprintf("if%d", ifaceIndex)
				}
			}

			if dstIP == "0.0.0.0" && netmask == "00000000" {
				return ifaceName, nil
			}
		}
	}

	return "", errors.New("No default route found")
}

func stunLoopbackBpfFilter(ctx context.Context, port uint16) ([]bpf.RawInstruction, error) {
	const (
		nullOff            = 0
		ipOff              = nullOff + 4
		udpOff             = ipOff + 5*4
		payloadOff         = udpOff + 2*4
		stunMagicCookieOff = payloadOff + 4

		stunMagicCookie = 0x2112A442
	)

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
			Val: 262144, // Return the length of the packet
		},
		// port and stun are not we need
		bpf.RetConstant{
			Val: 0, // Return 0 to indicate no match
		},
	})

	logger := zerolog.Ctx(ctx)
	if e != nil {
		logger.Error().Err(e).Msg("failed to assemble BPF filter")
		return nil, e
	}

	return r, nil
}
