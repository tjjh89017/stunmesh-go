package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const RefreshInterval = time.Duration(10) * time.Minute

func Run(ctx context.Context, privateKey [32]byte, ctrl *Controller, peers []*Peer) {
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
				serializer := NewCryptoSerializer(privateKey, peer.PublicKey())

				ctrl.Publish(daemonCtx, serializer, peer)
				ctrl.Establish(daemonCtx, serializer, peer)
			}
		}
	}
}
