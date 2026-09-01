package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// fakeBootstrap counts Execute calls.
type fakeBootstrap struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeBootstrap) Execute(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

func (f *fakeBootstrap) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakePublish counts Execute/Trigger calls and blocks Run until ctx is done,
// mirroring the real PublishController's worker-loop shape.
type fakePublish struct {
	mu           sync.Mutex
	executeCalls int
	triggerCalls int
}

func (f *fakePublish) Execute(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executeCalls++
}

func (f *fakePublish) Run(ctx context.Context) {
	<-ctx.Done()
}

func (f *fakePublish) Trigger() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggerCalls++
}

func (f *fakePublish) TriggerCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.triggerCalls
}

func (f *fakePublish) ExecuteCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.executeCalls
}

// fakeEstablish counts Trigger/WaitForCompletion calls and blocks Run until
// ctx is done, mirroring the real EstablishController's worker-loop shape.
type fakeEstablish struct {
	mu                     sync.Mutex
	triggerCalls           int
	waitForCompletionCalls int
}

func (f *fakeEstablish) Run(ctx context.Context) {
	<-ctx.Done()
}

func (f *fakeEstablish) Trigger(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggerCalls++
}

func (f *fakeEstablish) WaitForCompletion(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waitForCompletionCalls++
}

func (f *fakeEstablish) TriggerCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.triggerCalls
}

func (f *fakeEstablish) WaitForCompletionCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitForCompletionCalls
}

// fakePingMonitor blocks Execute until ctx is done, like the real controller.
type fakePingMonitor struct{}

func (f *fakePingMonitor) Execute(ctx context.Context) {
	<-ctx.Done()
}

// slowPingMonitor blocks Execute until ctx is done, then sleeps for delay
// before signaling completion via the finished channel. This proves the
// caller actually joins the goroutine (via sync.WaitGroup) rather than
// merely signaling it to stop: a test that only waits for ctx cancellation
// would pass even if the join were missing, since the fake would usually
// still be scheduled by the time assertions run.
type slowPingMonitor struct {
	delay    time.Duration
	finished chan struct{}
}

func newSlowPingMonitor(delay time.Duration) *slowPingMonitor {
	return &slowPingMonitor{delay: delay, finished: make(chan struct{})}
}

func (f *slowPingMonitor) Execute(ctx context.Context) {
	<-ctx.Done()
	time.Sleep(f.delay)
	close(f.finished)
}

// slowEstablish behaves like fakeEstablish for Trigger/WaitForCompletion but
// overrides Run to block until ctx is done, then sleep for delay before
// signaling completion via the finished channel — same rationale as
// slowPingMonitor, applied to RunOneshot's establishCtrl.Run worker.
type slowEstablish struct {
	*fakeEstablish
	delay    time.Duration
	finished chan struct{}
}

func newSlowEstablish(delay time.Duration) *slowEstablish {
	return &slowEstablish{fakeEstablish: &fakeEstablish{}, delay: delay, finished: make(chan struct{})}
}

func (f *slowEstablish) Run(ctx context.Context) {
	<-ctx.Done()
	time.Sleep(f.delay)
	close(f.finished)
}

func newTestDaemon(t *testing.T, refreshInterval time.Duration) (*Daemon, *fakeBootstrap, *fakePublish, *fakeEstablish) {
	t.Helper()

	boot := &fakeBootstrap{}
	publish := &fakePublish{}
	establish := &fakeEstablish{}
	pingMonitor := &fakePingMonitor{}

	cfg := &config.Config{RefreshInterval: refreshInterval}
	logger := zerolog.Nop()

	d := New(cfg, boot, publish, establish, pingMonitor, &logger)

	return d, boot, publish, establish
}

func TestRun_ShouldTriggerPublishAndEstablishOnTick(t *testing.T) {
	d, _, publish, establish := newTestDaemon(t, 20*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	// The initial Trigger call happens before the ticker loop starts, so wait
	// for at least one tick-driven Trigger beyond that baseline.
	deadline := time.After(2 * time.Second)
	for publish.TriggerCalls() < 2 || establish.TriggerCalls() < 2 {
		select {
		case <-deadline:
			t.Fatalf("ticker did not trigger controllers in time: publish=%d establish=%d",
				publish.TriggerCalls(), establish.TriggerCalls())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRun_ShouldExecuteBootstrapOnStart(t *testing.T) {
	d, boot, _, _ := newTestDaemon(t, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for boot.Calls() < 1 {
		select {
		case <-deadline:
			t.Fatal("bootCtrl.Execute was not called")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRun_ShouldWaitForWorkerToFinishBeforeReturning(t *testing.T) {
	boot := &fakeBootstrap{}
	publish := &fakePublish{}
	establish := &fakeEstablish{}
	pingMonitor := newSlowPingMonitor(150 * time.Millisecond)

	cfg := &config.Config{RefreshInterval: time.Hour}
	logger := zerolog.Nop()
	d := New(cfg, boot, publish, establish, pingMonitor, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for boot.Calls() < 1 {
		select {
		case <-deadline:
			t.Fatal("bootCtrl.Execute was not called")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	select {
	case <-pingMonitor.finished:
	default:
		t.Fatal("Run returned before pingMonitor.Execute finished — wg.Wait() did not join the worker goroutine")
	}
}

func TestRunOneshot_ShouldWaitForEstablishWorkerToFinishBeforeReturning(t *testing.T) {
	boot := &fakeBootstrap{}
	publish := &fakePublish{}
	establish := newSlowEstablish(150 * time.Millisecond)
	pingMonitor := &fakePingMonitor{}

	cfg := &config.Config{RefreshInterval: time.Hour}
	logger := zerolog.Nop()
	d := New(cfg, boot, publish, establish, pingMonitor, &logger)
	d.sleep = func(time.Duration) {} // skip RunOneshot's real multi-second pacing

	d.RunOneshot(context.Background())

	select {
	case <-establish.finished:
	default:
		t.Fatal("RunOneshot returned before establishCtrl.Run finished — wg.Wait() did not join the worker goroutine")
	}
}

func TestRunOneshot_ShouldRunExactlyThreeIterationsAndWaitForCompletion(t *testing.T) {
	d, boot, publish, establish := newTestDaemon(t, time.Hour)
	d.sleep = func(time.Duration) {} // skip RunOneshot's real multi-second pacing

	d.RunOneshot(context.Background())

	if got := boot.Calls(); got != 1 {
		t.Errorf("bootCtrl.Execute calls = %d, want 1", got)
	}
	if got := publish.ExecuteCalls(); got != 3 {
		t.Errorf("publishCtrl.Execute calls = %d, want 3", got)
	}
	if got := establish.TriggerCalls(); got != 3 {
		t.Errorf("establishCtrl.Trigger calls = %d, want 3", got)
	}
	if got := establish.WaitForCompletionCalls(); got != 1 {
		t.Errorf("establishCtrl.WaitForCompletion calls = %d, want 1", got)
	}
}
