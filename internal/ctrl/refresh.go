package ctrl

import (
	"context"
	"log"
)

type RefreshController struct {
	peers PeerRepository
	queue RefreshQueue
}

func NewRefreshController(peers PeerRepository, queue RefreshQueue) *RefreshController {
	return &RefreshController{
		peers: peers,
		queue: queue,
	}
}

func (c *RefreshController) Execute(ctx context.Context) {
	peers, err := c.peers.List(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	for _, peer := range peers {
		c.queue.Enqueue(peer.Id())
	}
}
