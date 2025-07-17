package main

import (
	"context"
	"flag"
	"os"
)

func main() {
	ctx := context.Background()

	oneshot := flag.Bool("oneshot", false, "run in oneshot mode (publish and establish 3 times, then exit)")
	configPath := flag.String("config", "", "path to configuration file")
	flag.Parse()

	daemon, err := setup(*configPath)
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
