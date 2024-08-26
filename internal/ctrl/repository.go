package ctrl

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type PeerRepository interface {
	Find(ctx context.Context, id entity.PeerId) (*entity.Peer, error)
	Save(ctx context.Context, peer *entity.Peer)
}
