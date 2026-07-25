//go:build windows || wgproxy

package wgproxy_test

import (
	"encoding/binary"
	"net/netip"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

var (
	stunServer  = netip.MustParseAddrPort("74.125.250.129:19302")
	wrongServer = netip.MustParseAddrPort("74.125.250.130:19302")
	peerAddrA   = netip.MustParseAddrPort("203.0.113.10:51820")
	peerAddrB   = netip.MustParseAddrPort("198.51.100.7:41414")
	unmappedSrc = netip.MustParseAddrPort("192.0.2.99:12345")
)

func newTestDemux(t *testing.T) *wgproxy.Demux {
	t.Helper()
	logger := zerolog.Nop()
	return wgproxy.NewDemux(&logger)
}

// stunMessage builds a header-only STUN message.
func stunMessage(msgType uint16, txn wgproxy.TxnID) []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b[0:2], msgType)
	binary.BigEndian.PutUint16(b[2:4], 0) // attribute length
	binary.BigEndian.PutUint32(b[4:8], 0x2112A442)
	copy(b[8:20], txn[:])
	return b
}

// wgMessage builds a WireGuard-shaped packet of the given type and size.
func wgMessage(msgType byte, size int) []byte {
	b := make([]byte, size)
	b[0] = msgType
	return b
}

func testTxnID(seed byte) wgproxy.TxnID {
	var id wgproxy.TxnID
	for i := range id {
		id[i] = seed + byte(i)
	}
	return id
}

func testPeerKey(seed byte) wgproxy.PeerKey {
	var key wgproxy.PeerKey
	for i := range key {
		key[i] = seed
	}
	return key
}

func TestClassify_StunBindingSuccessRoutedToWaiter(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x10)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer d.Registry().Unregister(txn)

	pkt := stunMessage(0x0101, txn)
	decision := d.Classify(stunServer, pkt)

	if decision.Bucket != wgproxy.BucketSTUN {
		t.Fatalf("bucket = %v, want BucketSTUN", decision.Bucket)
	}
	select {
	case got := <-reply:
		if len(got) != len(pkt) {
			t.Fatalf("reply length = %d, want %d", len(got), len(pkt))
		}
	default:
		t.Fatal("binding success response was not routed to the waiter")
	}
}

func TestClassify_StunBindingErrorRoutedToWaiter(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x20)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer d.Registry().Unregister(txn)

	decision := d.Classify(stunServer, stunMessage(0x0111, txn))

	if decision.Bucket != wgproxy.BucketSTUN {
		t.Fatalf("bucket = %v, want BucketSTUN", decision.Bucket)
	}
	select {
	case <-reply:
	default:
		t.Fatal("binding error response was not routed to the waiter")
	}
}

func TestClassify_StunFromWrongSourceNotRouted(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x30)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer d.Registry().Unregister(txn)

	decision := d.Classify(wrongServer, stunMessage(0x0101, txn))

	if decision.Bucket != wgproxy.BucketSTUN {
		t.Fatalf("bucket = %v, want BucketSTUN", decision.Bucket)
	}
	select {
	case <-reply:
		t.Fatal("response from wrong source address must not reach the waiter")
	default:
	}
	if got := d.DroppedSTUN(); got != 1 {
		t.Fatalf("DroppedSTUN() = %d, want 1", got)
	}
}

func TestClassify_StunUnmatchedTxnDroppedWithCounter(t *testing.T) {
	d := newTestDemux(t)

	decision := d.Classify(stunServer, stunMessage(0x0101, testTxnID(0x40)))

	if decision.Bucket != wgproxy.BucketSTUN {
		t.Fatalf("bucket = %v, want BucketSTUN", decision.Bucket)
	}
	if got := d.DroppedSTUN(); got != 1 {
		t.Fatalf("DroppedSTUN() = %d, want 1", got)
	}
}

