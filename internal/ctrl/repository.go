//go:generate mockgen -destination=./mock/mock_repository.go -package=mock_ctrl . DeviceRepository,PeerRepository

package ctrl

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type DeviceRepository interface {
	List(ctx context.Context) ([]*entity.Device, error)
	Find(ctx context.Context, name entity.DeviceId) (*entity.Device, error)
	Save(ctx context.Context, device *entity.Device)
	// UpdateStatus records the local host's latest STUN discovery result for a device.
	UpdateStatus(ctx context.Context, name entity.DeviceId, s entity.DeviceStatus)
	// Status returns the last recorded discovery result for a device, and whether one has been recorded yet.
	Status(ctx context.Context, name entity.DeviceId) (entity.DeviceStatus, bool)
}

type PeerRepository interface {
	List(ctx context.Context) ([]*entity.Peer, error)
	ListByDevice(ctx context.Context, deviceName entity.DeviceId) ([]*entity.Peer, error)
	Find(ctx context.Context, id entity.PeerId) (*entity.Peer, error)
	Save(ctx context.Context, peer *entity.Peer)
}
