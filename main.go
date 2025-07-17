package main

import (
	"context"
	"flag"
	"os"
)

func main() {
	ctx := context.Background()

	oneshot := flag.Bool("oneshot", false, "run in oneshot mode (publish and establish 3 times, then exit)")
	flag.Parse()

	daemon, err := setup()
	if err != nil {
		panic(err)
	}

	if *oneshot {
		daemon.RunOneshot(ctx)
		os.Exit(0)
	} else {
		daemon.Run(ctx)
	}
}
