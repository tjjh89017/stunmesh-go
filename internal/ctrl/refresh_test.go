package ctrl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	mock "github.com/tjjh89017/stunmesh-go/internal/ctrl/mock"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	gomock "go.uber.org/mock/gomock"
)

func TestRefresh_Execute(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockQueue := mock.NewMockRefreshQueue(mockCtrl)

	peers := []*entity.Peer{
		entity.NewPeer(
			entity.NewPeerId([]byte{0x01}, []byte{0x02}),
			"wg0",
			[32]byte{},
			"cloudflare",
			entity.PeerPingConfig{Enabled: false},
		),
	}

	mockPeers.EXPECT().List(gomock.Any()).Return(peers, nil)
	mockQueue.EXPECT().Enqueue(peers[0].Id())

	logger := zerolog.Nop()
	refresh := ctrl.NewRefreshController(
		mockPeers,
		mockQueue,
		&logger,
	)

	refresh.Execute(context.TODO())
}

func TestRefresh_ExecuteEmpty(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockQueue := mock.NewMockRefreshQueue(mockCtrl)

	mockPeers.EXPECT().List(gomock.Any()).Return([]*entity.Peer{}, nil)

	logger := zerolog.Nop()
	refresh := ctrl.NewRefreshController(
		mockPeers,
		mockQueue,
		&logger,
	)

	refresh.Execute(context.TODO())
}

func TestRefresh_ListError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockPeers := mock.NewMockPeerRepository(mockCtrl)
	mockQueue := mock.NewMockRefreshQueue(mockCtrl)

	repoError := errors.New("unable to list peers")
	mockPeers.EXPECT().List(gomock.Any()).Return(nil, repoError)

	logger := zerolog.Nop()
	refresh := ctrl.NewRefreshController(
		mockPeers,
		mockQueue,
		&logger,
	)

	refresh.Execute(context.TODO())
}
