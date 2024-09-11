package ctrl

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type DeviceRepository interface {
	List(ctx context.Context) ([]*entity.Device, error)
	Find(ctx context.Context, name entity.DeviceId) (*entity.Device, error)
	Save(ctx context.Context, device *entity.Device)
}

type PeerRepository interface {
	List(ctx context.Context) ([]*entity.Peer, error)
	ListByDevice(ctx context.Context, deviceName entity.DeviceId) ([]*entity.Peer, error)
	Find(ctx context.Context, id entity.PeerId) (*entity.Peer, error)
	Save(ctx context.Context, peer *entity.Peer)
}
