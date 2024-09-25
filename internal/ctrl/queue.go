//go:generate mockgen -destination=./mock/mock_queue.go -package=mock_ctrl . RefreshQueue
package ctrl

import "github.com/tjjh89017/stunmesh-go/internal/entity"

type RefreshQueue interface {
	Enqueue(entity entity.PeerId)
}
