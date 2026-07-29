package main

import (
	"context"
	"flag"
	"fmt"
	"runtime/debug"

	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// version comes from the VCS build info the Go toolchain stamps into the
// binary: the exact tag when built at one, a pseudo-version otherwise.
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	ctx := context.Background()

	var (
		oneshot     bool
		showVersion bool
		configFile  string
		configDir   string
	)

	// -c and --config are the same destination, so the usage text is shared.
	const configFileUsage = "path to the config file (takes priority over --config-dir)"

	flag.BoolVar(&oneshot, "oneshot", false, "run in oneshot mode (publish and establish 3 times, then exit)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&configFile, "c", "", configFileUsage)
	flag.StringVar(&configFile, "config", "", configFileUsage)
	flag.StringVar(&configDir, "config-dir", "", "directory containing config.yaml (ignored if -c/--config is set)")

	flag.Parse()

	if showVersion {
		fmt.Println(version())
		return
	}

	cfg, err := config.Load(configFile, configDir)
	if err != nil {
		panic(err)
	}

	daemon, cleanup, err := setup(cfg)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	if oneshot {
		daemon.RunOneshot(ctx)
		return
	}

	daemon.Run(ctx)
}
