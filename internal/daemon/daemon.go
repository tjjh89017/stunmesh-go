package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// BootstrapExecutor is the subset of ctrl.BootstrapController that Daemon calls.
type BootstrapExecutor interface {
	Execute(ctx context.Context) error
}

// PublishRunner is the subset of ctrl.PublishController that Daemon calls.
type PublishRunner interface {
	Execute(ctx context.Context)
	Run(ctx context.Context)
	Trigger()
}

// EstablishRunner is the subset of ctrl.EstablishController that Daemon calls.
type EstablishRunner interface {
	Run(ctx context.Context)
	Trigger(ctx context.Context)
	WaitForCompletion(ctx context.Context)
}

// PingMonitorExecutor is the subset of ctrl.PingMonitorController that Daemon calls.
type PingMonitorExecutor interface {
	Execute(ctx context.Context)
}

type Daemon struct {
	config        *config.Config
	bootCtrl      BootstrapExecutor
	publishCtrl   PublishRunner
	establishCtrl EstablishRunner
	pingMonitor   PingMonitorExecutor
	logger        zerolog.Logger
	wg            sync.WaitGroup
	// sleep is overridden in tests to avoid RunOneshot's real multi-second pacing.
	sleep func(time.Duration)
}

func New(
	config *config.Config,
	boot BootstrapExecutor,
	publish PublishRunner,
	establish EstablishRunner,
	pingMonitor PingMonitorExecutor,
	logger *zerolog.Logger) *Daemon {
	return &Daemon{
		config:        config,
		bootCtrl:      boot,
		publishCtrl:   publish,
		establishCtrl: establish,
		pingMonitor:   pingMonitor,
		logger:        logger.With().Str("component", "daemon").Logger(),
		sleep:         time.Sleep,
	}
}

// Run runs the daemon until ctx is cancelled. Shutdown (including OS signal
// handling) is the caller's responsibility; Run itself is purely ctx-driven.
func (d *Daemon) Run(ctx context.Context) error {
	daemonCtx, cancel := context.WithCancel(ctx)

	defer func() {
		d.logger.Info().Msg("shutting down")
		cancel()
		// Wait for workers to stop before the caller's cleanup() (e.g. closing
		// the WireGuard client) runs, so they cannot use it after it's closed.
		d.wg.Wait()
	}()

	if err := d.bootCtrl.Execute(daemonCtx); err != nil {
		return err
	}

	// Start controller workers
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.publishCtrl.Run(daemonCtx)
	}()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.establishCtrl.Run(daemonCtx)
	}()

	// Initialize ping monitoring for all peers
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.pingMonitor.Execute(daemonCtx)
	}()

	// Trigger initial publish and refresh
	d.publishCtrl.Trigger()
	d.establishCtrl.Trigger(daemonCtx)

	d.logger.Info().Msgf("daemon started with refresh interval %s", d.config.RefreshInterval)

	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-daemonCtx.Done():
			return nil
		case <-ticker.C:
			d.logger.Info().Msg("refreshing peers")
			d.publishCtrl.Trigger()
			d.establishCtrl.Trigger(daemonCtx)
		}
	}
}

func (d *Daemon) RunOneshot(ctx context.Context) error {
	d.logger.Info().Msg("running in oneshot mode")

	// Own cancellation scope so the establishCtrl.Run worker below is
	// stopped when this method returns, rather than outliving it for as
	// long as the caller's context happens to stay alive.
	oneshotCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Bootstrap first
	if err := d.bootCtrl.Execute(oneshotCtx); err != nil {
		return err
	}

	// Start establish controller worker
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.establishCtrl.Run(oneshotCtx)
	}()

	// Run publish and establish 3 times
	for i := 1; i <= 3; i++ {
		d.logger.Info().Msgf("oneshot iteration %d/3", i)

		// Publish peer information
		d.publishCtrl.Execute(oneshotCtx)

		// Wait a bit for publish to complete
		d.sleep(2 * time.Second)

		// Refresh to get peer information
		d.establishCtrl.Trigger(oneshotCtx)

		// Wait for peers to be processed
		d.sleep(2 * time.Second)

		// Wait between iterations
		if i < 3 {
			d.sleep(3 * time.Second)
		}
	}

	// Wait for all peers to be processed
	d.establishCtrl.WaitForCompletion(oneshotCtx)

	// Stop the establish worker and wait for it to actually exit before
	// returning, so the caller's deferred cleanup() (e.g. closing the
	// WireGuard client) cannot race it. cancel() is idempotent, so the
	// deferred cancel() above is a harmless no-op afterward.
	cancel()
	d.wg.Wait()

	d.logger.Info().Msg("oneshot mode completed")
	return nil
}
