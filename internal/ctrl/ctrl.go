package ctrl

import "github.com/google/wire"

var DefaultSet = wire.NewSet(
	NewBootstrapController,
	NewPublishController,
	NewEstablishController,
	NewRefreshController,
)
