//go:generate mockgen -destination=./mock/mock_peer.go -package=mock_entity . PeerSearcher,PeerAllower
package entity

import "context"

type PeerSearcher interface {
	SearchByDevice(context.Context, DeviceId) ([]*Peer, error)
}

type PeerAllower interface {
	Allow(ctx context.Context, deviceName string, publicKey []byte, peerId PeerId) bool
}

type FilterPeerService struct {
	searcher PeerSearcher
	allower  PeerAllower
}

func NewFilterPeerService(searcher PeerSearcher, allower PeerAllower) *FilterPeerService {
	return &FilterPeerService{
		searcher: searcher,
		allower:  allower,
	}
}

func (svc *FilterPeerService) Execute(ctx context.Context, deviceName DeviceId, publicKey []byte) ([]*Peer, error) {
	peers, err := svc.searcher.SearchByDevice(ctx, deviceName)
	if err != nil {
		return nil, err
	}

	allowedPeers := make([]*Peer, 0, len(peers))
	for _, peer := range peers {
		if svc.allower.Allow(ctx, string(deviceName), publicKey, peer.Id()) {
			allowedPeers = append(allowedPeers, peer)
		}
	}

	return allowedPeers, nil
}
