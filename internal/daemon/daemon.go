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
	logger        zerolog.Logger
}

func New(
	config *config.Config,
	queue *queue.Queue[entity.PeerId],
	boot *ctrl.BootstrapController,
	publish *ctrl.PublishController,
	establish *ctrl.EstablishController,
	refresh *ctrl.RefreshController,
	logger *zerolog.Logger) *Daemon {
	return &Daemon{
		config:        config,
		queue:         queue,
		bootCtrl:      boot,
		publishCtrl:   publish,
		establishCtrl: establish,
		refreshCtrl:   refresh,
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
	go d.refreshCtrl.Execute(daemonCtx)
	go d.publishCtrl.Execute(daemonCtx)
	d.logger.Info().Msgf("daemon started with refresh interval %s", d.config.RefreshInterval)

	ticker := time.NewTicker(d.config.RefreshInterval)

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
