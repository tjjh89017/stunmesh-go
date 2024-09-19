package main

import (
	"context"
)

func main() {
	ctx := context.Background()

	daemon, err := setup()
	if err != nil {
		panic(err)
	}

	daemon.Run(ctx)
}
