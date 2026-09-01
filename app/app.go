// Package app is the composition root for stunmesh-go: it wires the daemon
// and its dependencies (config, wg client, STUN, plugins, controllers) and
// exposes a small embeddable API so an external Go program can run the
// daemon in-process and stop/restart it via repeated New/Close cycles.
package app

import (
	"context"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/daemon"
)

// Options configures a new App. Its fields mirror the stunmesh-go CLI's
// -c/--config and --config-dir flags.
type Options struct {
	// ConfigFile is the exact config file to read; takes priority over ConfigDir.
	ConfigFile string
	// ConfigDir is the directory searched for config.yaml then config.yml
	// when ConfigFile is unset.
	ConfigDir string
}

// App is a fully wired stunmesh-go daemon. Call Run or RunOneshot to drive
// it, and Close to release its resources once done.
type App struct {
	daemon  *daemon.Daemon
	cleanup func()
	once    sync.Once
}

// New loads configuration per opts and wires the daemon and its dependencies.
func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigFile, opts.ConfigDir)
	if err != nil {
		return nil, err
	}

	d, cleanup, err := setup(cfg)
	if err != nil {
		return nil, err
	}

	return &App{daemon: d, cleanup: cleanup}, nil
}

// Run runs the daemon until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	return a.daemon.Run(ctx)
}

// RunOneshot runs the daemon's publish/establish cycle three times, then returns.
func (a *App) RunOneshot(ctx context.Context) error {
	return a.daemon.RunOneshot(ctx)
}

// Close releases resources acquired by New (the WireGuard client and the
// wgproxy manager). It is idempotent and safe to call more than once.
func (a *App) Close() {
	a.once.Do(a.cleanup)
}
