package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/tjjh89017/stunmesh-go/app"
	// builtin/all is the shipped plugin set. A custom binary imports the
	// builtin/* packages it wants, or its own plugin package, instead.
	_ "github.com/tjjh89017/stunmesh-go/builtin/all"
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

	if err := run(oneshot, configFile, configDir); err != nil {
		fmt.Fprintln(os.Stderr, "stunmesh:", err)
		os.Exit(1)
	}
}

// run returns config, plugin and runtime errors to main, which prints them
// without a stack trace: they are user errors, not bugs.
func run(oneshot bool, configFile, configDir string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	daemon, err := app.New(app.Options{ConfigFile: configFile, ConfigDir: configDir})
	if err != nil {
		return err
	}
	defer daemon.Close()

	if oneshot {
		return daemon.RunOneshot(ctx)
	}
	return daemon.Run(ctx)
}
