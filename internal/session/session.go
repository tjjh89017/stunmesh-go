package session

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pion/stun"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
)

type Session struct {
	conn       *ipv4.PacketConn
	once       sync.Once
	packetChan chan []byte
}

func New(filter ...bpf.RawInstruction) (*Session, error) {
	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		return nil, err
	}

	conn := ipv4.NewPacketConn(c)
	if err := conn.SetBPF(filter); err != nil {
		return nil, err
	}

	return &Session{
		conn:       conn,
		packetChan: make(chan []byte),
	}, nil
}

func (s *Session) Stop() {
	close(s.packetChan)
	_ = s.conn.Close()
}

func (s *Session) Start(ctx context.Context) {
	s.once.Do(func() {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					buf := make([]byte, 1500)
					n, _, _, err := s.conn.ReadFrom(buf)
					if err != nil {
						continue
					}
					s.packetChan <- buf[:n]
				}
			}
		}()
	})
}

func (s *Session) Bind(ctx context.Context, stunAddr string, port uint16) (string, int, error) {
	log.Printf("connecting to STUN server: %s\n", stunAddr)
	addr, err := net.ResolveUDPAddr("udp4", stunAddr)
	if err != nil {
		return "", 0, err
	}

	packet, err := createStunBindingPacket(port, uint16(addr.Port))
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

	replyAddr := Parse(reply)

	return replyAddr.IP.String(), replyAddr.Port, nil
}

func (s *Session) Read(ctx context.Context) (*stun.Message, error) {
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
