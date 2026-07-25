//go:build windows || wgproxy

package wgproxy_test

import (
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

const (
	benchPayloadSize = 1420
	benchGOMAXPROCS  = 4
	floorPPS         = 50000
	floorPacketCount = 100000

	// senderIdleWindow bounds a receive stall: a timeout after the sender has
	// finished means the remaining packets were dropped, not delayed.
	senderIdleWindow = 200 * time.Millisecond

	// benchSocketBuf is applied to TEST-owned sockets only: larger buffers
	// keep burst loss small without touching proxy internals.
	benchSocketBuf = 4 << 20
)

type relayEnv struct {
	proxy      *wgproxy.Proxy
	wgConn     *net.UDPConn
	remoteConn *net.UDPConn
	innerAddr  netip.AddrPort
	outerAddr  netip.AddrPort
}

func benchLoopbackConn(tb testing.TB) (*net.UDPConn, netip.AddrPort) {
	tb.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		tb.Fatalf("ListenUDP: %v", err)
	}
	tb.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadBuffer(benchSocketBuf)
	_ = conn.SetWriteBuffer(benchSocketBuf)
	addr := conn.LocalAddr().(*net.UDPAddr).AddrPort()
	return conn, netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
}

func newRelayEnv(tb testing.TB) *relayEnv {
	tb.Helper()
	logger := zerolog.Nop()
	p, err := wgproxy.New(&logger, map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: 0})
	if err != nil {
		tb.Fatalf("New: %v", err)
	}
	tb.Cleanup(func() { _ = p.Close() })

	wgConn, wgAddr := benchLoopbackConn(tb)
	remoteConn, remoteAddr := benchLoopbackConn(tb)
	p.SetWGTarget(wgAddr.Port())

	peer := testPeerKey(0xBE)
	innerAddr, err := p.AddPeer(peer)
	if err != nil {
		tb.Fatalf("AddPeer: %v", err)
	}
	p.SetPeerEndpoint(peer, remoteAddr)

	return &relayEnv{
		proxy:      p,
		wgConn:     wgConn,
		remoteConn: remoteConn,
		innerAddr:  innerAddr,
		outerAddr:  netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), p.OuterPort(wgproxy.FamilyIPv4)),
	}
}

// measureRelay pushes count packets from src and counts what dst receives.
// pps counts delivered packets, so kernel drops shrink the numerator instead
// of stalling; dst reads with deadlines so total loss ends the run.
func measureRelay(tb testing.TB, src, dst *net.UDPConn, target netip.AddrPort, count int) (received int, elapsed time.Duration) {
	tb.Helper()
	payload := wgMessage(4, benchPayloadSize)
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		for i := 0; i < count; i++ {
			sent := false
			for attempt := 0; attempt < 100; attempt++ {
				if _, err := src.WriteToUDPAddrPort(payload, target); err == nil {
					sent = true
					break
				}
				// ENOBUFS-class transient under burst; back off briefly.
				time.Sleep(time.Millisecond)
			}
			if !sent {
				return
			}
		}
	}()

	buf := make([]byte, benchPayloadSize+1)
	last := start
	for received < count {
		if err := dst.SetReadDeadline(time.Now().Add(senderIdleWindow)); err != nil {
			tb.Fatalf("SetReadDeadline: %v", err)
		}
		n, _, err := dst.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-done:
					elapsed = last.Sub(start)
					return received, elapsed
				default:
					continue
				}
			}
			tb.Fatalf("relay read: %v", err)
		}
		if n != benchPayloadSize {
			tb.Fatalf("relay read %d bytes, want %d", n, benchPayloadSize)
		}
		received++
		last = time.Now()
	}
	<-done
	return received, last.Sub(start)
}

func benchmarkRelayDirection(b *testing.B, src func(*relayEnv) *net.UDPConn, dst func(*relayEnv) *net.UDPConn, target func(*relayEnv) netip.AddrPort) {
	// Pinned via runtime.GOMAXPROCS, not b.SetParallelism: SetParallelism only
	// scales RunParallel goroutines and does not bound scheduler Ps.
	prev := runtime.GOMAXPROCS(benchGOMAXPROCS)
	defer runtime.GOMAXPROCS(prev)

	env := newRelayEnv(b)
	b.SetBytes(benchPayloadSize)
	b.ResetTimer()
	received, elapsed := measureRelay(b, src(env), dst(env), target(env), b.N)
	b.StopTimer()
	if received == 0 {
		b.Fatal("no packets relayed")
	}
	b.ReportMetric(float64(received)/elapsed.Seconds(), "pps")
	b.ReportMetric(float64(b.N-received)/float64(b.N), "loss")
}

func BenchmarkRelay(b *testing.B) {
	b.Run("inbound", func(b *testing.B) {
		benchmarkRelayDirection(b,
			func(e *relayEnv) *net.UDPConn { return e.remoteConn },
			func(e *relayEnv) *net.UDPConn { return e.wgConn },
			func(e *relayEnv) netip.AddrPort { return e.outerAddr })
	})
	b.Run("outbound", func(b *testing.B) {
		benchmarkRelayDirection(b,
			func(e *relayEnv) *net.UDPConn { return e.wgConn },
			func(e *relayEnv) *net.UDPConn { return e.remoteConn },
			func(e *relayEnv) netip.AddrPort { return e.innerAddr })
	})
}

// TestRelayThroughputFloor enforces a 50k pps relay floor; env-gated because
// only the pinned ubuntu-24.04 CI cell gives comparable numbers.
func TestRelayThroughputFloor(t *testing.T) {
	if os.Getenv("STUNMESH_BENCH_FLOOR") != "1" {
		t.Skip("set STUNMESH_BENCH_FLOOR=1 to enforce the relay throughput floor (ubuntu-24.04 CI only)")
	}

	prev := runtime.GOMAXPROCS(benchGOMAXPROCS)
	defer runtime.GOMAXPROCS(prev)

	env := newRelayEnv(t)
	received, elapsed := measureRelay(t, env.remoteConn, env.wgConn, env.outerAddr, floorPacketCount)
	if received == 0 {
		t.Fatal("no packets relayed")
	}
	pps := float64(received) / elapsed.Seconds()
	t.Logf("relay floor burst: %d/%d packets delivered in %v (%.0f pps)", received, floorPacketCount, elapsed, pps)
	if pps < floorPPS {
		t.Fatalf("relay throughput %.0f pps below the %d pps floor", pps, floorPPS)
	}
}
