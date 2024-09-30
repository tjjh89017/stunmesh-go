package repo

import (
	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

var DefaultSet = wire.NewSet(
	NewPeers,
	wire.Bind(new(ctrl.PeerRepository), new(*Peers)),
	wire.Bind(new(entity.PeerSearcher), new(*Peers)),
	NewDevices,
	wire.Bind(new(ctrl.DeviceRepository), new(*Devices)),
)