func TestClassify_StunRequestShapedPacketDropped(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x50)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer d.Registry().Unregister(txn)

	// A binding request (0x0001) is STUN-shaped but not a response; it must
	// never be routed to a waiter even with a matching txn ID and source.
	decision := d.Classify(stunServer, stunMessage(0x0001, txn))

	if decision.Bucket != wgproxy.BucketSTUN {
		t.Fatalf("bucket = %v, want BucketSTUN", decision.Bucket)
	}
	select {
	case <-reply:
		t.Fatal("non-response STUN message must not reach the waiter")
	default:
	}
	if got := d.DroppedSTUN(); got != 1 {
		t.Fatalf("DroppedSTUN() = %d, want 1", got)
	}
}

func TestClassify_ReplyIsCopiedNotAliased(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x60)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer d.Registry().Unregister(txn)

	buf := stunMessage(0x0101, txn)
	d.Classify(stunServer, buf)
	// Simulate the receive loop reusing its buffer for the next datagram.
	for i := range buf {
		buf[i] = 0xFF
	}

	var got []byte
	select {
	case got = <-reply:
	default:
		t.Fatal("binding success response was not routed to the waiter")
	}
	if got[0] == 0xFF {
		t.Fatal("routed reply aliases the caller's reused buffer; it must be a copy")
	}
	if binary.BigEndian.Uint16(got[0:2]) != 0x0101 {
		t.Fatalf("routed reply corrupted: type = %#x, want 0x0101", got[0:2])
	}
}

func TestClassify_WireGuardPacketsFromProgrammedSourceRelayed(t *testing.T) {
	peer := testPeerKey(0xAA)

	cases := []struct {
		name string
		pkt  []byte
	}{
		{"handshake initiation 148B", wgMessage(1, 148)},
		{"handshake response 92B", wgMessage(2, 92)},
		{"cookie reply 64B", wgMessage(3, 64)},
		{"transport data 32B", wgMessage(4, 32)},
		{"transport data full MTU", wgMessage(4, 1452)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDemux(t)
			d.Program(peer, peerAddrA)

			decision := d.Classify(peerAddrA, tc.pkt)

			if decision.Bucket != wgproxy.BucketRelay {
				t.Fatalf("bucket = %v, want BucketRelay", decision.Bucket)
			}
			if decision.Peer != peer {
				t.Fatalf("peer = %x, want %x", decision.Peer, peer)
			}
		})
	}
}

func TestClassify_WireGuardTransportFromUnmappedSourceDropped(t *testing.T) {
	d := newTestDemux(t)
	d.Program(testPeerKey(0xAA), peerAddrA)

	decision := d.Classify(unmappedSrc, wgMessage(4, 96))

	if decision.Bucket != wgproxy.BucketDrop {
		t.Fatalf("bucket = %v, want BucketDrop: unmapped source must never be forwarded", decision.Bucket)
	}
	if got := d.DroppedUnattributable(); got != 1 {
		t.Fatalf("DroppedUnattributable() = %d, want 1", got)
	}
}

func TestClassify_GarbageDropped(t *testing.T) {
	d := newTestDemux(t)

	garbage := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01}
	decision := d.Classify(unmappedSrc, garbage)

	if decision.Bucket != wgproxy.BucketDrop {
		t.Fatalf("bucket = %v, want BucketDrop", decision.Bucket)
	}
	if got := d.DroppedUnattributable(); got != 1 {
		t.Fatalf("DroppedUnattributable() = %d, want 1", got)
	}
}

func TestClassify_TruncatedPacketDropped(t *testing.T) {
	d := newTestDemux(t)

	// 19 bytes: one short of a STUN header, from an unmapped source.
	decision := d.Classify(unmappedSrc, make([]byte, 19))

	if decision.Bucket != wgproxy.BucketDrop {
		t.Fatalf("bucket = %v, want BucketDrop", decision.Bucket)
	}
}

