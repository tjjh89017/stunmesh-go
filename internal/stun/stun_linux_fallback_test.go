//go:build linux

package stun

import (
	"errors"
	"net"
	"testing"
	"time"

	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
)

// newLoopbackStun builds a Linux Stun whose conn4 is a plain UDP socket, so
// the reader runs without root. A UDP socket returns only the payload, so
// deliver prepends the 8-byte UDP header a raw socket would return.
func newLoopbackStun(t *testing.T) (*Stun, *net.UDPConn) {
	t.Helper()
	rc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := net.DialUDP("udp4", nil, rc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sc.Close() })

	s := &Stun{
		protocol:   "ipv4",
		conn4:      ipv4.NewPacketConn(rc),
		packetChan: make(chan *stun.Message),
		done:       make(chan struct{}),
	}
	t.Cleanup(func() { _ = s.Stop() })
	s.Start(zerolog.Nop().WithContext(t.Context()))
	return s, sc
}

func deliver(t *testing.T, sc *net.UDPConn, payload []byte) {
	t.Helper()
	if _, err := sc.Write(append(make([]byte, udpHeaderSize), payload...)); err != nil {
		t.Fatal(err)
	}
}

func newTxID(t *testing.T) [stun.TransactionIDSize]byte {
	t.Helper()
	return buildMessage(t, stun.TransactionID).TransactionID
}

var (
	firstAddr  = &stun.XORMappedAddress{IP: net.IPv4(198, 51, 100, 1), Port: 40001}
	healthyIP  = net.IPv4(198, 51, 100, 7)
	healthyRep = &stun.XORMappedAddress{IP: healthyIP, Port: 40000}
)

func reply(t *testing.T, txID [stun.TransactionIDSize]byte, setters ...stun.Setter) []byte {
	t.Helper()
	return buildMessage(t, append([]stun.Setter{stun.NewTransactionIDSetter(txID)}, setters...)...).Raw
}

// exchange is Connect after its send: wait for the reply to txID and parse it.
func exchange(t *testing.T, s *Stun, txID [stun.TransactionIDSize]byte) (*stun.XORMappedAddress, error) {
	t.Helper()
	ctx := zerolog.Nop().WithContext(t.Context())
	m, err := s.awaitReply(ctx, txID)
	if err != nil {
		return nil, err
	}
	if a := Parse(ctx, m); a != nil {
		return a, nil
	}
	return nil, ErrNoMappedAddress
}

func wantHealthy(t *testing.T, a *stun.XORMappedAddress, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("healthy server failed: %v", err)
	}
	if !a.IP.Equal(healthyIP) || a.Port != healthyRep.Port {
		t.Fatalf("got %v, want %v", a, healthyRep)
	}
}

func TestFallback_NoMappedAddressThenHealthy(t *testing.T) {
	s, sc := newLoopbackStun(t)

	tx1 := newTxID(t)
	deliver(t, sc, reply(t, tx1, stun.BindingSuccess,
		&stun.MappedAddress{IP: net.IPv4(198, 51, 100, 1), Port: 40001}))
	if _, err := exchange(t, s, tx1); !errors.Is(err, ErrNoMappedAddress) {
		t.Fatalf("server 1: want ErrNoMappedAddress, got %v", err)
	}

	tx2 := newTxID(t)
	deliver(t, sc, reply(t, tx2, stun.BindingSuccess, healthyRep))
	a, err := exchange(t, s, tx2)
	wantHealthy(t, a, err)
}

func TestFallback_ErrorResponseThenHealthy(t *testing.T) {
	s, sc := newLoopbackStun(t)

	tx1 := newTxID(t)
	deliver(t, sc, reply(t, tx1,
		stun.NewType(stun.MethodBinding, stun.ClassErrorResponse), stun.CodeBadRequest))
	if _, err := exchange(t, s, tx1); err == nil {
		t.Fatal("server 1: an error response must fail")
	}

	tx2 := newTxID(t)
	deliver(t, sc, reply(t, tx2, stun.BindingSuccess, healthyRep))
	a, err := exchange(t, s, tx2)
	wantHealthy(t, a, err)
}

func TestFallback_ForeignTransactionIDIsDropped(t *testing.T) {
	s, sc := newLoopbackStun(t)

	tx := newTxID(t)
	deliver(t, sc, reply(t, newTxID(t), stun.BindingSuccess, firstAddr))
	deliver(t, sc, reply(t, newTxID(t), stun.BindingRequest))
	deliver(t, sc, reply(t, tx, stun.BindingSuccess, healthyRep))
	a, err := exchange(t, s, tx)
	wantHealthy(t, a, err)
}

func TestFallback_UndecodablePacketIsSkipped(t *testing.T) {
	s, sc := newLoopbackStun(t)

	tx := newTxID(t)
	deliver(t, sc, []byte{0xff, 0xff, 0xff, 0xff})
	deliver(t, sc, reply(t, tx, stun.BindingSuccess, healthyRep))
	a, err := exchange(t, s, tx)
	wantHealthy(t, a, err)
}

func TestStop_EndsLinuxReader(t *testing.T) {
	s, _ := newLoopbackStun(t)

	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not return")
	}

	// A closed socket must not leave the reader spinning: with done closed and
	// the fd gone, pump returns instead of retrying.
	if _, err := s.readFrom(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("read after Stop: want net.ErrClosed, got %v", err)
	}
}
