package stun

import (
	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

var DefaultSet = wire.NewSet(
	NewResolver,
	wire.Bind(new(ctrl.StunResolver), new(*Resolver)),
)
