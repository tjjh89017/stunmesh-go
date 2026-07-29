//go:build windows

package wgproxy

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestWatchNotify_DebouncesBurstIntoSingleApply drives watchNotify with fake
// sig/stop channels standing in for notifyDispatch's fan-out, without
// registering a real NotifyIpInterfaceChange callback. A burst of signals
// within routeWatchDebounce of each other must coalesce into one apply call,
// not one per signal.
func TestWatchNotify_DebouncesBurstIntoSingleApply(t *testing.T) {
	sig := make(chan struct{}, 1)
	stop := make(chan struct{})

	var applyCount int32
	done := watchNotify(sig, stop, func() {
		atomic.AddInt32(&applyCount, 1)
	})

	for i := 0; i < 5; i++ {
		select {
		case sig <- struct{}{}:
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait past the debounce window for the coalesced apply to fire.
	time.Sleep(routeWatchDebounce + 150*time.Millisecond)

	if got := atomic.LoadInt32(&applyCount); got != 1 {
		t.Fatalf("apply called %d times, want 1 (burst should coalesce)", got)
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchNotify did not stop after stop closed")
	}
}

// TestWatchNotify_StopRaceWithConcurrentSignalSender exercises the documented
// stop/close race from watchNotify's doc comment: sig is the shared
// notifyDispatch channel, which may have a concurrent, non-blocking sender
// still racing the stop close — that is exactly why production code closes
// stop (single writer, no concurrent sender) instead of sig (would panic a
// racing sender). This proves the guarantee under `go test -race`: no panic,
// no deadlock, regardless of ordering between the sender and the stop close.
func TestWatchNotify_StopRaceWithConcurrentSignalSender(t *testing.T) {
	sig := make(chan struct{}, 1)
	stop := make(chan struct{})

	done := watchNotify(sig, stop, func() {})

	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		for i := 0; i < 100; i++ {
			select {
			case sig <- struct{}{}:
			default:
			}
		}
	}()

	close(stop)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchNotify did not stop when racing a concurrent sig sender against stop close")
	}
	<-senderDone
}
