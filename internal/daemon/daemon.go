package daemon

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

type Daemon struct {
	config        *config.Config
	bootCtrl      *ctrl.BootstrapController
	publishCtrl   *ctrl.PublishController
	establishCtrl *ctrl.EstablishController
	pingMonitor   *ctrl.PingMonitorController
	logger        zerolog.Logger
}

func New(
	config *config.Config,
	boot *ctrl.BootstrapController,
	publish *ctrl.PublishController,
	establish *ctrl.EstablishController,
	pingMonitor *ctrl.PingMonitorController,
	logger *zerolog.Logger) *Daemon {
	return &Daemon{
		config:        config,
		bootCtrl:      boot,
		publishCtrl:   publish,
		establishCtrl: establish,
		pingMonitor:   pingMonitor,
		logger:        logger.With().Str("component", "daemon").Logger(),
	}
}

func (d *Daemon) Run(ctx context.Context) {
	daemonCtx, cancel := context.WithCancel(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		d.logger.Info().Msg("shutting down")
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	d.bootCtrl.Execute(daemonCtx)

	// Start controller workers
	go d.publishCtrl.Run(daemonCtx)
	go d.establishCtrl.Run(daemonCtx)

	// Initialize ping monitoring for all peers
	go d.pingMonitor.Execute(daemonCtx)

	// Trigger initial publish and refresh
	d.publishCtrl.Trigger()
	d.establishCtrl.Trigger(daemonCtx)

	d.logger.Info().Msgf("daemon started with refresh interval %s", d.config.RefreshInterval)

	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-signalChan:
			return
		case <-ticker.C:
			d.logger.Info().Msg("refreshing peers")
			d.publishCtrl.Trigger()
			d.establishCtrl.Trigger(daemonCtx)
		}
	}
}

func (d *Daemon) RunOneshot(ctx context.Context) {
	d.logger.Info().Msg("running in oneshot mode")

	// Bootstrap first
	d.bootCtrl.Execute(ctx)

	// Run publish and establish 3 times
	for i := 1; i <= 3; i++ {
		d.logger.Info().Msgf("oneshot iteration %d/3", i)

		// Publish peer information
		d.publishCtrl.Execute(ctx)

		// Wait a bit for publish to complete
		time.Sleep(2 * time.Second)

		// Refresh to get peer information
		d.establishCtrl.Trigger(ctx)

		// Wait for peers to be processed
		time.Sleep(2 * time.Second)

		// Wait between iterations
		if i < 3 {
			time.Sleep(3 * time.Second)
		}
	}

	// Wait for all peers to be processed
	d.establishCtrl.WaitForCompletion(ctx)

	d.logger.Info().Msg("oneshot mode completed")
}
