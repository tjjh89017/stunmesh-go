package wgproxy_test

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

const testReadTimeout = 2 * time.Second

// newTestProxy creates an IPv4-only proxy with an ephemeral outer port and
// registers cleanup.
func newTestProxy(t *testing.T) *wgproxy.Proxy {
	t.Helper()
	logger := zerolog.Nop()
	p, err := wgproxy.New(&logger, map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// newLoopbackConn opens a real UDP socket on 127.0.0.1 with an ephemeral port,
// standing in for the WG device or a remote peer.
func newLoopbackConn(t *testing.T) (*net.UDPConn, netip.AddrPort) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	addr := conn.LocalAddr().(*net.UDPAddr).AddrPort()
	return conn, netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
}

// readPacket reads one datagram with a deadline (deadlines on test-owned
// sockets are fine; the constraint forbids them only on proxy sockets).
func readPacket(t *testing.T, conn *net.UDPConn) ([]byte, netip.AddrPort) {
	t.Helper()
	buf := make([]byte, 65535)
	if err := conn.SetReadDeadline(time.Now().Add(testReadTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, src, err := conn.ReadFromUDPAddrPort(buf)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort: %v", err)
	}
	return buf[:n], netip.AddrPortFrom(src.Addr().Unmap(), src.Port())
}

// expectNoPacket asserts the socket receives nothing within the window.
func expectNoPacket(t *testing.T, conn *net.UDPConn, window time.Duration) {
	t.Helper()
	buf := make([]byte, 65535)
	if err := conn.SetReadDeadline(time.Now().Add(window)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, src, err := conn.ReadFromUDPAddrPort(buf)
	if err == nil {
		t.Fatalf("unexpected packet: %d bytes from %s", n, src)
	}
	if !err.(net.Error).Timeout() {
		t.Fatalf("read error is not a timeout: %v", err)
	}
}

// startStunServer runs a minimal STUN server on loopback that answers every
// binding request with a binding success carrying the same transaction ID.
func startStunServer(t *testing.T) netip.AddrPort {
	t.Helper()
	conn, addr := newLoopbackConn(t)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, src, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			if n < 20 {
				continue
			}
			var txn wgproxy.TxnID
			copy(txn[:], buf[8:20])
			if _, err := conn.WriteToUDPAddrPort(stunMessage(0x0101, txn), src); err != nil {
				return
			}
		}
	}()
	return addr
}

// proxyOuterAddr is the loopback address a fake remote peer dials to reach the
// proxy's outer socket.
func proxyOuterAddr(t *testing.T, p *wgproxy.Proxy, fam wgproxy.Family) netip.AddrPort {
	t.Helper()
	port := p.OuterPort(fam)
	if port == 0 {
		t.Fatalf("OuterPort(%v) = 0, want a bound port", fam)
	}
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port)
}

func TestProxy_RelayRoundTrip_RemoteInitiatedFirst(t *testing.T) {
	p := newTestProxy(t)
	wgConn, wgAddr := newLoopbackConn(t)
	remoteConn, remoteAddr := newLoopbackConn(t)

	p.SetWGTarget(wgAddr.Port())
	peer := testPeerKey(0x01)
	innerAddr, err := p.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	p.SetPeerEndpoint(peer, remoteAddr)

	// Inbound first: the remote initiates before the proxy has ever seen WG
	// output, proving the WG-side target is fed, not learned from packets.
	inbound := wgMessage(1, 148) // handshake initiation
	if _, err := remoteConn.WriteToUDPAddrPort(inbound, proxyOuterAddr(t, p, wgproxy.FamilyIPv4)); err != nil {
		t.Fatalf("remote write: %v", err)
	}
	got, src := readPacket(t, wgConn)
	if !bytes.Equal(got, inbound) {
		t.Fatalf("WG side received %d bytes, want the %d-byte handshake unmodified", len(got), len(inbound))
	}
	if src != innerAddr {
		t.Fatalf("WG side saw source %s, want the peer's inner socket %s", src, innerAddr)
	}

	// Outbound: WG replies to the inner address it just saw.
	outbound := wgMessage(2, 92) // handshake response
	if _, err := wgConn.WriteToUDPAddrPort(outbound, innerAddr); err != nil {
		t.Fatalf("wg write: %v", err)
	}
	got, src = readPacket(t, remoteConn)
	if !bytes.Equal(got, outbound) {
		t.Fatalf("remote received %d bytes, want the %d-byte response unmodified", len(got), len(outbound))
	}
	if src.Port() != p.OuterPort(wgproxy.FamilyIPv4) {
		t.Fatalf("remote saw source port %d, want outer port %d", src.Port(), p.OuterPort(wgproxy.FamilyIPv4))
	}
}

