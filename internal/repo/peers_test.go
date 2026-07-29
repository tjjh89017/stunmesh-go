package repo_test

import (
	"context"
	"sync"
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
		"ipv4",
		entity.PeerPingConfig{Enabled: false},
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
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
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
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
				),
				entity.NewPeer(
					entity.NewPeerId([]byte{1}, []byte{1}),
					"wg1",
					[32]byte{},
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
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
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
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
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
				),
				entity.NewPeer(
					entity.NewPeerId([]byte{1}, []byte{1}),
					"wg1",
					[32]byte{},
					"exec",
					"ipv4",
					entity.PeerPingConfig{Enabled: false},
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

// Test_PeerRepository_ConcurrentAccess spawns concurrent readers and
// writers on a shared repo instance to exercise the sync.RWMutex under
// `go test -race`. It intentionally mixes Save (write lock) with Find and
// List (read lock) so the writer/reader distinction is actually exercised,
// rather than only ever taking one side of the lock.
func Test_PeerRepository_ConcurrentAccess(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockWgClient := mock.NewMockWireGuardClient(mockCtrl)

	const goroutinesPerOp = 50

	peers := repo.NewPeers(mockWgClient)

	var wg sync.WaitGroup
	wg.Add(goroutinesPerOp * 3)

	for i := 0; i < goroutinesPerOp; i++ {
		go func(i int) {
			defer wg.Done()

			id := entity.NewPeerId([]byte{byte(i)}, []byte{byte(i)})
			peer := entity.NewPeer(
				id,
				"wg0",
				[32]byte{},
				"exec",
				"ipv4",
				entity.PeerPingConfig{Enabled: false},
			)
			peers.Save(context.TODO(), peer)
		}(i)

		go func(i int) {
			defer wg.Done()

			id := entity.NewPeerId([]byte{byte(i)}, []byte{byte(i)})
			// The target peer may not have been saved yet by its writer
			// goroutine; a not-found error is expected and fine, what
			// matters is that access is race-free.
			_, _ = peers.Find(context.TODO(), id)
		}(i)

		go func() {
			defer wg.Done()

			_, err := peers.List(context.TODO())
			if err != nil {
				t.Errorf("PeerRepository.List() error = %v", err)
			}
		}()
	}

	wg.Wait()
}
