package repo

import (
	"context"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

var _ ctrl.PeerRepository = &Peers{}
var _ entity.PeerSearcher = &Peers{}

type Peers struct {
	wgCtrl   WireGuardClient
	mutex    sync.RWMutex
	entities map[entity.PeerId]*entity.Peer
}

func NewPeers(wgCtrl WireGuardClient) *Peers {
	return &Peers{
		wgCtrl:   wgCtrl,
		entities: make(map[entity.PeerId]*entity.Peer),
	}
}

func (r *Peers) List(ctx context.Context) ([]*entity.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	peers := make([]*entity.Peer, 0, len(r.entities))
	for _, peer := range r.entities {
		peers = append(peers, peer)
	}

	return peers, nil
}

func (r *Peers) ListByDevice(ctx context.Context, deviceName entity.DeviceId) ([]*entity.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	peers := make([]*entity.Peer, 0)
	for _, peer := range r.entities {
		if peer.DeviceName() == string(deviceName) {
			peers = append(peers, peer)
		}
	}

	return peers, nil
}

// NOTE: will replace the above ListByDevice
func (r *Peers) SearchByDevice(ctx context.Context, deviceName entity.DeviceId) ([]*entity.Peer, error) {
	device, err := r.wgCtrl.Device(string(deviceName))
	if err != nil {
		return nil, err
	}

	peers := make([]*entity.Peer, len(device.Peers))
	for i, peer := range device.Peers {
		peers[i] = entity.NewPeer(
			entity.NewPeerId(device.PublicKey[:], peer.PublicKey[:]),
			device.Name,
			peer.PublicKey,
		)
	}

	return peers, nil
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
