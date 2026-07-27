// Socket relay: one Proxy per WireGuard interface, owning the per-family
// outer sockets and per-peer inner loopback sockets (classification lives in
// demux.go). Port lifetime invariant: every socket is bound exactly once and
// never rebound; read errors retry in place, Close is process-shutdown only,
// and there deliberately is no Stop.
package wgproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

const (
	relayBufSize = 65535

	defaultExchangeTimeout = 5 * time.Second

	readBackoffStep = time.Millisecond
	maxReadBackoff  = 100 * time.Millisecond
)

var (
	// ErrNoFamilies is returned by New when no protocol family is enabled.
	ErrNoFamilies = errors.New("wgproxy: no protocol families enabled")
	// ErrProxyClosed is returned for operations on a closed proxy.
	ErrProxyClosed = errors.New("wgproxy: proxy closed")
	// ErrFamilyNotEnabled marks an address whose family has no outer socket.
	ErrFamilyNotEnabled = errors.New("wgproxy: protocol family not enabled")
	// ErrExchangeTimeout is returned when no response arrives in time.
	ErrExchangeTimeout = errors.New("wgproxy: STUN exchange timed out")
)

// Family identifies an outer-socket protocol family.
type Family uint8

const (
	FamilyIPv4 Family = iota + 1
	FamilyIPv6
)

func (f Family) String() string {
	switch f {
	case FamilyIPv4:
		return "ipv4"
	case FamilyIPv6:
		return "ipv6"
	default:
		return fmt.Sprintf("family(%d)", uint8(f))
	}
}

func (f Family) network() string {
	switch f {
	case FamilyIPv4:
		return "udp4"
	case FamilyIPv6:
		return "udp6"
	default:
		return ""
	}
}

func familyOf(ap netip.AddrPort) Family {
	if ap.Addr().Unmap().Is4() {
		return FamilyIPv4
	}
	return FamilyIPv6
}

type outerSocket struct {
	conn *net.UDPConn
	port uint16
}

type peerState struct {
	inner     *net.UDPConn
	innerAddr netip.AddrPort
	// remote is programmed via SetPeerEndpoint only, never from packets.
	remote atomic.Pointer[netip.AddrPort]
}

// Proxy relays UDP between one WireGuard interface (over loopback) and the
// internet, and multiplexes STUN exchanges onto the same outer sockets.
type Proxy struct {
	logger zerolog.Logger
	demux  *Demux

	outer map[Family]*outerSocket // immutable after New

	// escapeStops holds cleanup funcs returned by escapeOuterSocket for
	// sockets that started a background watcher (currently darwin's
	// route-change watcher); immutable after New, called by Close.
	escapeStops []func()

	// wgPort is fed via SetWGTarget only, never learned from packets.
	wgPort atomic.Uint32

	mu     sync.RWMutex
	peers  map[PeerKey]*peerState
	closed bool

	loops sync.WaitGroup

	exchangeTimeout time.Duration

	truncated  atomic.Uint64
	unroutable atomic.Uint64
	writeErrs  atomic.Uint64
}

// New binds one outer socket per requested family (port 0 = ephemeral) and
// starts its receive loop. opts (see WithEscape) configure the per-OS
// tunnel-escape hook applied to each outer socket at creation.
func New(logger *zerolog.Logger, families map[Family]uint16, opts ...Option) (*Proxy, error) {
	if len(families) == 0 {
		return nil, ErrNoFamilies
	}
	var eo escapeOptions
	for _, opt := range opts {
		opt(&eo)
	}
	p := &Proxy{
		logger:          logger.With().Str("component", "wgproxy.proxy").Logger(),
		demux:           NewDemux(logger),
		outer:           make(map[Family]*outerSocket, len(families)),
		peers:           make(map[PeerKey]*peerState),
		exchangeTimeout: defaultExchangeTimeout,
	}
	for fam, port := range families {
		network := fam.network()
		if network == "" {
			p.closeOnError()
			return nil, fmt.Errorf("wgproxy: unknown protocol family %d", uint8(fam))
		}
		conn, err := net.ListenUDP(network, &net.UDPAddr{Port: int(port)})
		if err != nil {
			p.closeOnError()
			return nil, fmt.Errorf("wgproxy: bind %s outer socket: %w", fam, err)
		}
		if stop := escapeOuterSocket(conn, fam, eo, p.logger); stop != nil {
			p.escapeStops = append(p.escapeStops, stop)
		}
		local := conn.LocalAddr().(*net.UDPAddr)
		p.outer[fam] = &outerSocket{conn: conn, port: uint16(local.Port)}
		p.logger.Info().Stringer("family", fam).Int("port", local.Port).Msg("outer socket bound")
	}
	for fam, sock := range p.outer {
		p.loops.Add(1)
		go p.outerLoop(fam, sock.conn)
	}
	return p, nil
}

