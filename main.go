package main

import (
	"context"
	"flag"
	"os"

	"github.com/tjjh89017/stunmesh-go/internal/config"
)

func main() {
	ctx := context.Background()

	oneshot := flag.Bool("oneshot", false, "run in oneshot mode (publish and establish 3 times, then exit)")

	const configFileUsage = "path to the config file (takes priority over --config-dir)"
	var configFile string
	flag.StringVar(&configFile, "c", "", configFileUsage)
	flag.StringVar(&configFile, "config", "", configFileUsage)

	var configDir string
	flag.StringVar(&configDir, "config-dir", "", "directory containing config.yaml (ignored if -c/--config is set)")

	flag.Parse()

	config.File = configFile
	config.Dir = configDir

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
