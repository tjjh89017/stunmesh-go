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
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
)

type Daemon struct {
	config        *config.Config
	queue         *queue.Queue[entity.PeerId]
	bootCtrl      *ctrl.BootstrapController
	publishCtrl   *ctrl.PublishController
	establishCtrl *ctrl.EstablishController
	refreshCtrl   *ctrl.RefreshController
	pingMonitor   *ctrl.PingMonitorController
	logger        zerolog.Logger
}

func New(
	config *config.Config,
	queue *queue.Queue[entity.PeerId],
	boot *ctrl.BootstrapController,
	publish *ctrl.PublishController,
	establish *ctrl.EstablishController,
	refresh *ctrl.RefreshController,
	pingMonitor *ctrl.PingMonitorController,
	logger *zerolog.Logger) *Daemon {
	return &Daemon{
		config:        config,
		queue:         queue,
		bootCtrl:      boot,
		publishCtrl:   publish,
		establishCtrl: establish,
		refreshCtrl:   refresh,
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
		d.queue.Close()
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	d.bootCtrl.Execute(daemonCtx)

	// Initialize ping monitoring for all peers
	go d.pingMonitor.Execute(daemonCtx)

	go d.refreshCtrl.Execute(daemonCtx)
	go d.publishCtrl.Execute(daemonCtx)
	d.logger.Info().Msgf("daemon started with refresh interval %s", d.config.RefreshInterval)

	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-signalChan:
			return
		case peerId := <-d.queue.Dequeue():
			d.logger.Info().Str("peer", peerId.String()).Msg("processing peer")

			go d.establishCtrl.Execute(daemonCtx, peerId)
		case <-ticker.C:
			d.logger.Info().Msg("refreshing peers")

			go d.publishCtrl.Execute(daemonCtx)
			go d.refreshCtrl.Execute(daemonCtx)
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
		d.refreshCtrl.Execute(ctx)

		// Process all peers in queue
		d.processAllPeers(ctx)

		// Wait between iterations
		if i < 3 {
			time.Sleep(3 * time.Second)
		}
	}

	d.logger.Info().Msg("oneshot mode completed")
}

func (d *Daemon) processAllPeers(ctx context.Context) {
	// Process all peers currently in the queue
	for {
		select {
		case peerId := <-d.queue.Dequeue():
			d.logger.Info().Str("peer", peerId.String()).Msg("processing peer in oneshot mode")
			d.establishCtrl.Execute(ctx, peerId)
		case <-time.After(1 * time.Second):
			// No more peers to process
			return
		case <-ctx.Done():
			return
		}
	}
}
