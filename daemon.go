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
	"github.com/tjjh89017/stunmesh-go/internal/queue"
)

const RefreshInterval = time.Duration(10) * time.Minute

func Run(ctx context.Context, queue *queue.Queue[entity.PeerId], publish *ctrl.PublishController, establish *ctrl.EstablishController, refresh *ctrl.RefreshController) {
	daemonCtx, cancel := context.WithCancel(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		log.Println("Shutting down")
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	go refresh.Execute(daemonCtx)

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-signalChan:
			return
		case peerId := <-queue.Dequeue():
			log.Printf("Processing peer %s", peerId)

			go publish.Execute(daemonCtx, peerId)
			go establish.Execute(daemonCtx, peerId)
		case <-time.Tick(RefreshInterval):
			log.Println("Refreshing peers")

			go refresh.Execute(daemonCtx)
		}
	}
}
