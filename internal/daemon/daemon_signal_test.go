//go:build !windows

package daemon

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestRun_ShouldReturnWhenSignalReceived sends a real SIGINT via syscall.Kill,
// which only exists on Unix-like platforms. Windows lacks POSIX signals, and
// Go's Windows substitute (os.Process.Signal(os.Interrupt), backed by
// GenerateConsoleCtrlEvent) requires an attached console and is unreliable
// for self-signaling in headless CI runners, so this test is not built there.
// The other Daemon.Run tests in daemon_test.go remain platform-independent.
func TestRun_ShouldReturnWhenSignalReceived(t *testing.T) {
	// Run listens for real OS signals (SIGINT/SIGTERM), so this test sends
	// one to the current process rather than cancelling the context.
	d, _, _, _ := newTestDaemon(t, time.Hour)

	done := make(chan struct{})
	go func() {
		d.Run(context.Background())
		close(done)
	}()

	// Give Run time to register its signal.Notify before we send the signal.
	time.Sleep(50 * time.Millisecond)

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("failed to send SIGINT: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after receiving SIGINT")
	}
}
