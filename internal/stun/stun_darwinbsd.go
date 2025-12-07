//go:build darwin || freebsd

package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"

	pcap "github.com/packetcap/go-pcap"
	"github.com/pion/stun"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const PacketSize = 1600

type interfaceHandle struct {
	name       string
	handle     *pcap.Handle
	payloadOff uint32
}

type Stun struct {
	port       uint16
	protocol   string
	conn4      *ipv4.PacketConn
	conn6      *ipv6.PacketConn
	handles    []interfaceHandle
	once       sync.Once
	packetChan chan *stun.Message
	waitGroup  sync.WaitGroup
}

func calculatePayloadOffset(linkType uint32, protocol string) uint32 {
	if linkType == pcap.LinkTypeNull {
		if protocol == "ipv6" {
			return 4 + 40 + 8 // Null header + IPv6 header + UDP header
		}
		return 4 + 20 + 8 // Null header + IPv4 header + UDP header
	}
	// Ethernet
	if protocol == "ipv6" {
		return 14 + 40 + 8 // Ethernet header + IPv6 header + UDP header
	}
	return 14 + 20 + 8 // Ethernet header + IPv4 header + UDP header
}

func configureBPFFilter(ctx context.Context, linkType uint32, port uint16, protocol, ifaceName string, logger *zerolog.Logger) ([]bpf.RawInstruction, uint32, error) {
	var filter []bpf.RawInstruction
	var err error

	if linkType == pcap.LinkTypeNull {
		filter, err = stunNullBpfFilter(ctx, port, protocol)
		if err != nil {
			logger.Debug().Msgf("failed to create BPF filter for Null/loopback interface %s: %v", ifaceName, err)
			return nil, 0, err
		}
		logger.Debug().Msgf("set raw BPF filter for Null/loopback interface %s", ifaceName)
	} else {
		filter, err = stunEthernetBpfFilter(ctx, port, protocol)
		if err != nil {
			logger.Debug().Msgf("failed to create raw BPF filter for interface %s: %v", ifaceName, err)
			return nil, 0, err
		}
		logger.Debug().Msgf("set raw BPF filter for interface %s", ifaceName)
	}

	payloadOff := calculatePayloadOffset(linkType, protocol)
	return filter, payloadOff, nil
}

func New(ctx context.Context, excludeInterface string, port uint16, protocol string) (*Stun, error) {
	logger := zerolog.Ctx(ctx)

	// Get all eligible interfaces (excluding the specific WireGuard interface)
	interfaceNames, err := getAllEligibleInterfaces(excludeInterface)
	if err != nil {
		return nil, err
	}

	logger.Debug().Msgf("found %d eligible interfaces (excluding %s): %v", len(interfaceNames), excludeInterface, interfaceNames)

	var handles []interfaceHandle

	// Create pcap handle for each eligible interface
	for _, ifaceName := range interfaceNames {
		logger.Debug().Msgf("attempting to register OpenLive for interface: %s", ifaceName)
		handle, err := pcap.OpenLive(ifaceName, PacketSize, false, 0, pcap.DefaultSyscalls)
		if err != nil {
			logger.Debug().Msgf("failed to open interface %s: %v", ifaceName, err)
			// Skip interfaces that can't be opened
			continue
		}

		linkType := handle.LinkType()
		filter, payloadOff, err := configureBPFFilter(ctx, linkType, port, protocol, ifaceName, logger)
		if err != nil {
			handle.Close()
			continue
		}
		if err := handle.SetRawBPFFilter(filter); err != nil {
			logger.Debug().Msgf("failed to set raw BPF filter for interface %s: %v", ifaceName, err)
			handle.Close()
			continue
		}

		handles = append(handles, interfaceHandle{
			name:       ifaceName,
			handle:     handle,
			payloadOff: payloadOff,
		})
		logger.Debug().Msgf("successfully registered OpenLive for interface: %s (payloadOff: %d)", ifaceName, payloadOff)
	}

	if len(handles) == 0 {
		logger.Error().Msg("no usable interfaces found after attempting to open all eligible interfaces")
		return nil, errors.New("no usable interfaces found")
	}

	logger.Info().Msgf("successfully registered OpenLive for %d interfaces", len(handles))

	c, err := createRawSocket(protocol, handles, *logger)
	if err != nil {
		return nil, err
	}

	s := &Stun{
		port:       port,
		protocol:   protocol,
		handles:    handles,
		packetChan: make(chan *stun.Message),
	}

	if err := setPacketConn(s, c); err != nil {
		return nil, err
	}

	return s, nil
}

