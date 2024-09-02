package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/config"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/queue"
)

type Daemon struct {
	config        *config.Config
	queue         *queue.Queue[entity.PeerId]
	publishCtrl   *ctrl.PublishController
	establishCtrl *ctrl.EstablishController
	refreshCtrl   *ctrl.RefreshController
}

func New(config *config.Config, queue *queue.Queue[entity.PeerId], publish *ctrl.PublishController, establish *ctrl.EstablishController, refresh *ctrl.RefreshController) *Daemon {
	return &Daemon{
		config:        config,
		queue:         queue,
		publishCtrl:   publish,
		establishCtrl: establish,
		refreshCtrl:   refresh,
	}
}

func (d *Daemon) Run(ctx context.Context) {
	daemonCtx, cancel := context.WithCancel(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		log.Println("Shutting down")
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	go d.refreshCtrl.Execute(daemonCtx)
	log.Printf("Daemon started with refresh interval %s", d.config.RefreshInterval)

	ticker := time.NewTicker(d.config.RefreshInterval)

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-signalChan:
			return
		case peerId := <-d.queue.Dequeue():
			log.Printf("Processing peer %s", peerId)

			go d.publishCtrl.Execute(daemonCtx, peerId)
			go d.establishCtrl.Execute(daemonCtx, peerId)
		case <-ticker.C:
			log.Println("Refreshing peers")

			go d.refreshCtrl.Execute(daemonCtx)
		}
	}
}
