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
	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/net/route"
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

// firewallMark is accepted and ignored here: SO_MARK is a Linux socket option
// with no darwin/freebsd equivalent, so the STUN socket cannot be pinned to the
// device's routing path the way it is on Linux. ping_monitor_darwinbsd.go gives
// up on device binding for the same reason.
//
// listenInterfaces and listenDefaultRoute restrict which underlay interfaces to
// open (see resolveListenInterfaces). Empty list + false keeps the historical
// "open every eligible interface" behavior.
func New(ctx context.Context, excludeInterface string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (*Stun, error) {
	logger := zerolog.Ctx(ctx)

	interfaceNames, required, err := resolveListenInterfaces(logger, protocol, excludeInterface, listenInterfaces, listenDefaultRoute, net.Interfaces, defaultRouteInterface)
	if err != nil {
		return nil, err
	}

	logger.Debug().Msgf("listening on %d interface(s) (excluding %s): %v", len(interfaceNames), excludeInterface, interfaceNames)

	var handles []interfaceHandle

	// Create pcap handle for each selected interface
	for _, ifaceName := range interfaceNames {
		logger.Debug().Msgf("attempting to register OpenLive for interface: %s", ifaceName)
		handle, err := pcap.OpenLive(ctx, ifaceName, PacketSize, false, time.Duration(StunTimeout)*time.Second, pcap.DefaultSyscalls)
		if err != nil {
			// Tolerate open failures and move on; the daemon retries every
			// refresh cycle, so a transiently-down interface heals itself. An
			// interface the user named explicitly gets a louder warn (they
			// asked for it), the scan-all default stays at debug. If nothing
			// opens at all, the len(handles)==0 check below still errors.
			if required[ifaceName] {
				logger.Warn().Err(err).Msgf("requested interface %s could not be opened, skipping", ifaceName)
			} else {
				logger.Debug().Msgf("failed to open interface %s: %v", ifaceName, err)
			}
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
						buf, _, err = handle.handle.ReadPacketData()
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

// resolveListenInterfaces decides which underlay interfaces STUN discovery
// should open for the given protocol.
//
// With no selector (empty listenInterfaces and listenDefaultRoute false) it
// returns every eligible interface -- up, non-loopback, and not the WireGuard
// interface itself -- preserving the historical open-everything behavior.
//
// With a selector active it returns the union of the explicitly named
// interfaces and, when listenDefaultRoute is set, the default-route interface
// for this protocol (the two are additive, not mutually exclusive). Names
// absent from the system (typos) and a missing default route are warned and
// skipped rather than fatal; New's "no usable interfaces" check is the backstop
// when the union comes out empty. The returned required set marks the
// explicitly named interfaces so New can warn louder if one fails to open.
func resolveListenInterfaces(
	logger *zerolog.Logger,
	protocol, excludeInterface string,
	listenInterfaces []string,
	listenDefaultRoute bool,
	allInterfaces func() ([]net.Interface, error),
	defaultRoute func(protocol string) (string, error),
) ([]string, map[string]bool, error) {
	interfaces, err := allInterfaces()
	if err != nil {
		return nil, nil, err
	}

	if len(listenInterfaces) == 0 && !listenDefaultRoute {
		var eligible []string
		for _, iface := range interfaces {
			// Skip loopback, down, and the WireGuard interface we're excluding
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			if iface.Name == excludeInterface {
				continue
			}
			eligible = append(eligible, iface.Name)
		}
		if len(eligible) == 0 {
			return nil, nil, errors.New("no eligible interfaces found")
		}
		return eligible, nil, nil
	}

	present := make(map[string]bool, len(interfaces))
	for _, iface := range interfaces {
		present[iface.Name] = true
	}

	var names []string
	required := make(map[string]bool)
	seen := make(map[string]bool)
	add := func(name string, req bool) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
		if req {
			required[name] = true
		}
	}

	for _, name := range listenInterfaces {
		switch {
		case name == excludeInterface:
			logger.Warn().Msgf("listen_interfaces names the WireGuard interface %q itself; skipping (STUN cannot run through the tunnel)", name)
		case !present[name]:
			logger.Warn().Msgf("listen_interfaces names unknown interface %q; skipping", name)
		default:
			add(name, true)
		}
	}

	if listenDefaultRoute {
		name, err := defaultRoute(protocol)
		switch {
		case err != nil:
			logger.Warn().Err(err).Str("protocol", protocol).Msg("could not resolve default-route interface; skipping")
		case name == "":
			logger.Debug().Str("protocol", protocol).Msg("no default-route interface for protocol; skipping")
		case name == excludeInterface:
			logger.Debug().Msgf("default route points at the WireGuard interface %q; skipping", name)
		default:
			add(name, false)
		}
	}

	if len(names) == 0 {
		return nil, nil, fmt.Errorf("listen restriction for %s resolved to no interfaces", protocol)
	}

	return names, required, nil
}

// defaultRouteInterface returns the interface carrying the default route for
// the given protocol ("ipv4"/"ipv6"), read straight from the kernel routing
// table via a routing-socket RIB dump -- no exec, no netstat parsing. v4 and v6
// default routes can sit on different interfaces, hence per-protocol. An empty
// name with nil error means there simply is no default route for that family
// (a host may legitimately lack an IPv6 one).
func defaultRouteInterface(protocol string) (string, error) {
	af := syscall.AF_INET
	if protocol == "ipv6" {
		af = syscall.AF_INET6
	}

	rib, err := route.FetchRIB(af, route.RIBTypeRoute, 0)
	if err != nil {
		return "", err
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return "", err
	}

	for _, m := range msgs {
		rm, ok := m.(*route.RouteMessage)
		if !ok {
			continue
		}
		// A default route goes via a gateway and its destination is the
		// zero address (0.0.0.0 / ::). Addrs[0] is the destination.
		if rm.Flags&syscall.RTF_GATEWAY == 0 || rm.Flags&syscall.RTF_UP == 0 {
			continue
		}
		if len(rm.Addrs) == 0 || !isDefaultDestination(rm.Addrs[0]) {
			continue
		}
		iface, err := net.InterfaceByIndex(rm.Index)
		if err != nil {
			return "", err
		}
		return iface.Name, nil
	}

	return "", nil
}

func isDefaultDestination(a route.Addr) bool {
	switch v := a.(type) {
	case *route.Inet4Addr:
		return v.IP == [4]byte{}
	case *route.Inet6Addr:
		return v.IP == [16]byte{}
	default:
		return false
	}
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