func TestClassify_EmptyPacketDropped(t *testing.T) {
	d := newTestDemux(t)

	if got := d.Classify(unmappedSrc, nil).Bucket; got != wgproxy.BucketDrop {
		t.Fatalf("bucket = %v, want BucketDrop", got)
	}
}

func TestProgram_ReprogramMovesMapping(t *testing.T) {
	d := newTestDemux(t)
	peer := testPeerKey(0xBB)
	d.Program(peer, peerAddrA)
	d.Program(peer, peerAddrB) // peer roamed: establish re-programs

	if got := d.Classify(peerAddrA, wgMessage(4, 96)).Bucket; got != wgproxy.BucketDrop {
		t.Fatalf("stale source bucket = %v, want BucketDrop", got)
	}
	decision := d.Classify(peerAddrB, wgMessage(4, 96))
	if decision.Bucket != wgproxy.BucketRelay || decision.Peer != peer {
		t.Fatalf("new source decision = %+v, want relay to programmed peer", decision)
	}
}

func TestProgram_MappedAndUnmappedIPv4AddressesMatch(t *testing.T) {
	// A 4-in-6 mapped source (as a dual-stack read may report) must match a
	// mapping programmed with the plain IPv4 form, and vice versa.
	d := newTestDemux(t)
	peer := testPeerKey(0xCC)
	d.Program(peer, netip.MustParseAddrPort("[::ffff:203.0.113.10]:51820"))

	decision := d.Classify(peerAddrA, wgMessage(4, 96))
	if decision.Bucket != wgproxy.BucketRelay || decision.Peer != peer {
		t.Fatalf("decision = %+v, want relay despite 4-in-6 programmed form", decision)
	}
}

func TestUnprogram_RemovesMapping(t *testing.T) {
	d := newTestDemux(t)
	peer := testPeerKey(0xDD)
	d.Program(peer, peerAddrA)
	d.Unprogram(peer)

	if got := d.Classify(peerAddrA, wgMessage(4, 96)).Bucket; got != wgproxy.BucketDrop {
		t.Fatalf("bucket = %v, want BucketDrop after Unprogram", got)
	}
}

func TestTxnRegistry_DuplicateRegisterFails(t *testing.T) {
	r := wgproxy.NewTxnRegistry()
	txn := testTxnID(0x70)
	if _, err := r.Register(txn, stunServer); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := r.Register(txn, stunServer); err == nil {
		t.Fatal("second Register with the same txn ID must fail")
	}
}

func TestTxnRegistry_UnregisteredResponseDropped(t *testing.T) {
	d := newTestDemux(t)
	txn := testTxnID(0x80)
	reply, err := d.Registry().Register(txn, stunServer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	d.Registry().Unregister(txn)

	d.Classify(stunServer, stunMessage(0x0101, txn))

	select {
	case <-reply:
		t.Fatal("response after Unregister must not be delivered")
	default:
	}
	if got := d.DroppedSTUN(); got != 1 {
		t.Fatalf("DroppedSTUN() = %d, want 1", got)
	}
}

func TestTxnRegistry_ConcurrentUse(t *testing.T) {
	d := newTestDemux(t)
	r := d.Registry()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			txn := testTxnID(seed)
			reply, err := r.Register(txn, stunServer)
			if err != nil {
				t.Errorf("Register(%#x): %v", seed, err)
				return
			}
			d.Classify(stunServer, stunMessage(0x0101, txn))
			select {
			case <-reply:
			default:
				t.Errorf("txn %#x: reply not delivered", seed)
			}
			r.Unregister(txn)
		}(byte(i))
	}
	// Concurrent relay traffic against the same demux.
	peer := testPeerKey(0xEE)
	d.Program(peer, peerAddrA)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				d.Classify(peerAddrA, wgMessage(4, 96))
				d.Classify(unmappedSrc, wgMessage(4, 96))
			}
		}()
	}
	wg.Wait()
}
