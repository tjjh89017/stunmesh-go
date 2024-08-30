package ctrl

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type DeviceRepository interface {
	Find(ctx context.Context, name entity.DeviceId) (*entity.Device, error)
	Save(ctx context.Context, device *entity.Device)
}

type PeerRepository interface {
	List(ctx context.Context) ([]*entity.Peer, error)
	Find(ctx context.Context, id entity.PeerId) (*entity.Peer, error)
	Save(ctx context.Context, peer *entity.Peer)
}