// SetWGTarget records the real WireGuard listen port that inbound relay
// packets are written to.
func (p *Proxy) SetWGTarget(port uint16) {
	p.wgPort.Store(uint32(port))
}

func (p *Proxy) wgTarget() netip.AddrPort {
	port := p.wgPort.Load()
	if port == 0 {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(port))
}

// AddPeer opens the peer's inner loopback socket and starts its relay
// goroutine; idempotent, returning the existing inner address on repeat calls.
func (p *Proxy) AddPeer(key PeerKey) (netip.AddrPort, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return netip.AddrPort{}, ErrProxyClosed
	}
	if ps, ok := p.peers[key]; ok {
		return ps.innerAddr, nil
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("wgproxy: bind inner socket: %w", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr).AddrPort()
	ps := &peerState{inner: conn, innerAddr: normalize(addr)}
	p.peers[key] = ps
	p.loops.Add(1)
	go p.innerLoop(ps)
	p.logger.Debug().Str("inner", ps.innerAddr.String()).Msg("peer inner socket bound")
	return ps.innerAddr, nil
}

// SetPeerEndpoint programs the peer's inbound demux mapping and outbound
// remote — the only way forwarding state changes.
func (p *Proxy) SetPeerEndpoint(key PeerKey, remote netip.AddrPort) {
	remote = normalize(remote)
	p.demux.Program(key, remote)
	p.mu.RLock()
	ps := p.peers[key]
	p.mu.RUnlock()
	if ps == nil {
		p.logger.Warn().Str("remote", remote.String()).Msg("SetPeerEndpoint for unknown peer; call AddPeer first")
		return
	}
	ps.remote.Store(&remote)
}

// OuterPort reports the family's outer-socket port (0 when not enabled); it
// never changes for the life of the proxy.
func (p *Proxy) OuterPort(fam Family) uint16 {
	if sock, ok := p.outer[fam]; ok {
		return sock.port
	}
	return 0
}

// Demux exposes the packet classifier.
func (p *Proxy) Demux() *Demux {
	return p.demux
}

// Truncated reports how many relay reads filled the entire buffer.
func (p *Proxy) Truncated() uint64 {
	return p.truncated.Load()
}

// Exchange sends the request on the family-matching outer socket and returns
// the raw demux-routed response. Timeouts use select — never socket read
// deadlines, which would break the relay loop sharing the socket.
func (p *Proxy) Exchange(ctx context.Context, server netip.AddrPort, txnID TxnID, packet []byte) ([]byte, error) {
	sock, ok := p.outer[familyOf(server)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFamilyNotEnabled, familyOf(server))
	}
	reply, err := p.demux.Registry().Register(txnID, server)
	if err != nil {
		return nil, err
	}
	defer p.demux.Registry().Unregister(txnID)

	if _, err := sock.conn.WriteToUDPAddrPort(packet, server); err != nil {
		return nil, fmt.Errorf("wgproxy: send STUN request: %w", err)
	}

	timer := time.NewTimer(p.exchangeTimeout)
	defer timer.Stop()
	select {
	case b := <-reply:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrExchangeTimeout
	}
}

