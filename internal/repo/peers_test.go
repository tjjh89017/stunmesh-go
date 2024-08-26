package repo_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
)

func Test_PeerRepository_Find(t *testing.T) {
	peerId := entity.NewPeerId([]byte{}, []byte{})

	peer := entity.NewPeer(
		peerId,
		"wg0",
		8080,
		[32]byte{},
	)

	peers := repo.NewPeers()
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
