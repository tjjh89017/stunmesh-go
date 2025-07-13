package repo

import (
	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

var DefaultSet = wire.NewSet(
	NewPeers,
	wire.Bind(new(ctrl.PeerRepository), new(*Peers)),
	NewDevices,
	wire.Bind(new(ctrl.DeviceRepository), new(*Devices)),
)