func TestProxy_TwoPeers_DistinctInnerSocketsAndCorrectRouting(t *testing.T) {
	p := newTestProxy(t)
	wgConn, wgAddr := newLoopbackConn(t)
	p.SetWGTarget(wgAddr.Port())

	remoteConnA, remoteAddrA := newLoopbackConn(t)
	remoteConnB, remoteAddrB := newLoopbackConn(t)

	peerA, peerB := testPeerKey(0xA1), testPeerKey(0xB2)
	innerA, err := p.AddPeer(peerA)
	if err != nil {
		t.Fatalf("AddPeer(A): %v", err)
	}
	innerB, err := p.AddPeer(peerB)
	if err != nil {
		t.Fatalf("AddPeer(B): %v", err)
	}
	if innerA == innerB {
		t.Fatalf("both peers share inner socket %s; one inner socket per peer is mandatory", innerA)
	}
	p.SetPeerEndpoint(peerA, remoteAddrA)
	p.SetPeerEndpoint(peerB, remoteAddrB)

	outer := proxyOuterAddr(t, p, wgproxy.FamilyIPv4)

	// Inbound: each remote's packet must surface from its own peer's inner
	// socket (that is how WG attributes the peer).
	pktA, pktB := wgMessage(4, 100), wgMessage(4, 200)
	if _, err := remoteConnA.WriteToUDPAddrPort(pktA, outer); err != nil {
		t.Fatalf("remote A write: %v", err)
	}
	got, src := readPacket(t, wgConn)
	if !bytes.Equal(got, pktA) || src != innerA {
		t.Fatalf("peer A inbound: got %dB from %s, want %dB from %s", len(got), src, len(pktA), innerA)
	}
	if _, err := remoteConnB.WriteToUDPAddrPort(pktB, outer); err != nil {
		t.Fatalf("remote B write: %v", err)
	}
	got, src = readPacket(t, wgConn)
	if !bytes.Equal(got, pktB) || src != innerB {
		t.Fatalf("peer B inbound: got %dB from %s, want %dB from %s", len(got), src, len(pktB), innerB)
	}

	// Outbound: traffic WG sends to each inner socket must reach that peer's
	// remote, not the other one.
	outA, outB := wgMessage(4, 300), wgMessage(4, 400)
	if _, err := wgConn.WriteToUDPAddrPort(outA, innerA); err != nil {
		t.Fatalf("wg write to A: %v", err)
	}
	if got, _ := readPacket(t, remoteConnA); !bytes.Equal(got, outA) {
		t.Fatalf("remote A received %d bytes, want %d", len(got), len(outA))
	}
	if _, err := wgConn.WriteToUDPAddrPort(outB, innerB); err != nil {
		t.Fatalf("wg write to B: %v", err)
	}
	if got, _ := readPacket(t, remoteConnB); !bytes.Equal(got, outB) {
		t.Fatalf("remote B received %d bytes, want %d", len(got), len(outB))
	}
	expectNoPacket(t, remoteConnA, 50*time.Millisecond)
	expectNoPacket(t, remoteConnB, 50*time.Millisecond)
}

func TestProxy_AddPeerIdempotent(t *testing.T) {
	p := newTestProxy(t)
	peer := testPeerKey(0x11)
	first, err := p.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	second, err := p.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer (repeat): %v", err)
	}
	if first != second {
		t.Fatalf("repeated AddPeer returned %s, want the existing %s", second, first)
	}
}

