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
)

const PacketSize = 1600

type interfaceHandle struct {
	name       string
	handle     *pcap.Handle
	payloadOff uint32
}

type Stun struct {
	port       uint16
	conn       *ipv4.PacketConn
	handles    []interfaceHandle
	once       sync.Once
	packetChan chan *stun.Message
	waitGroup  sync.WaitGroup
}

func New(ctx context.Context, excludeInterface string, port uint16) (*Stun, error) {
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

		var payloadOff uint32
		linkType := handle.LinkType()
		if linkType == pcap.LinkTypeNull {
			filter, err := stunNullBpfFilter(ctx, port)
			if err != nil {
				logger.Debug().Msgf("failed to create BPF filter for Null/loopback interface %s: %v", ifaceName, err)
				handle.Close()
				continue
			}
			if err := handle.SetRawBPFFilter(filter); err != nil {
				logger.Debug().Msgf("failed to set raw BPF filter for Null/loopback interface %s: %v", ifaceName, err)
				handle.Close()
				continue
			}
			logger.Debug().Msgf("set raw BPF filter for Null/loopback interface %s", ifaceName)
			payloadOff = 0 + 4 + 5*4 + 2*4 // Null header + IPv4 header + UDP header
		} else {
			filter, err := stunEthernetBpfFilter(ctx, port)
			if err != nil {
				logger.Debug().Msgf("failed to create raw BPF filter for interface %s: %v", ifaceName, err)
				handle.Close()
				continue
			}
			if err := handle.SetRawBPFFilter(filter); err != nil {
				logger.Debug().Msgf("failed to set raw BPF filter for interface %s: %v", ifaceName, err)
				handle.Close()
				continue
			}
			logger.Debug().Msgf("set raw BPF filter for interface %s", ifaceName)
			payloadOff = 0 + 14 + 5*4 + 2*4 // Ethernet header + IPv4 header + UDP header
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

	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		// Close all handles on error
		for _, ih := range handles {
			ih.handle.Close()
		}
		return nil, err
	}

	conn := ipv4.NewPacketConn(c)

	return &Stun{
		port:       port,
		conn:       conn,
		handles:    handles,
		packetChan: make(chan *stun.Message),
	}, nil
}

func (s *Stun) Stop() error {
	s.waitGroup.Wait()
	close(s.packetChan)
	return s.conn.Close()
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

func stunNullBpfFilter(ctx context.Context, port uint16) ([]bpf.RawInstruction, error) {
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
			// A = protocol type
			Off:  nullOff,
			Size: 4,
		},
		bpf.JumpIf{
			// if A == 0x02000000 // Null header for IPv4
			Cond:      bpf.JumpEqual,
			Val:       0x02000000,
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

func stunEthernetBpfFilter(ctx context.Context, port uint16) ([]bpf.RawInstruction, error) {
	const (
		ethernetOff        = 0
		ipOff              = ethernetOff + 14
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
		logger.Error().Err(e).Msg("failed to assemble ethernet BPF filter")
		return nil, e
	}

	return r, nil
}
