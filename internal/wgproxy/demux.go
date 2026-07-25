// Package wgproxy implements the portable core of the UDP proxy that fronts a
// WireGuard interface on platforms without raw-socket port sharing (Windows).
// This file holds the inbound packet classifier and the STUN transaction
// registry; the socket relay itself lives in proxy.go.
package wgproxy

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// PeerKey is a WireGuard peer public key. It aliases [32]byte so values are
// interchangeable with wg.Key without importing that package.
type PeerKey = [32]byte

// TxnID is a STUN 96-bit transaction ID (RFC 8489 section 5).
type TxnID [12]byte

const (
	stunHeaderLen   = 20
	stunMagicCookie = 0x2112A442

	stunBindingSuccess = 0x0101
	stunBindingError   = 0x0111
)

// ErrTxnExists is returned by TxnRegistry.Register when the transaction ID is
// already pending.
var ErrTxnExists = errors.New("wgproxy: transaction already registered")

// Bucket is the demux classification of an inbound outer-socket packet.
type Bucket uint8

const (
	// BucketSTUN marks a STUN-shaped packet. It was consumed here: either
	// routed to the registered waiter or dropped (see Demux.DroppedSTUN).
	BucketSTUN Bucket = iota + 1
	// BucketRelay marks a packet from a programmed peer mapping; the caller
	// forwards it unmodified to Decision.Peer's inner socket.
	BucketRelay
	// BucketDrop marks an unattributable packet; the caller must not forward
	// it anywhere.
	BucketDrop
)

// Decision is the outcome of classifying one inbound packet. Peer is set only
// when Bucket is BucketRelay.
type Decision struct {
	Bucket Bucket
	Peer   PeerKey
}

// TxnRegistry tracks pending STUN transactions and routes binding responses
// (success and error alike) to the goroutine waiting on each transaction.
// Timeouts are the caller's job: wait with select on the reply channel,
// ctx.Done(), and time.After — never with socket read deadlines.
type TxnRegistry struct {
	mu      sync.Mutex
	pending map[TxnID]pendingTxn
}

type pendingTxn struct {
	server netip.AddrPort
	reply  chan []byte
}

func NewTxnRegistry() *TxnRegistry {
	return &TxnRegistry{pending: make(map[TxnID]pendingTxn)}
}

// Register records a pending transaction and returns the channel its response
// will be delivered on. server is the STUN server the request is sent to;
// responses from any other source address are rejected (RFC 8489
// section 7.2.3). The caller must Unregister when done.
func (r *TxnRegistry) Register(id TxnID, server netip.AddrPort) (<-chan []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.pending[id]; ok {
		return nil, ErrTxnExists
	}
	txn := pendingTxn{
		server: normalize(server),
		reply:  make(chan []byte, 1),
	}
	r.pending[id] = txn
	return txn.reply, nil
}

// Unregister removes a pending transaction. Responses arriving afterwards are
// dropped. It is safe to call for an ID that is not registered.
func (r *TxnRegistry) Unregister(id TxnID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, id)
}

// route delivers a STUN-shaped packet to its waiter. It returns false when the
// packet is not a binding response, no matching transaction is pending, or the
// source address is not the expected server.
func (r *TxnRegistry) route(src netip.AddrPort, b []byte) bool {
	msgType := binary.BigEndian.Uint16(b[0:2])
	if msgType != stunBindingSuccess && msgType != stunBindingError {
		return false
	}
	var id TxnID
	copy(id[:], b[8:stunHeaderLen])

	r.mu.Lock()
	defer r.mu.Unlock()
	txn, ok := r.pending[id]
	if !ok || txn.server != normalize(src) {
		return false
	}
	// Copy: the caller reuses its receive buffer for the next datagram.
	packet := make([]byte, len(b))
	copy(packet, b)
	select {
	case txn.reply <- packet:
		return true
	default:
		// The waiter already has an undrained response; treat as unmatched.
		return false
	}
}