// Close stops all relay goroutines and closes every socket; idempotent.
func (p *Proxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	p.closeOnError()
	p.loops.Wait()
	return nil
}

// closeOnError runs the escape stops collected so far, then closes every
// socket opened so far; shared by Close and New's failure paths so a
// half-built Proxy never leaks an earlier family's watcher goroutine or fd.
func (p *Proxy) closeOnError() {
	for _, stop := range p.escapeStops {
		stop()
	}
	p.closeSockets()
}

func (p *Proxy) closeSockets() {
	for _, sock := range p.outer {
		_ = sock.conn.Close()
	}
	p.mu.RLock()
	for _, ps := range p.peers {
		_ = ps.inner.Close()
	}
	p.mu.RUnlock()
}

func (p *Proxy) isClosed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.closed
}

// outerLoop owns one outer socket: receive, classify, and relay to the
// WG-side target from the peer's inner socket (so WireGuardNT sees the source
// port it dialed).
func (p *Proxy) outerLoop(fam Family, conn *net.UDPConn) {
	defer p.loops.Done()
	buf := make([]byte, relayBufSize)
	consecutiveErrs := 0
	for {
		n, src, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || p.isClosed() {
				return
			}
			consecutiveErrs++
			p.logger.Warn().Err(err).Stringer("family", fam).Int("consecutive", consecutiveErrs).Msg("outer read error, retrying")
			backoff(consecutiveErrs)
			continue
		}
		consecutiveErrs = 0
		p.noteTruncation(n, len(buf))

		decision := p.demux.Classify(src, buf[:n])
		if decision.Bucket != BucketRelay {
			continue
		}
		p.mu.RLock()
		ps := p.peers[decision.Peer]
		p.mu.RUnlock()
		target := p.wgTarget()
		if ps == nil || !target.IsValid() {
			p.countWarn(&p.unroutable, "relay packet for peer without inner socket or WG target")
			continue
		}
		if _, err := ps.inner.WriteToUDPAddrPort(buf[:n], target); err != nil {
			p.countWarn(&p.writeErrs, "inner write failed")
		}
	}
}

// innerLoop owns one peer's inner socket: relay WG output to the peer's
// current remote via the family-matching outer socket.
func (p *Proxy) innerLoop(ps *peerState) {
	defer p.loops.Done()
	buf := make([]byte, relayBufSize)
	consecutiveErrs := 0
	for {
		n, _, err := ps.inner.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || p.isClosed() {
				return
			}
			consecutiveErrs++
			p.logger.Warn().Err(err).Str("inner", ps.innerAddr.String()).Int("consecutive", consecutiveErrs).Msg("inner read error, retrying")
			backoff(consecutiveErrs)
			continue
		}
		consecutiveErrs = 0
		p.noteTruncation(n, len(buf))

		remote := ps.remote.Load()
		if remote == nil {
			p.countWarn(&p.unroutable, "outbound packet before peer endpoint programmed")
			continue
		}
		sock, ok := p.outer[familyOf(*remote)]
		if !ok {
			p.countWarn(&p.unroutable, "outbound packet for disabled protocol family")
			continue
		}
		if _, err := sock.conn.WriteToUDPAddrPort(buf[:n], *remote); err != nil {
			p.countWarn(&p.writeErrs, "outer write failed")
		}
	}
}

func (p *Proxy) noteTruncation(n, bufLen int) {
	if n == bufLen {
		count := p.truncated.Add(1)
		p.logger.Warn().Uint64("count", count).Msg("relay read filled the entire buffer; datagram may be truncated")
	}
}

// countWarn logs rate-limited: the first event, then every 1024th.
func (p *Proxy) countWarn(counter *atomic.Uint64, msg string) {
	n := counter.Add(1)
	if n == 1 || n%1024 == 0 {
		p.logger.Warn().Uint64("count", n).Msg(msg)
	}
}

func backoff(consecutiveErrs int) {
	if consecutiveErrs <= 1 {
		return
	}
	time.Sleep(min(time.Duration(consecutiveErrs-1)*readBackoffStep, maxReadBackoff))
}
