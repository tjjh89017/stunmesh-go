package entity

import "github.com/google/wire"

var DefaultSet = wire.NewSet(
	NewFilterPeerService,
)
