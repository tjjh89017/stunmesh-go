//go:build !windows

package stun

import (
	"context"
	"encoding/binary"
	"net"

	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// getUDPAddressFamily, Connect and createStunBindingPacket were identical
// between stun_linux.go and stun_darwinbsd.go; they are platform-agnostic
// once s.writeTo and s.Read (each platform's own method on *Stun) supply the
// actual send/receive primitives. Windows has no socket-owning Stun (see
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

	packet, err := createStunBindingPacket(s.port, dst.Port())
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
