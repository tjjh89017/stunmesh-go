// build +windows
package stun

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"time"

	"github.com/pion/stun"
	"github.com/rs/zerolog"
	"golang.org/x/sys/windows"
)

const PacketSize = 1500

const (
	ipOff      = 0
	udpOff     = ipOff + 5*4
	payloadOff = udpOff + 2*4
)

type Stun struct {
	port       uint16
	once       sync.Once
	sock       windows.Handle
	packetChan chan *stun.Message
}

func New(ctx context.Context, port uint16) (*Stun, error) {
	logger := zerolog.Ctx(ctx)

	logger.Debug().Msgf("Port: %v", port)

	sock, err := windows.Socket(windows.AF_INET, windows.SOCK_RAW, windows.IPPROTO_UDP)
	if err != nil {
		logger.Error().Msgf("socket err: %v", err)
		return nil, err
	}
	logger.Debug().Msgf("sock: %v", sock)

	err = windows.SetsockoptInet4Addr(sock, windows.SOL_SOCKET, windows.SO_REUSEADDR, [4]byte{1, 1, 1, 1})
	if err != nil {
		logger.Error().Msgf("set sock opt err: %v", err)
	}

	err = windows.Bind(sock, &windows.SockaddrInet4{
		Port: int(port),
	})
	if err != nil {
		logger.Error().Msgf("bind err %v", err)
	}

	return &Stun{
		port:       port,
		sock:       sock,
		packetChan: make(chan *stun.Message),
	}, nil
}

func (s *Stun) Stop() error {
	close(s.packetChan)
	return windows.CloseHandle(s.sock)
}

func (s *Stun) Start(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("starting to listen stun response")
	buf := make([]byte, PacketSize)
	s.once.Do(func() {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return

				//case <-time.After(time.Duration(StunTimeout+5) * time.Second):
				//return
				default:
					n, addr, err := windows.Recvfrom(s.sock, buf, 0)
					if err != nil {
						continue
					}
					logger.Debug().Msgf("buf: %v", hex.EncodeToString(buf[:n]))
					logger.Debug().Msgf("addr: %v", addr)
					// decode STUN
					m := &stun.Message{
						Raw: buf[payloadOff:n],
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

	// TODO
	/*
		if err = windows.Connect(s.sock, &windows.SockaddrInet4{
			Addr: [4]byte(addr.IP),
			Port: addr.Port,
		}); err != nil {
			logger.Error().Msgf("connect err: %v", err)
		}*/

	err = windows.Sendto(s.sock, packet, 0, &windows.SockaddrInet4{
		Addr: [4]byte(addr.IP),
		Port: addr.Port,
	})
	if err != nil {
		return "", 0, err
	}

	reply, err := s.Read(ctx)
	if err != nil {
		return "", 0, err
	}

	replyAddr := Parse(ctx, reply)

	return replyAddr.String(), replyAddr.Port, nil
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