func TestProxy_UnmappedSourceNeverReachesInnerSockets(t *testing.T) {
	p := newTestProxy(t)
	wgConn, wgAddr := newLoopbackConn(t)
	p.SetWGTarget(wgAddr.Port())

	peer := testPeerKey(0x22)
	if _, err := p.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	remoteConn, remoteAddr := newLoopbackConn(t)
	p.SetPeerEndpoint(peer, remoteAddr)

	strangerConn, _ := newLoopbackConn(t) // never programmed
	if _, err := strangerConn.WriteToUDPAddrPort(wgMessage(4, 96), proxyOuterAddr(t, p, wgproxy.FamilyIPv4)); err != nil {
		t.Fatalf("stranger write: %v", err)
	}
	expectNoPacket(t, wgConn, 200*time.Millisecond)
	_ = remoteConn
}

func TestProxy_Exchange_Success(t *testing.T) {
	p := newTestProxy(t)
	server := startStunServer(t)

	txn := testTxnID(0x90)
	ctx, cancel := context.WithTimeout(context.Background(), testReadTimeout)
	defer cancel()
	reply, err := p.Exchange(ctx, server, txn, stunMessage(0x0001, txn))
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if len(reply) < 20 || !bytes.Equal(reply[8:20], txn[:]) {
		t.Fatalf("reply does not carry the transaction ID: %x", reply)
	}
}

func TestProxy_Exchange_ContextCanceled(t *testing.T) {
	p := newTestProxy(t)
	_, silentServer := newLoopbackConn(t) // real socket, never answers

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := p.Exchange(ctx, silentServer, testTxnID(0x91), stunMessage(0x0001, testTxnID(0x91)))
	if err != context.Canceled {
		t.Fatalf("Exchange error = %v, want context.Canceled", err)
	}
}

func TestProxy_Exchange_Timeout(t *testing.T) {
	p := newTestProxy(t)
	p.SetExchangeTimeoutForTest(50 * time.Millisecond)
	_, silentServer := newLoopbackConn(t)

	_, err := p.Exchange(context.Background(), silentServer, testTxnID(0x92), stunMessage(0x0001, testTxnID(0x92)))
	if err != wgproxy.ErrExchangeTimeout {
		t.Fatalf("Exchange error = %v, want ErrExchangeTimeout", err)
	}
}

func TestProxy_Exchange_DuplicateTxnFails(t *testing.T) {
	p := newTestProxy(t)
	_, silentServer := newLoopbackConn(t)
	txn := testTxnID(0x93)
	if _, err := p.Demux().Registry().Register(txn, silentServer); err != nil {
		t.Fatalf("pre-Register: %v", err)
	}
	defer p.Demux().Registry().Unregister(txn)

	_, err := p.Exchange(context.Background(), silentServer, txn, stunMessage(0x0001, txn))
	if err == nil {
		t.Fatal("Exchange with an already-pending txn ID must fail")
	}
}

// TestProxy_OuterPortStableAcrossCycles is the KR5 regression guard: many
// Exchange + SetPeerEndpoint reprogramming cycles must never change the outer
// port, and the outer socket must stay usable throughout. The Proxy API has no
// Stop() by design — the structural defense — so "usable after Stop" reduces
// to "usable after every completed Exchange cycle".
func TestProxy_OuterPortStableAcrossCycles(t *testing.T) {
	p := newTestProxy(t)
	server := startStunServer(t)
	wgConn, wgAddr := newLoopbackConn(t)
	p.SetWGTarget(wgAddr.Port())

	peer := testPeerKey(0x55)
	if _, err := p.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	remoteConn, remoteAddr := newLoopbackConn(t)
	altConn, altAddr := newLoopbackConn(t)
	_ = altConn

	initial := p.OuterPort(wgproxy.FamilyIPv4)
	if initial == 0 {
		t.Fatal("OuterPort = 0 before any activity")
	}
	for i := 0; i < 50; i++ {
		txn := testTxnID(byte(i))
		ctx, cancel := context.WithTimeout(context.Background(), testReadTimeout)
		if _, err := p.Exchange(ctx, server, txn, stunMessage(0x0001, txn)); err != nil {
			cancel()
			t.Fatalf("cycle %d: Exchange: %v", i, err)
		}
		cancel()
		if i%2 == 0 {
			p.SetPeerEndpoint(peer, remoteAddr)
		} else {
			p.SetPeerEndpoint(peer, altAddr)
		}
		if got := p.OuterPort(wgproxy.FamilyIPv4); got != initial {
			t.Fatalf("cycle %d: outer port changed %d -> %d", i, initial, got)
		}
	}

	// The outer socket is still fully usable for relay after all cycles.
	p.SetPeerEndpoint(peer, remoteAddr)
	pkt := wgMessage(4, 96)
	if _, err := remoteConn.WriteToUDPAddrPort(pkt, proxyOuterAddr(t, p, wgproxy.FamilyIPv4)); err != nil {
		t.Fatalf("remote write: %v", err)
	}
	if got, _ := readPacket(t, wgConn); !bytes.Equal(got, pkt) {
		t.Fatalf("relay broken after cycles: got %d bytes, want %d", len(got), len(pkt))
	}
}

