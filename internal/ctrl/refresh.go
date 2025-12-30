package ctrl

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	queuepkg "github.com/tjjh89017/stunmesh-go/internal/queue"
)

type RefreshController struct {
	peers        PeerRepository
	queue        RefreshQueue
	logger       zerolog.Logger
	mu           sync.Mutex
	triggerQueue *queuepkg.Queue[struct{}]
}

func NewRefreshController(peers PeerRepository, queue RefreshQueue, logger *zerolog.Logger) *RefreshController {
	return &RefreshController{
		peers:        peers,
		queue:        queue,
		logger:       logger.With().Str("controller", "refresh").Logger(),
		triggerQueue: queuepkg.NewBuffered[struct{}](queuepkg.TriggerQueueSize),
	}
}

func (c *RefreshController) Execute(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	peers, err := c.peers.List(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list peers")
		return
	}

	for _, peer := range peers {
		c.queue.Enqueue(peer.Id())
	}
}

// Run starts the worker goroutine that processes refresh triggers
func (c *RefreshController) Run(ctx context.Context) {
	c.logger.Info().Msg("refresh controller worker started")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("refresh controller worker stopped")
			return
		case <-c.triggerQueue.Dequeue():
			c.Execute(ctx)
		}
	}
}

// Trigger requests a refresh operation (non-blocking)
func (c *RefreshController) Trigger() {
	if c.triggerQueue.TryEnqueue(struct{}{}) {
		c.logger.Debug().Msg("refresh triggered")
	} else {
		c.logger.Debug().Msg("refresh queue full, skipping trigger")
	}
}
