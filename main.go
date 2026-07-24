package main

import (
	"context"
	"flag"
	"os"

	"github.com/tjjh89017/stunmesh-go/internal/config"
)

func main() {
	ctx := context.Background()

	var (
		oneshot    bool
		configFile string
		configDir  string
	)

	// -c and --config are the same destination, so the usage text is shared.
	const configFileUsage = "path to the config file (takes priority over --config-dir)"

	flag.BoolVar(&oneshot, "oneshot", false, "run in oneshot mode (publish and establish 3 times, then exit)")
	flag.StringVar(&configFile, "c", "", configFileUsage)
	flag.StringVar(&configFile, "config", "", configFileUsage)
	flag.StringVar(&configDir, "config-dir", "", "directory containing config.yaml (ignored if -c/--config is set)")

	flag.Parse()

	config.ConfigFile = configFile
	config.ConfigDir = configDir

	daemon, err := setup()
	if err != nil {
		panic(err)
	}

	if oneshot {
		daemon.RunOneshot(ctx)
		os.Exit(0)
	}

	daemon.Run(ctx)
}
