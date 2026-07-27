package wgproxy_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

func newTestManager(t *testing.T) *wgproxy.Manager {
	t.Helper()
	logger := zerolog.Nop()
	m := wgproxy.NewManager(&logger)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func ipv4Families() map[wgproxy.Family]uint16 {
	return map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: 0}
}

func TestManagerFor_MemoizesPerDevice(t *testing.T) {
	m := newTestManager(t)

	first, err := m.For("wg0", ipv4Families())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	port := first.OuterPort(wgproxy.FamilyIPv4)
	if port == 0 {
		t.Fatal("expected a bound outer port")
	}

	// A second For with different families must return the same instance and
	// port — never a freshly-bound proxy.
	second, err := m.For("wg0", map[wgproxy.Family]uint16{wgproxy.FamilyIPv6: 0})
	if err != nil {
		t.Fatalf("For (second): %v", err)
	}
	if second != first {
		t.Fatal("expected the memoized proxy instance")
	}
	if got := second.OuterPort(wgproxy.FamilyIPv4); got != port {
		t.Fatalf("outer port changed: %d != %d", got, port)
	}
	if got := second.OuterPort(wgproxy.FamilyIPv6); got != 0 {
		t.Fatalf("later families argument bound a new socket: port %d", got)
	}
}

func TestManagerFor_DistinctDevices(t *testing.T) {
	m := newTestManager(t)

	a, err := m.For("wg0", ipv4Families())
	if err != nil {
		t.Fatalf("For wg0: %v", err)
	}
	b, err := m.For("wg1", ipv4Families())
	if err != nil {
		t.Fatalf("For wg1: %v", err)
	}
	if a == b {
		t.Fatal("expected distinct proxies for distinct devices")
	}
	if a.OuterPort(wgproxy.FamilyIPv4) == b.OuterPort(wgproxy.FamilyIPv4) {
		t.Fatal("expected distinct outer ports for distinct devices")
	}
}

func TestManagerFor_Concurrent(t *testing.T) {
	m := newTestManager(t)

	const goroutines = 16
	proxies := make([]*wgproxy.Proxy, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := m.For("wg0", ipv4Families())
			if err != nil {
				t.Errorf("For: %v", err)
				return
			}
			proxies[i] = p
		}()
	}
	wg.Wait()

	for i := 1; i < goroutines; i++ {
		if proxies[i] != proxies[0] {
			t.Fatalf("goroutine %d got a different proxy instance", i)
		}
	}
}

func TestManagerFor_CreationErrorNotMemoized(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.For("wg0", nil); !errors.Is(err, wgproxy.ErrNoFamilies) {
		t.Fatalf("expected ErrNoFamilies, got %v", err)
	}
	if _, err := m.For("wg0", ipv4Families()); err != nil {
		t.Fatalf("For after failed creation: %v", err)
	}
}

func TestManagerClose_IdempotentAndForErrors(t *testing.T) {
	logger := zerolog.Nop()
	m := wgproxy.NewManager(&logger)

	p, err := m.For("wg0", ipv4Families())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}
	// The proxy's Close is idempotent too, so verify it was actually closed.
	if _, err := p.AddPeer(wgproxy.PeerKey{1}); !errors.Is(err, wgproxy.ErrProxyClosed) {
		t.Fatalf("expected proxy closed after manager Close, got %v", err)
	}
	if _, err := m.For("wg1", ipv4Families()); !errors.Is(err, wgproxy.ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got %v", err)
	}
}

func TestManagerGet_BeforeForReturnsNotReady(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.Get("wg0"); !errors.Is(err, wgproxy.ErrProxyNotReady) {
		t.Fatalf("expected ErrProxyNotReady, got %v", err)
	}
	// Get must never create: a later For should still be the first binding.
	p, err := m.For("wg0", ipv4Families())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	got, err := m.Get("wg0")
	if err != nil {
		t.Fatalf("Get after For: %v", err)
	}
	if got != p {
		t.Fatal("expected Get to return the memoized proxy instance")
	}
}

func TestManagerGet_AfterCloseReturnsClosed(t *testing.T) {
	logger := zerolog.Nop()
	m := wgproxy.NewManager(&logger)
	if _, err := m.For("wg0", ipv4Families()); err != nil {
		t.Fatalf("For: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Get("wg0"); !errors.Is(err, wgproxy.ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got %v", err)
	}
}
