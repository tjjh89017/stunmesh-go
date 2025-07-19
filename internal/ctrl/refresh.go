package ctrl

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

type RefreshController struct {
	peers  PeerRepository
	queue  RefreshQueue
	logger zerolog.Logger
	mu     sync.Mutex
}

func NewRefreshController(peers PeerRepository, queue RefreshQueue, logger *zerolog.Logger) *RefreshController {
	return &RefreshController{
		peers:  peers,
		queue:  queue,
		logger: logger.With().Str("controller", "refresh").Logger(),
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
