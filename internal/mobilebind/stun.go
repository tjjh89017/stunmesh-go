//go:build mobile

package mobilebind

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// STUN message types and attributes (RFC 8489).
const (
	stunBindingRequest  = 0x0001
	stunBindingSuccess  = 0x0101
	attrMappedAddress   = 0x0001
	attrXorMappedAddress = 0x0020
)

// Retransmit schedule: RTO 500ms doubling per RFC 8489 section 6.2.1,
// bounded by the context deadline.
const (
	stunInitialRTO = 500 * time.Millisecond
	stunMaxRetries = 4
)

var ErrStunTimeout = errors.New("stun: no response")

// Discover sends a STUN binding request to server ("host:port") from the
// shared WG socket and returns the reflexive address. network is "udp4" or
// "udp6" and selects the address family to discover. The response arrives
// through the demux path, so the mapping it reports is the one WG traffic
// uses.
func (b *Bind) Discover(ctx context.Context, network, server string) (netip.AddrPort, error) {
	udpAddr, err := net.ResolveUDPAddr(network, server)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("stun: resolve %s: %w", server, err)
	}
	dst := udpAddr.AddrPort()

	req, txn, err := buildBindingRequest()
	if err != nil {
		return netip.AddrPort{}, err
	}
	ch := b.registry.Register(txn)
	defer b.registry.Unregister(txn)

	rto := stunInitialRTO
	for attempt := 0; attempt < stunMaxRetries; attempt++ {
		if err := b.SendTo(dst, req); err != nil {
			return netip.AddrPort{}, fmt.Errorf("stun: send: %w", err)
		}
		timer := time.NewTimer(rto)
		select {
		case resp := <-ch:
			timer.Stop()
			return parseBindingResponse(resp, txn)
		case <-ctx.Done():
			timer.Stop()
			return netip.AddrPort{}, ctx.Err()
		case <-timer.C:
			rto *= 2
		}
	}
	return netip.AddrPort{}, ErrStunTimeout
}

func buildBindingRequest() ([]byte, TxnID, error) {
	var txn TxnID
	if _, err := rand.Read(txn[:]); err != nil {
		return nil, txn, fmt.Errorf("stun: txn id: %w", err)
	}
	msg := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(msg[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(msg[2:4], 0)
	binary.BigEndian.PutUint32(msg[4:8], stunMagicCookie)
	copy(msg[8:20], txn[:])
	return msg, txn, nil
}

func parseBindingResponse(msg []byte, txn TxnID) (netip.AddrPort, error) {
	if !IsSTUN(msg) || TxnIDOf(msg) != txn {
		return netip.AddrPort{}, errors.New("stun: not a matching response")
	}
	if binary.BigEndian.Uint16(msg[0:2]) != stunBindingSuccess {
		return netip.AddrPort{}, fmt.Errorf("stun: error response type %#04x", binary.BigEndian.Uint16(msg[0:2]))
	}

	attrs := msg[stunHeaderSize : stunHeaderSize+int(binary.BigEndian.Uint16(msg[2:4]))]
	for len(attrs) >= 4 {
		attrType := binary.BigEndian.Uint16(attrs[0:2])
		attrLen := int(binary.BigEndian.Uint16(attrs[2:4]))
		if 4+attrLen > len(attrs) {
			break
		}
		value := attrs[4 : 4+attrLen]
		switch attrType {
		case attrXorMappedAddress:
			return decodeAddress(value, txn, true)
		case attrMappedAddress:
			// Only a fallback: legacy servers without XOR-MAPPED-ADDRESS.
			return decodeAddress(value, txn, false)
		}
		// Attributes are padded to 4-byte boundaries.
		attrs = attrs[4+(attrLen+3)/4*4:]
	}
	return netip.AddrPort{}, errors.New("stun: no mapped address attribute")
}

func decodeAddress(value []byte, txn TxnID, xored bool) (netip.AddrPort, error) {
	if len(value) < 8 {
		return netip.AddrPort{}, errors.New("stun: short address attribute")
	}
	family := value[1]
	port := binary.BigEndian.Uint16(value[2:4])
	if xored {
		port ^= uint16(stunMagicCookie >> 16)
	}

	var rawAddr []byte
	switch family {
	case 0x01: // IPv4
		if len(value) < 8 {
			return netip.AddrPort{}, errors.New("stun: short IPv4 attribute")
		}
		rawAddr = append([]byte(nil), value[4:8]...)
		if xored {
			var cookie [4]byte
			binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
			for i := range rawAddr {
				rawAddr[i] ^= cookie[i]
			}
		}
	case 0x02: // IPv6: xor with magic cookie followed by the txn id
		if len(value) < 20 {
			return netip.AddrPort{}, errors.New("stun: short IPv6 attribute")
		}
		rawAddr = append([]byte(nil), value[4:20]...)
		if xored {
			var mask [16]byte
			binary.BigEndian.PutUint32(mask[0:4], stunMagicCookie)
			copy(mask[4:], txn[:])
			for i := range rawAddr {
				rawAddr[i] ^= mask[i]
			}
		}
	default:
		return netip.AddrPort{}, fmt.Errorf("stun: unknown address family %#02x", family)
	}

	addr, ok := netip.AddrFromSlice(rawAddr)
	if !ok {
		return netip.AddrPort{}, errors.New("stun: invalid address")
	}
	if port == 0 || !addr.IsValid() {
		return netip.AddrPort{}, errors.New("stun: invalid mapped endpoint")
	}
	return netip.AddrPortFrom(addr, port), nil
}
