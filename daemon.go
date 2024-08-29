package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

const RefreshInterval = time.Duration(10) * time.Minute

func Run(ctx context.Context, privateKey [32]byte, publish *ctrl.PublishController, establish *ctrl.EstablishController, peers []*entity.Peer) {
	daemonCtx, cancel := context.WithCancel(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		log.Println("Shutting down")
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-signalChan:
			return
		case <-time.Tick(RefreshInterval):
			log.Println("Refreshing peers")

			for _, peer := range peers {
				publish.Execute(daemonCtx, peer.Id())
				establish.Execute(daemonCtx, peer.Id())
			}
		}
	}
}
