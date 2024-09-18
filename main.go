package main

import (
	"context"

	"github.com/rs/zerolog"
)

func main() {
	ctx := context.Background()

	daemon, err := setup()
	if err != nil {
		zerolog.DefaultContextLogger.Panic().Err(err).Msg("failed to setup daemon")
	}

	daemon.Run(ctx)
}