// Demux classifies packets read from an outer socket with a fixed three-bucket
// rule, in order: STUN-shaped (handed to the TxnRegistry), programmed peer
// relay, drop. Peer mappings change only through Program/Unprogram
// (establish-driven) — never from inbound packet source addresses.
type Demux struct {
	txns   *TxnRegistry
	logger zerolog.Logger

	mu        sync.RWMutex
	peerBySrc map[netip.AddrPort]PeerKey
	srcByPeer map[PeerKey]netip.AddrPort

	droppedSTUN  atomic.Uint64
	droppedOther atomic.Uint64
}

func NewDemux(logger *zerolog.Logger) *Demux {
	return &Demux{
		txns:      NewTxnRegistry(),
		logger:    logger.With().Str("component", "wgproxy.demux").Logger(),
		peerBySrc: make(map[netip.AddrPort]PeerKey),
		srcByPeer: make(map[PeerKey]netip.AddrPort),
	}
}

// Registry exposes the transaction registry so the proxy's STUN exchange can
// register waiters on it.
func (d *Demux) Registry() *TxnRegistry {
	return d.txns
}

// Program maps a peer's outer source address to the peer, replacing any
// previous mapping for that peer. Called from the establish flow only.
func (d *Demux) Program(peer PeerKey, remote netip.AddrPort) {
	remote = normalize(remote)
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.srcByPeer[peer]; ok {
		delete(d.peerBySrc, old)
	}
	d.srcByPeer[peer] = remote
	d.peerBySrc[remote] = peer
}

// Unprogram removes a peer's mapping. Safe to call for an unknown peer.
func (d *Demux) Unprogram(peer PeerKey) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.srcByPeer[peer]; ok {
		delete(d.peerBySrc, old)
		delete(d.srcByPeer, peer)
	}
}

// Classify applies the three-bucket rule to one packet read from an outer
// socket. STUN-shaped packets are consumed here (routed to the waiter or
// dropped); relay packets carry the destination peer in the decision; anything
// else is dropped. b is only read during the call — routed STUN responses are
// copied, so the caller may reuse the buffer immediately.
func (d *Demux) Classify(src netip.AddrPort, b []byte) Decision {
	if isSTUNShaped(b) {
		if !d.txns.route(src, b) {
			d.countDrop(&d.droppedSTUN, src, "dropped unmatched STUN-shaped packet")
		}
		return Decision{Bucket: BucketSTUN}
	}

	d.mu.RLock()
	peer, ok := d.peerBySrc[normalize(src)]
	d.mu.RUnlock()
	if ok {
		return Decision{Bucket: BucketRelay, Peer: peer}
	}

	d.countDrop(&d.droppedOther, src, "dropped unattributable packet")
	return Decision{Bucket: BucketDrop}
}

// DroppedSTUN reports how many STUN-shaped packets were dropped for having no
// matching pending transaction (or the wrong source address).
func (d *Demux) DroppedSTUN() uint64 {
	return d.droppedSTUN.Load()
}

// DroppedUnattributable reports how many packets matched neither a pending
// STUN transaction shape nor a programmed peer mapping.
func (d *Demux) DroppedUnattributable() uint64 {
	return d.droppedOther.Load()
}

// countDrop increments the counter and logs rate-limited: the first drop, then
// every 1024th, so a flood cannot flood the log.
func (d *Demux) countDrop(counter *atomic.Uint64, src netip.AddrPort, msg string) {
	n := counter.Add(1)
	if n == 1 || n%1024 == 0 {
		d.logger.Warn().Uint64("count", n).Str("src", src.String()).Msg(msg)
	}
}

// isSTUNShaped reports whether b looks like a STUN message: at least a full
// header, the two most significant bits zero, and the magic cookie in place.
func isSTUNShaped(b []byte) bool {
	return len(b) >= stunHeaderLen &&
		b[0]&0xC0 == 0 &&
		binary.BigEndian.Uint32(b[4:8]) == stunMagicCookie
}

// normalize strips the 4-in-6 mapped form so addresses compare equal no matter
// which representation the socket layer reported.
func normalize(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