func createRawSocket(protocol string, handles []interfaceHandle, logger zerolog.Logger) (net.PacketConn, error) {
	network := "ip4:17"
	bindAddr := "0.0.0.0"
	if protocol == "ipv6" {
		network = "ip6:17"
		bindAddr = "::"
		logger.Debug().Msg("creating IPv6 raw socket")
	} else {
		logger.Debug().Msg("creating IPv4 raw socket")
	}

	c, err := net.ListenPacket(network, bindAddr)
	if err != nil {
		// Close all handles on error
		for _, ih := range handles {
			ih.handle.Close()
		}
		return nil, err
	}
	return c, nil
}

func setPacketConn(s *Stun, c net.PacketConn) error {
	if s.protocol == "ipv6" {
		conn6 := ipv6.NewPacketConn(c)
		// Enable kernel checksum calculation for UDP (checksum at offset 6)
		if err := conn6.SetChecksum(true, 6); err != nil {
			return err
		}
		s.conn6 = conn6
	} else {
		s.conn4 = ipv4.NewPacketConn(c)
	}
	return nil
}

func (s *Stun) Stop() error {
	s.waitGroup.Wait()
	close(s.packetChan)
	if s.protocol == "ipv6" {
		return s.conn6.Close()
	}
	return s.conn4.Close()
}

func (s *Stun) Start(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("starting to listen stun response on %d interfaces", len(s.handles))
	s.once.Do(func() {
		// Start a goroutine for each interface handle
		s.waitGroup.Add(len(s.handles))
		for _, ih := range s.handles {
			logger.Debug().Msgf("start handle for interface: %s", ih.name)
			go func(handle interfaceHandle) {
				defer func() {
					handle.handle.Close()
					logger.Debug().Msgf("closed handle for interface: %s", handle.name)
					s.waitGroup.Done()
				}()
				timeout := time.After(time.Duration(StunTimeout) * time.Second)
				for {
					select {
					case <-ctx.Done():
						return
					case <-timeout:
						return
					default:
						var (
							buf []byte
							err error
						)
						buf, _, err = handle.handle.ReadPacketDataWithTimeout(time.Duration(StunTimeout) * time.Second)
						if err != nil {
							logger.Trace().Msgf("fail to read packet data from %s, err %v", handle.name, err)
							continue
						}
						// decode STUN
						m := &stun.Message{
							Raw: buf[handle.payloadOff:],
						}
						if err := m.Decode(); err != nil {
							logger.Debug().Msgf("fail to decode stun msg from %s", handle.name)
							continue
						}
						select {
						case s.packetChan <- m:
						case <-ctx.Done():
							return
						}
						return
					}
				}
			}(ih)
		}
	})
}

func (s *Stun) getUDPAddressFamily() string {
	if s.protocol == "ipv6" {
		return "udp6"
	}
	return "udp4"
}

func (s *Stun) writeTo(packet []byte, addr net.Addr) (int, error) {
	if s.protocol == "ipv6" {
		return s.conn6.WriteTo(packet, nil, addr)
	}
	return s.conn4.WriteTo(packet, nil, addr)
}

func (s *Stun) Connect(ctx context.Context, stunAddr string) (_ string, _ int, err error) {
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

func getAllEligibleInterfaces(excludeInterface string) ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var eligible []string
	for _, iface := range interfaces {
		// Skip loopback, down, and the specific WireGuard interface we're excluding
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Name == excludeInterface {
			continue
		}
		eligible = append(eligible, iface.Name)
	}

	if len(eligible) == 0 {
		return nil, errors.New("no eligible interfaces found")
	}

	return eligible, nil
}

