package repo_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
	mock "github.com/tjjh89017/stunmesh-go/internal/repo/mock"
	"go.uber.org/mock/gomock"
)

func Test_PeerRepository_Find(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)

	peerId := entity.NewPeerId([]byte{}, []byte{})

	peer := entity.NewPeer(
		peerId,
		"wg0",
		[32]byte{},
		"cloudflare",
	)

	peers := repo.NewPeers(mockWgClient)
	peers.Save(context.TODO(), peer)

	tests := []struct {
		name    string
		peerId  entity.PeerId
		wantErr bool
	}{
		{
			name:    "find peer",
			peerId:  peerId,
			wantErr: false,
		},
		{
			name:    "find non-existent peer",
			peerId:  entity.NewPeerId([]byte{1}, []byte{1}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := peers.Find(context.TODO(), tt.peerId)
			if (err != nil) != tt.wantErr {
				t.Errorf("PeerRepository.Find() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_PeerRepository_List(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)

	tests := []struct {
		name  string
		peers []*entity.Peer
	}{
		{
			name:  "no peers",
			peers: []*entity.Peer{},
		},
		{
			name: "one peer",
			peers: []*entity.Peer{
				entity.NewPeer(
					entity.NewPeerId([]byte{}, []byte{}),
					"wg0",
					[32]byte{},
					"cloudflare",
				),
			},
		},
		{
			name: "two peers",
			peers: []*entity.Peer{
				entity.NewPeer(
					entity.NewPeerId([]byte{}, []byte{}),
					"wg0",
					[32]byte{},
					"cloudflare",
				),
				entity.NewPeer(
					entity.NewPeerId([]byte{1}, []byte{1}),
					"wg1",
					[32]byte{},
					"exec",
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			peers := repo.NewPeers(mockWgClient)
			for _, peer := range tt.peers {
				peers.Save(context.TODO(), peer)
			}

			entities, err := peers.List(context.TODO())
			if err != nil {
				t.Errorf("PeerRepository.List() error = %v", err)
				return
			}

			expectedSize := len(tt.peers)
			actualSize := len(entities)
			if actualSize != expectedSize {
				t.Errorf("PeerRepository.List() = %v, want %v", actualSize, expectedSize)
				return
			}
		})
	}
}

func Test_PeerListByDevice(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)

	tests := []struct {
		name       string
		deviceName entity.DeviceId
		peers      []*entity.Peer
		expected   int
	}{
		{
			name:       "no peers",
			deviceName: "wg0",
			peers:      []*entity.Peer{},
			expected:   0,
		},
		{
			name:       "one peer",
			deviceName: "wg0",
			peers: []*entity.Peer{
				entity.NewPeer(
					entity.NewPeerId([]byte{}, []byte{}),
					"wg0",
					[32]byte{},
					"cloudflare",
				),
			},
			expected: 1,
		},
		{
			name:       "two peers with one matching device",
			deviceName: "wg0",
			peers: []*entity.Peer{
				entity.NewPeer(
					entity.NewPeerId([]byte{}, []byte{}),
					"wg0",
					[32]byte{},
					"cloudflare",
				),
				entity.NewPeer(
					entity.NewPeerId([]byte{1}, []byte{1}),
					"wg1",
					[32]byte{},
					"exec",
				),
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			peers := repo.NewPeers(mockWgClient)
			for _, peer := range tt.peers {
				peers.Save(context.TODO(), peer)
			}

			entities, err := peers.ListByDevice(context.TODO(), tt.deviceName)
			if err != nil {
				t.Errorf("PeerRepository.ListByDevice() error = %v", err)
				return
			}

			actualSize := len(entities)
			if actualSize != tt.expected {
				t.Errorf("PeerRepository.ListByDevice() = %v, want %v", actualSize, tt.expected)
				return
			}
		})
	}
}
