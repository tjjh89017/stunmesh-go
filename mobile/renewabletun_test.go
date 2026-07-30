//go:build mobile && (linux || android)

package mobile

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun"
)

// fakeTun is a minimal tun.Device double. When block is true, Read parks
// until Close is called and then returns an error, simulating a real fd
// read that fails once the fd is closed out from under it.
type fakeTun struct {
	name  string
	block bool

	started   chan struct{}
	startOnce sync.Once

	closed    chan struct{}
	closeOnce sync.Once
	events    chan tun.Event
}

var _ tun.Device = (*fakeTun)(nil)

func newFakeTun(name string, block bool) *fakeTun {
	return &fakeTun{
		name:    name,
		block:   block,
		started: make(chan struct{}),
		closed:  make(chan struct{}),
		events:  make(chan tun.Event, 1),
	}
}

func (f *fakeTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	f.startOnce.Do(func() { close(f.started) })
	if f.block {
		<-f.closed
		return 0, errors.New("fd closed")
	}
	n := copy(bufs[0][offset:], []byte(f.name))
	sizes[0] = n
	return 1, nil
}

func (f *fakeTun) Write(bufs [][]byte, offset int) (int, error) { return len(bufs), nil }
func (f *fakeTun) File() *os.File                               { return nil }
func (f *fakeTun) MTU() (int, error)                            { return 1420, nil }
func (f *fakeTun) Name() (string, error)                        { return f.name, nil }
func (f *fakeTun) Events() <-chan tun.Event                     { return f.events }
func (f *fakeTun) BatchSize() int                               { return 1 }

func (f *fakeTun) Close() error {
	f.closeOnce.Do(func() {
		close(f.closed)
		close(f.events)
	})
	return nil
}

func TestSwappableTun_ReadRetriesOnSwapWhileBlocked(t *testing.T) {
	first := newFakeTun("first", true)
	second := newFakeTun("second", false)
	s := newSwappableTun(first)
	defer func() { _ = s.Close() }()

	type result struct {
		n   int
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		bufs := [][]byte{make([]byte, 64)}
		sizes := make([]int, 1)
		n, err := s.Read(bufs, sizes, 0)
		resultCh <- result{n, err}
	}()

	select {
	case <-first.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Read never entered the blocking first device")
	}

	// Swap while the Read goroutine is still parked in first.Read: this
	// closes first (unblocking the Read with an error) and installs
	// second. The blocked Read must notice the generation changed and
	// retry against second instead of surfacing first's close error.
	s.swap(second)

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("Read returned error after swap-triggered retry: %v", r.err)
		}
		if r.n != 1 {
			t.Errorf("Read returned n=%d, want 1", r.n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after swap; retry path likely stuck")
	}
}

func TestSwappableTun_ReadReturnsErrorWhenDeviceClosedDirectly(t *testing.T) {
	first := newFakeTun("first", true)
	s := newSwappableTun(first)

	type result struct {
		n   int
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		bufs := [][]byte{make([]byte, 64)}
		sizes := make([]int, 1)
		n, err := s.Read(bufs, sizes, 0)
		resultCh <- result{n, err}
	}()

	select {
	case <-first.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Read never entered the blocking first device")
	}

	// Close (not swap): the generation never changes, so the blocked
	// Read's error must be surfaced instead of retried forever.
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	select {
	case r := <-resultCh:
		if r.err == nil {
			t.Fatal("expected Read to return an error after Close, got nil")
		}
		if r.n != 0 {
			t.Errorf("Read returned n=%d on error, want 0", r.n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after Close")
	}
}

func TestSwappableTun_ConcurrentReadDuringRepeatedSwaps(t *testing.T) {
	s := newSwappableTun(newFakeTun("gen0", false))
	defer func() { _ = s.Close() }()

	const readers = 8
	const swaps = 50

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bufs := [][]byte{make([]byte, 64)}
			sizes := make([]int, 1)
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = s.Read(bufs, sizes, 0)
			}
		}()
	}

	for i := 0; i < swaps; i++ {
		s.swap(newFakeTun(fmt.Sprintf("gen%d", i+1), false))
	}
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("readers did not finish after swaps stopped; possible deadlock")
	}
}

func TestSwappableTun_PassthroughMethods(t *testing.T) {
	inner := newFakeTun("wg-mobile0", false)
	s := newSwappableTun(inner)
	defer func() { _ = s.Close() }()

	if mtu, err := s.MTU(); err != nil || mtu != 1420 {
		t.Errorf("MTU() = (%d, %v), want (1420, nil)", mtu, err)
	}
	if name, err := s.Name(); err != nil || name != "wg-mobile0" {
		t.Errorf("Name() = (%q, %v), want (%q, nil)", name, err, "wg-mobile0")
	}
	if bs := s.BatchSize(); bs != 1 {
		t.Errorf("BatchSize() = %d, want 1", bs)
	}
	if n, err := s.Write([][]byte{{1, 2, 3}}, 0); err != nil || n != 1 {
		t.Errorf("Write() = (%d, %v), want (1, nil)", n, err)
	}
}

func TestSwappableTun_PassthroughMethodsUseCurrentDeviceAfterSwap(t *testing.T) {
	s := newSwappableTun(newFakeTun("before", false))
	defer func() { _ = s.Close() }()

	s.swap(newFakeTun("after", false))

	if name, err := s.Name(); err != nil || name != "after" {
		t.Errorf("Name() after swap = (%q, %v), want (%q, nil)", name, err, "after")
	}
}

func TestSwappableTun_EventsForwardedFromCurrentDevice(t *testing.T) {
	inner := newFakeTun("first", false)
	s := newSwappableTun(inner)
	defer func() { _ = s.Close() }()

	inner.events <- tun.EventUp

	select {
	case ev := <-s.Events():
		if ev != tun.EventUp {
			t.Errorf("forwarded event = %v, want %v", ev, tun.EventUp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event was not forwarded from inner device")
	}
}

// TestSwappableTun_SwapEventRacingClose drives the interleaving where a
// freshly swapped-in device's forwarder goroutine delivers its pending event
// while Close concurrently closes the events channel. Before the
// check-and-send in forwardEvents was made atomic with Close, this
// occasionally panicked with a send on a closed channel.
func TestSwappableTun_SwapEventRacingClose(t *testing.T) {
	for i := 0; i < 2000; i++ {
		s := newSwappableTun(newFakeTun("first", false))
		replacement := newFakeTun("second", false)
		replacement.events <- tun.EventUp
		s.swap(replacement)
		if err := s.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}
}

func TestSwappableTun_CloseIsIdempotent(t *testing.T) {
	s := newSwappableTun(newFakeTun("first", false))

	if err := s.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}
