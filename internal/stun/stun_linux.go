// build +linux
package stun

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/pion/stun"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
)

const PacketSize = 1500

type Stun struct {
	port       uint16
	conn       *ipv4.PacketConn
	once       sync.Once
	packetChan chan []byte
}

func New(ctx context.Context, excludeInterface string, port uint16) (*Stun, error) {
	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		return nil, err
	}

	filter, err := stunBpfFilter(ctx, port)
	if err != nil {
		return nil, err
	}

	conn := ipv4.NewPacketConn(c)
	if err := conn.SetBPF(filter); err != nil {
		return nil, err
	}

	return &Stun{
		port:       port,
		conn:       conn,
		packetChan: make(chan []byte),
	}, nil
}

func (s *Stun) Stop() error {
	close(s.packetChan)
	return s.conn.Close()
}

func (s *Stun) Start(ctx context.Context) {
	s.once.Do(func() {
		go func() {
			timeout := time.After(time.Duration(StunTimeout+5) * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-timeout:
					return
				default:
					buf := make([]byte, PacketSize)
					n, _, _, err := s.conn.ReadFrom(buf)
					if err != nil {
						continue
					}
					select {
					case s.packetChan <- buf[:n]:
						return
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	})
}

func (s *Stun) Connect(ctx context.Context, stunAddr string) (string, int, error) {
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
	case buf := <-s.packetChan:
		m := &stun.Message{
			Raw: buf[8:],
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

func stunBpfFilter(ctx context.Context, port uint16) ([]bpf.RawInstruction, error) {
	// if possible make some magic here to determine STUN packet
	const (
		ipOff              = 0
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
			Val: 262144,
		},
		// port and stun are not we need
		bpf.RetConstant{
			Val: 0,
		},
	})

	logger := zerolog.Ctx(ctx)
	if e != nil {
		logger.Error().Err(e).Msg("failed to assemble BPF filter")
		return nil, e
	}

	return r, nil
}
