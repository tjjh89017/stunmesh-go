//go:build darwin

package wgproxy

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestWatchRoutes_DebouncesBurstIntoSingleApply drives watchRoutes with a real
// os.Pipe standing in for the PF_ROUTE socket: writes to the pipe are read
// via unix.Read exactly like routing-socket messages, without needing an
// actual routing socket. A burst of signals within routeWatchDebounce of each
// other must coalesce into one apply call, not one per signal.
func TestWatchRoutes_DebouncesBurstIntoSingleApply(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	var applyCount int32
	done := watchRoutes(int(r.Fd()), func() {
		atomic.AddInt32(&applyCount, 1)
	})

	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte{0}); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait past the debounce window for the coalesced apply to fire.
	time.Sleep(routeWatchDebounce + 150*time.Millisecond)

	if got := atomic.LoadInt32(&applyCount); got != 1 {
		t.Fatalf("apply called %d times, want 1 (burst should coalesce)", got)
	}

	if err := unix.Close(int(r.Fd())); err != nil {
		t.Fatalf("close read fd: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchRoutes did not stop after fd closed")
	}
}

// TestWatchRoutes_StopRaceWithInFlightSignal exercises the documented
// stop/close race: escapeOuterSocket's returned closure calls unix.Close(fd)
// while the reader goroutine may be blocked in unix.Read on that same fd, or
// mid-select trying to forward a just-read signal. Neither path is
// synchronized against the other beyond the fd itself, so this only proves
// its intended guarantee under `go test -race`: no unsynchronized access, no
// panic, no deadlock, regardless of which happens first.
func TestWatchRoutes_StopRaceWithInFlightSignal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	done := watchRoutes(int(r.Fd()), func() {})

	go func() {
		_, _ = w.Write([]byte{0})
	}()
	if err := unix.Close(int(r.Fd())); err != nil {
		t.Fatalf("close read fd: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchRoutes did not stop when racing close against an in-flight signal")
	}
}
