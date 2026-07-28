package mobilebind

import (
	"encoding/binary"
	"sync"
)

// STUN constants from RFC 8489.
const (
	stunHeaderSize  = 20
	stunMagicCookie = 0x2112A442
)

// IsSTUN reports whether the packet is STUN-shaped: first two bits zero,
// magic cookie present, and a length field consistent with the packet.
// WireGuard message types 1-4 never match (their first byte is 1-4 with the
// two high bits zero, but they carry no magic cookie at offset 4).
func IsSTUN(b []byte) bool {
	if len(b) < stunHeaderSize {
		return false
	}
	if b[0]&0xC0 != 0 {
		return false
	}
	if binary.BigEndian.Uint32(b[4:8]) != stunMagicCookie {
		return false
	}
	msgLen := int(binary.BigEndian.Uint16(b[2:4]))
	return msgLen%4 == 0 && stunHeaderSize+msgLen <= len(b)
}

// TxnID is a STUN transaction ID (RFC 8489 section 5).
type TxnID [12]byte

// TxnIDOf extracts the transaction ID; the caller must have checked IsSTUN.
func TxnIDOf(b []byte) TxnID {
	var id TxnID
	copy(id[:], b[8:20])
	return id
}

// TxnRegistry routes demuxed STUN responses to the transaction that sent the
// request. Unknown transactions are dropped, which also drops unsolicited
// STUN traffic.
type TxnRegistry struct {
	mu   sync.Mutex
	txns map[TxnID]chan []byte
}

func NewTxnRegistry() *TxnRegistry {
	return &TxnRegistry{txns: make(map[TxnID]chan []byte)}
}

// Register returns the channel the response for id will arrive on. The
// caller must Unregister when done.
func (r *TxnRegistry) Register(id TxnID) <-chan []byte {
	ch := make(chan []byte, 1)
	r.mu.Lock()
	r.txns[id] = ch
	r.mu.Unlock()
	return ch
}

func (r *TxnRegistry) Unregister(id TxnID) {
	r.mu.Lock()
	delete(r.txns, id)
	r.mu.Unlock()
}

// Dispatch delivers a STUN packet to its waiting transaction, returning
// false when no transaction matches. The packet is copied; the caller may
// reuse the buffer.
func (r *TxnRegistry) Dispatch(b []byte) bool {
	id := TxnIDOf(b)
	r.mu.Lock()
	ch, ok := r.txns[id]
	r.mu.Unlock()
	if !ok {
		return false
	}
	pkt := make([]byte, len(b))
	copy(pkt, b)
	select {
	case ch <- pkt:
	default:
	}
	return true
}
