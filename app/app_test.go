package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeRunner is a runner whose Run blocks until its ctx is cancelled, then
// signals exit via the started/exited channels so tests can assert ordering.
type fakeRunner struct {
	started chan struct{}
	exited  chan struct{}

	oneshotCalls int
	mu           sync.Mutex
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		started: make(chan struct{}, 1),
		exited:  make(chan struct{}, 1),
	}
}

func (f *fakeRunner) Run(ctx context.Context) error {
	f.started <- struct{}{}
	<-ctx.Done()
	f.exited <- struct{}{}
	return ctx.Err()
}

func (f *fakeRunner) RunOneshot(ctx context.Context) error {
	f.mu.Lock()
	f.oneshotCalls++
	f.mu.Unlock()
	return nil
}

func newTestApp(r runner) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		daemon:  r,
		cleanup: func() {},
		ctx:     ctx,
		cancel:  cancel,
	}
}

func TestClose_WaitsForInFlightRun(t *testing.T) {
	r := newFakeRunner()
	a := newTestApp(r)

	runErr := make(chan error, 1)
	go func() {
		runErr <- a.Run(context.Background())
	}()

	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("Run did not start")
	}

	closeDone := make(chan struct{})
	go func() {
		a.Close()
		close(closeDone)
	}()

	select {
	case <-r.exited:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the in-flight Run")
	}

	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after being cancelled")
	}

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after Run returned")
	}
}

func TestRun_SecondConcurrentCallReturnsErrAlreadyRunning(t *testing.T) {
	r := newFakeRunner()
	a := newTestApp(r)
	defer a.Close()

	go func() {
		_ = a.Run(context.Background())
	}()

	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("Run did not start")
	}

	if err := a.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run = %v, want ErrAlreadyRunning", err)
	}
}

func TestRun_AfterCloseReturnsErrClosed(t *testing.T) {
	r := newFakeRunner()
	a := newTestApp(r)
	a.Close()

	if err := a.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close = %v, want ErrClosed", err)
	}
	if err := a.RunOneshot(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("RunOneshot after Close = %v, want ErrClosed", err)
	}
}

func TestRun_CallerCtxCancellationStopsRun(t *testing.T) {
	r := newFakeRunner()
	a := newTestApp(r)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- a.Run(ctx)
	}()

	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("Run did not start")
	}

	cancel()

	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after caller ctx was cancelled")
	}
}

func TestClose_TwiceIsNoOp(t *testing.T) {
	closes := 0
	a := newTestApp(newFakeRunner())
	a.cleanup = func() { closes++ }

	a.Close()
	a.Close()

	if closes != 1 {
		t.Fatalf("cleanup called %d times, want 1", closes)
	}
}

func TestRunOneshot_SequentialCallsAllowed(t *testing.T) {
	r := newFakeRunner()
	a := newTestApp(r)
	defer a.Close()

	if err := a.RunOneshot(context.Background()); err != nil {
		t.Fatalf("first RunOneshot: %v", err)
	}
	if err := a.RunOneshot(context.Background()); err != nil {
		t.Fatalf("second RunOneshot: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.oneshotCalls != 2 {
		t.Fatalf("oneshotCalls = %d, want 2", r.oneshotCalls)
	}
}