func TestProxy_IPv6RoundTrip(t *testing.T) {
	logger := zerolog.Nop()
	p, err := wgproxy.New(&logger, map[wgproxy.Family]uint16{wgproxy.FamilyIPv6: 0})
	if err != nil {
		t.Skipf("IPv6 unavailable: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	remoteConn, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	t.Cleanup(func() { _ = remoteConn.Close() })
	remoteAddr := remoteConn.LocalAddr().(*net.UDPAddr).AddrPort()

	wgConn, wgAddr := newLoopbackConn(t)
	p.SetWGTarget(wgAddr.Port())
	peer := testPeerKey(0x66)
	innerAddr, err := p.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	p.SetPeerEndpoint(peer, remoteAddr) // Is4() == false selects the v6 outer

	outerPort := p.OuterPort(wgproxy.FamilyIPv6)
	if outerPort == 0 {
		t.Fatal("OuterPort(FamilyIPv6) = 0")
	}
	outer := netip.AddrPortFrom(netip.IPv6Loopback(), outerPort)

	pkt := wgMessage(4, 128)
	if _, err := remoteConn.WriteToUDPAddrPort(pkt, outer); err != nil {
		t.Fatalf("remote write: %v", err)
	}
	got, src := readPacket(t, wgConn)
	if !bytes.Equal(got, pkt) || src != innerAddr {
		t.Fatalf("inbound v6: got %dB from %s, want %dB from %s", len(got), src, len(pkt), innerAddr)
	}

	out := wgMessage(4, 64)
	if _, err := wgConn.WriteToUDPAddrPort(out, innerAddr); err != nil {
		t.Fatalf("wg write: %v", err)
	}
	buf := make([]byte, 65535)
	if err := remoteConn.SetReadDeadline(time.Now().Add(testReadTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, src6, err := remoteConn.ReadFromUDPAddrPort(buf)
	if err != nil {
		t.Fatalf("remote read: %v", err)
	}
	if !bytes.Equal(buf[:n], out) || src6.Port() != outerPort {
		t.Fatalf("outbound v6: got %dB from %s, want %dB from outer port %d", n, src6, len(out), outerPort)
	}
}

func TestProxy_TruncationCounter(t *testing.T) {
	// A 65535-byte read cannot be produced by a real UDP datagram (payload max
	// is 65507), so the counter logic is unit-tested through the exported test
	// hook instead.
	p := newTestProxy(t)
	if got := p.Truncated(); got != 0 {
		t.Fatalf("Truncated() = %d before any reads, want 0", got)
	}
	p.NoteTruncationForTest(65534, 65535) // full-size-minus-one: not truncated
	if got := p.Truncated(); got != 0 {
		t.Fatalf("Truncated() = %d after a non-full read, want 0", got)
	}
	p.NoteTruncationForTest(65535, 65535) // n == len(buf): the tripwire
	if got := p.Truncated(); got != 1 {
		t.Fatalf("Truncated() = %d after a full-buffer read, want 1", got)
	}
}

func TestProxy_NewWithNoFamiliesFails(t *testing.T) {
	logger := zerolog.Nop()
	if _, err := wgproxy.New(&logger, nil); err == nil {
		t.Fatal("New with no families must fail")
	}
}

func TestProxy_CloseIsIdempotentAndStopsAcceptingPeers(t *testing.T) {
	logger := zerolog.Nop()
	p, err := wgproxy.New(&logger, map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.AddPeer(testPeerKey(0x77)); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := p.AddPeer(testPeerKey(0x78)); err == nil {
		t.Fatal("AddPeer after Close must fail")
	}
}