func stunNullBpfFilter(ctx context.Context, port uint16, protocol string) ([]bpf.RawInstruction, error) {
	logger := zerolog.Ctx(ctx)

	const stunMagicCookie = 0x2112A442

	var (
		nullOff            uint32 = 0
		ipOff              uint32
		udpOff             uint32
		payloadOff         uint32
		stunMagicCookieOff uint32
		protocolValue      uint32
	)

	if protocol == "ipv6" {
		// IPv6 can be 24, 28, or 30 in BSD Null header (host byte order)
		// Using big-endian: 0x18000000, 0x1C000000, 0x1E000000
		ipOff = nullOff + 4
		udpOff = ipOff + 40
		payloadOff = udpOff + 8
		stunMagicCookieOff = payloadOff + 4

		// BPF filter accepting any of the three IPv6 values (24, 28, 30)
		r, e := bpf.Assemble([]bpf.Instruction{
			bpf.LoadAbsolute{
				Off:  nullOff,
				Size: 4,
			},
			// Check if A == 24 (0x18000000)
			bpf.JumpIf{
				Cond:     bpf.JumpEqual,
				Val:      0x18000000,
				SkipTrue: 2,
			},
			// Check if A == 28 (0x1C000000)
			bpf.JumpIf{
				Cond:     bpf.JumpEqual,
				Val:      0x1C000000,
				SkipTrue: 1,
			},
			// Check if A == 30 (0x1E000000)
			bpf.JumpIf{
				Cond:      bpf.JumpEqual,
				Val:       0x1E000000,
				SkipFalse: 5,
			},
			bpf.LoadAbsolute{
				// A = UDP dst port
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
				// A = STUN magic cookie
				Off:  stunMagicCookieOff,
				Size: 4,
			},
			bpf.JumpIf{
				// if A == STUN magic cookie
				Cond:      bpf.JumpEqual,
				Val:       stunMagicCookie,
				SkipFalse: 1,
			},
			bpf.RetConstant{
				Val: 262144,
			},
			bpf.RetConstant{
				Val: 0,
			},
		})

		if e != nil {
			logger.Error().Err(e).Msg("failed to assemble BPF filter")
			return nil, e
		}

		return r, nil
	} else {
		// IPv4 is 2 (0x02000000 in big-endian)
		protocolValue = 0x02000000
		ipOff = nullOff + 4
		udpOff = ipOff + 20
		payloadOff = udpOff + 8
		stunMagicCookieOff = payloadOff + 4
	}

	r, e := bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute{
			// A = protocol type
			Off:  nullOff,
			Size: 4,
		},
		bpf.JumpIf{
			// if A == protocol value
			Cond:      bpf.JumpEqual,
			Val:       protocolValue,
			SkipFalse: 5,
		},
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

func stunEthernetBpfFilter(ctx context.Context, port uint16, protocol string) ([]bpf.RawInstruction, error) {
	logger := zerolog.Ctx(ctx)

	const stunMagicCookie = 0x2112A442

	var (
		ethernetOff        uint32 = 0
		ipOff              uint32
		udpOff             uint32
		payloadOff         uint32
		stunMagicCookieOff uint32
	)

	if protocol == "ipv6" {
		ipOff = ethernetOff + 14
		udpOff = ipOff + 40
		payloadOff = udpOff + 8
		stunMagicCookieOff = payloadOff + 4

		// Need to check EtherType for IPv6 (0x86DD)
		r, e := bpf.Assemble([]bpf.Instruction{
			bpf.LoadAbsolute{
				// A = EtherType
				Off:  12,
				Size: 2,
			},
			bpf.JumpIf{
				// if A == 0x86DD (IPv6)
				Cond:      bpf.JumpEqual,
				Val:       0x86DD,
				SkipFalse: 5,
			},
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
			bpf.RetConstant{
				Val: 262144,
			},
			bpf.RetConstant{
				Val: 0,
			},
		})

		if e != nil {
			logger.Error().Err(e).Msg("failed to assemble ethernet BPF filter for IPv6")
			return nil, e
		}
		return r, nil
	} else {
		// IPv4
		ipOff = ethernetOff + 14
		udpOff = ipOff + 20
		payloadOff = udpOff + 8
		stunMagicCookieOff = payloadOff + 4

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
			bpf.RetConstant{
				Val: 262144,
			},
			bpf.RetConstant{
				Val: 0,
			},
		})

		if e != nil {
			logger.Error().Err(e).Msg("failed to assemble ethernet BPF filter")
			return nil, e
		}

		return r, nil
	}
}
