package daemon

import (
	"context"
	"encoding/base64"
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
		signal.Stop(signalChan)
		close(signalChan)
		cancel()
	}()

	d.bootCtrl.Execute(daemonCtx)
	
	// Initialize ping monitoring for all peers
	d.initializePingMonitoring(daemonCtx)
	
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

func (d *Daemon) RunOneshot(ctx context.Context) {
	d.logger.Info().Msg("running in oneshot mode")
	
	// Bootstrap first
	d.bootCtrl.Execute(ctx)
	
	// Initialize ping monitoring for all peers
	d.initializePingMonitoring(ctx)
	
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

func (d *Daemon) initializePingMonitoring(ctx context.Context) {
	// Get all configured peers from all interfaces
	for _, deviceConfig := range d.config.Interfaces {
		for peerName, peerConfig := range deviceConfig.Peers {
			// Only add peers that have ping monitoring enabled
			if peerConfig.Ping != nil && peerConfig.Ping.Enabled && peerConfig.Ping.Target != "" {
				// Create a dummy peer ID for ping monitoring
				// In a real implementation, you'd get this from the actual peer
				peerPublicKey, err := base64.StdEncoding.DecodeString(peerConfig.PublicKey)
				if err != nil {
					d.logger.Warn().Err(err).Str("peer", peerName).Msg("failed to decode peer public key")
					continue
				}
				
				// For now, use a dummy local public key - this should be improved
				localPublicKey := make([]byte, 32)
				peerId := entity.NewPeerId(localPublicKey, peerPublicKey)
				
				// Convert config.PingConfig to entity.PeerPingConfig
				pingConfig := entity.PeerPingConfig{
					Enabled:  peerConfig.Ping.Enabled,
					Target:   peerConfig.Ping.Target,
					Interval: peerConfig.Ping.Interval,
					Timeout:  peerConfig.Ping.Timeout,
				}
				
				d.pingMonitor.AddPeer(peerId, pingConfig)
				
				d.logger.Info().
					Str("peer", peerName).
					Str("target", peerConfig.Ping.Target).
					Msg("added peer to ping monitoring")
			}
		}
	}
	
	// Start ping monitoring
	go d.pingMonitor.Start(ctx)
}
