package repo

import (
	"context"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type Peers struct {
	mutex    sync.RWMutex
	entities map[entity.PeerId]*entity.Peer
}

func NewPeers() *Peers {
	return &Peers{
		entities: make(map[entity.PeerId]*entity.Peer),
	}
}

func (r *Peers) Find(ctx context.Context, id entity.PeerId) (*entity.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	peer, ok := r.entities[id]
	if !ok {
		return nil, entity.ErrPeerNotFound
	}

	return peer, nil
}

func (r *Peers) Save(ctx context.Context, peer *entity.Peer) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.entities[peer.Id()] = peer
}
