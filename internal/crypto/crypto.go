package crypto

import (
	"github.com/google/wire"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

var DefaultSet = wire.NewSet(
	NewEndpoint,
	wire.Bind(new(ctrl.EndpointEncryptor), new(*Endpoint)),
	wire.Bind(new(ctrl.EndpointDecryptor), new(*Endpoint)),
)
