//go:build !windows

package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// The send path, the reader loop and the reply wait are shared between
// stun_linux.go and stun_darwinbsd.go; each platform supplies s.writeTo and
// the raw read that Start hands to pump. Windows has no socket-owning Stun (see
// stun_windows.go), so this file is excluded there rather than folded into
// the untagged helper.go.

func (s *Stun) getUDPAddressFamily() string {
	if s.protocol == "ipv6" {
		return "udp6"
	}
	return "udp4"
}

func (s *Stun) Connect(ctx context.Context, stunAddr string) (string, int, error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("connecting to STUN server: %s", stunAddr)

	// Resolved through the dialer's escaped resolver, not net.ResolveUDPAddr:
	// the probe itself already escapes a covering tunnel (raw socket carrying
	// the device's fwmark, or pcap), but a stdlib lookup opens an unmarked UDP
	// socket that a covering allowed-IPs route sends into the very tunnel
	// discovery is trying to establish. ctx carries the escape; see
	// ctrl.PublishController.discoverEndpoints.
	dst, err := dialer.ResolveAddrPort(ctx, s.getUDPAddressFamily(), stunAddr)
	if err != nil {
		return "", 0, err
	}
	addr := net.UDPAddrFromAddrPort(dst)

	packet, txID, err := createStunBindingPacket(s.port, dst.Port())
	if err != nil {
		return "", 0, err
	}

	if _, err = s.writeTo(packet, addr); err != nil {
		return "", 0, err
	}

	reply, err := s.awaitReply(ctx, txID)
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

// awaitReply returns the first message carrying txID, dropping any other
// (a late reply to an earlier server, a stray request), until StunTimeout
// after the send.
func (s *Stun) awaitReply(ctx context.Context, txID [stun.TransactionIDSize]byte) (*stun.Message, error) {
	readCtx, cancel := context.WithTimeout(ctx, StunTimeout)
	defer cancel()

	for {
		reply, err := s.Read(readCtx)
		if err != nil {
			if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				return nil, ErrTimeout
			}
			return nil, err
		}
		if reply.TransactionID != txID {
			zerolog.Ctx(ctx).Debug().Msg("dropping STUN message with a foreign transaction ID")
			continue
		}
		return reply, nil
	}
}

// Read waits for the next decoded STUN message until ctx ends.
func (s *Stun) Read(ctx context.Context) (*stun.Message, error) {
	select {
	case m := <-s.packetChan:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// errReaderClosed is returned by a pump source whose socket or handle is gone.
var errReaderClosed = errors.New("stun reader closed")

// readRetryDelay spaces out read errors other than a poll timeout, which can
// repeat at once (a bpf device whose interface went away).
const readRetryDelay = 50 * time.Millisecond

// pump decodes what read returns and hands each STUN message to packetChan
// until Stop, ctx ends, or read returns errReaderClosed. read must block until
// a packet arrives or report context.DeadlineExceeded after a poll timeout;
// pump retries that at once and waits readRetryDelay after any other error.
func (s *Stun) pump(ctx context.Context, name string, read func() ([]byte, error), payloadOff uint32) {
	logger := zerolog.Ctx(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		buf, err := read()
		if err != nil {
			if errors.Is(err, errReaderClosed) {
				return
			}
			logger.Trace().Msgf("fail to read packet data from %s, err %v", name, err)
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			select {
			case <-time.After(readRetryDelay):
			case <-ctx.Done():
				return
			case <-s.done:
				return
			}
			continue
		}
		m, err := decodeStunPacket(buf, payloadOff)
		if err != nil {
			if errors.Is(err, errShortPacket) {
				logger.Trace().Msgf("%v from %s", err, name)
			} else {
				logger.Debug().Msgf("fail to decode stun msg from %s", name)
			}
			continue
		}

		select {
		case s.packetChan <- m:
		case <-ctx.Done():
			return
		case <-s.done:
			return
		}
	}
}

// errShortPacket marks decodeStunPacket's short-packet case so callers can
// distinguish it from a STUN decode failure and log each at its own level.
var errShortPacket = errors.New("short packet")

// decodeStunPacket validates that buf is long enough to contain the STUN
// payload at payloadOff, then decodes it. A read can hand back a short or
// empty buffer with a nil error (a timed-out pcap read yields len 0), so the
// payload offset is not safe to slice with until it is known to be in range.
func decodeStunPacket(buf []byte, payloadOff uint32) (*stun.Message, error) {
	if uint32(len(buf)) < payloadOff {
		return nil, fmt.Errorf("%w: %d bytes, need %d", errShortPacket, len(buf), payloadOff)
	}

	m := &stun.Message{
		Raw: buf[payloadOff:],
	}
	if err := m.Decode(); err != nil {
		return nil, err
	}

	return m, nil
}

func createStunBindingPacket(srcPort, dstPort uint16) ([]byte, [stun.TransactionIDSize]byte, error) {
	// stun.TransactionID setter automatically generates a random transaction ID
	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return nil, [stun.TransactionIDSize]byte{}, err
	}

	packetLength := uint16(BindingPacketHeaderSize + len(msg.Raw))
	checksum := uint16(0)

	buf := make([]byte, BindingPacketHeaderSize)
	binary.BigEndian.PutUint16(buf[0:], srcPort)
	binary.BigEndian.PutUint16(buf[2:], dstPort)
	binary.BigEndian.PutUint16(buf[4:], packetLength)
	binary.BigEndian.PutUint16(buf[6:], checksum)

	return append(buf, msg.Raw...), msg.TransactionID, nil
}
