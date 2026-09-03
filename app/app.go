// Package app is the composition root for stunmesh-go: it wires the daemon
// and its dependencies (config, wg client, STUN, plugins, controllers) and
// exposes a small embeddable API so an external Go program can run the
// daemon in-process and stop/restart it via repeated New/Close cycles.
//
// Lifecycle: New wires everything, Run (or RunOneshot) drives the daemon one
// call at a time, and Close cancels any in-flight Run, waits for it to
// return, then releases resources. New/Close cycles may be repeated.
package app

import (
	"context"
	"errors"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// ErrAlreadyRunning is returned by Run or RunOneshot when a previous call is
// still in flight on the same App.
var ErrAlreadyRunning = errors.New("app: already running")

// ErrClosed is returned by Run or RunOneshot after Close has been called.
var ErrClosed = errors.New("app: closed")

// Options configures a new App. Its fields mirror the stunmesh-go CLI's
// -c/--config and --config-dir flags.
type Options struct {
	// ConfigFile is the exact config file to read; takes priority over ConfigDir.
	ConfigFile string
	// ConfigDir is the directory searched for config.yaml then config.yml
	// when ConfigFile is unset.
	ConfigDir string
}

// runner is the subset of *daemon.Daemon that App drives.
type runner interface {
	Run(ctx context.Context) error
	RunOneshot(ctx context.Context) error
}

// App is a fully wired stunmesh-go daemon. Call Run or RunOneshot to drive
// it, and Close to release its resources once done.
type App struct {
	daemon  runner
	cleanup func()

	ctx    context.Context
	cancel context.CancelFunc

	// mu guards running and closed so a Run/RunOneshot call and a
	// concurrent Close agree on whether the call is admitted before it
	// registers with runs; without that, Close could observe an empty
	// runs and return before the just-admitted call finishes.
	mu      sync.Mutex
	running bool
	closed  bool
	runs    sync.WaitGroup

	closeOnce sync.Once
}

// New loads configuration per opts and wires the daemon and its dependencies.
func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigFile, opts.ConfigDir)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	d, cleanup, err := setup(ctx, cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	return &App{daemon: d, cleanup: cleanup, ctx: ctx, cancel: cancel}, nil
}

// Run runs the daemon until ctx is cancelled, the App is closed, or the
// daemon returns. It returns ErrAlreadyRunning if another Run or RunOneshot
// call is already in flight, and ErrClosed if Close has already been called.
func (a *App) Run(ctx context.Context) error {
	return a.run(ctx, a.daemon.Run)
}

// RunOneshot runs the daemon's publish/establish cycle three times, then
// returns. It returns ErrAlreadyRunning if another Run or RunOneshot call is
// already in flight, and ErrClosed if Close has already been called.
func (a *App) RunOneshot(ctx context.Context) error {
	return a.run(ctx, a.daemon.RunOneshot)
}

func (a *App) run(ctx context.Context, do func(context.Context) error) error {
	a.mu.Lock()
	switch {
	case a.closed:
		a.mu.Unlock()
		return ErrClosed
	case a.running:
		a.mu.Unlock()
		return ErrAlreadyRunning
	}
	a.running = true
	a.runs.Add(1)
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		a.runs.Done()
	}()

	// runCtx is cancelled when either the caller's ctx or the App's own
	// lifetime ctx is cancelled, while keeping ctx as the parent so its
	// values (e.g. logger) still flow through.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	stopAfterFunc := context.AfterFunc(a.ctx, stop)
	defer stopAfterFunc()

	return do(runCtx)
}

// Close cancels any in-flight Run or RunOneshot, waits for it to return,
// then releases resources acquired by New (the WireGuard client and the
// wgproxy manager). It is idempotent and safe to call more than once; Run
// and RunOneshot return ErrClosed if called afterward.
func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()

		a.cancel()
		a.runs.Wait()
		a.cleanup()
	})
}
