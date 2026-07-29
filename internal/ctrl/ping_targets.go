//go:generate mockgen -destination=./mock/mock_ping_targets.go -package=mock_ctrl . Publisher,Establisher

package ctrl

import "github.com/tjjh89017/stunmesh-go/internal/entity"

// Publisher is the narrow slice of *PublishController that PingMonitorController needs.
type Publisher interface {
	TriggerForPeer(peerId entity.PeerId)
}

// Establisher is the narrow slice of *EstablishController that PingMonitorController needs.
type Establisher interface {
	TriggerForPeer(peerId entity.PeerId)
}
