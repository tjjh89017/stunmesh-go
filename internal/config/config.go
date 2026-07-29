package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/wire"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
	"go.yaml.in/yaml/v3"
)

// DefaultSet excludes Load: main.go calls it directly to produce the
// *Config that setup() takes as a parameter, since Load's two string
// arguments (configFile, configDir) can't be told apart by Wire's
// type-based injection.
var DefaultSet = wire.NewSet(
	NewDeviceConfig,
)

// Defaults applied by Load when the config file omits the corresponding keys.
const (
	DefaultRefreshInterval  = 10 * time.Minute
	DefaultStunServer       = "stun.l.google.com:19302"
	DefaultPingInterval     = 1 * time.Second
	DefaultPingTimeout      = 1 * time.Second
	DefaultPingFixedRetries = 3
	DefaultLogFormat        = LogFormatConsole
	DefaultLogLevel         = "info"
)

// Log output formats accepted by log.format and --log-format.
const (
	LogFormatConsole = "console"
	LogFormatJSON    = "json"
)

// Accepted log settings, both the validation source and the list quoted in
// the error messages.
var (
	LogFormats = []string{LogFormatConsole, LogFormatJSON}
	LogLevels  = logLevels()
)

// logLevels asks zerolog for its own level names, in increasing severity.
// NoLevel is skipped: it stringifies to "" and is not a value to configure.
func logLevels() []string {
	var names []string
	for level := zerolog.TraceLevel; level <= zerolog.Disabled; level++ {
		if name := level.String(); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ConfigFileNames lists candidate file names inside a directory; the first
// one that exists wins.
var ConfigFileNames = []string{"config.yaml", "config.yml"}

// defaultSearchPaths lists directories (env-expanded) Load searches when
// neither configFile nor configDir is set.
var defaultSearchPaths = []string{
	"$STUNMESH_CONFIG_DIR",
	"/etc/stunmesh",
	"$HOME/.stunmesh",
	".",
}

var (
	ErrReadConfig      = errors.New("failed to read config")
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrNoStunServers   = errors.New("stun.addresses has no usable entries (empty list or only empty strings) and no stun.address is set")
)

type Logger struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type Stun struct {
	Address   string   `mapstructure:"address"`
	Addresses []string `mapstructure:"addresses"`
}

// GetServers merges the deprecated Address into Addresses, deduplicates
// preserving order, and falls back to DefaultStunServer if empty.
func (s *Stun) GetServers() []string {
	seen := make(map[string]struct{})
	var servers []string

	for _, addr := range append([]string{s.Address}, s.Addresses...) {
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		servers = append(servers, addr)
	}

	if len(servers) == 0 {
		return []string{DefaultStunServer}
	}

	return servers
}

type PingMonitor struct {
	Interval     time.Duration `mapstructure:"interval"`
	Timeout      time.Duration `mapstructure:"timeout"`
	FixedRetries int           `mapstructure:"fixed_retries"`
}

type Config struct {
	Interfaces      Interfaces                            `mapstructure:"interfaces"`
	Plugins         map[string]pluginapi.PluginDefinition `mapstructure:"plugins"`
	RefreshInterval time.Duration                         `mapstructure:"refresh_interval"`
	Log             Logger                                `mapstructure:"log"`
	Stun            Stun                                  `mapstructure:"stun"`
	PingMonitor     PingMonitor                           `mapstructure:"ping_monitor"`
}

// findConfigFile resolves the config file path, honoring configFile and configDir before
// the paths search; "" with nil error means not found (proceed with defaults).
func findConfigFile(configFile, configDir string, paths []string) (string, error) {
	if configFile != "" {
		return configFile, nil
	}

	if configDir != "" {
		for _, name := range ConfigFileNames {
			candidate := filepath.Join(configDir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		// Explicit configDir override must succeed: return the primary name so the read fails hard.
		return filepath.Join(configDir, ConfigFileNames[0]), nil
	}

	for _, path := range paths {
		expanded := os.ExpandEnv(path)
		if expanded == "" {
			continue
		}
		for _, name := range ConfigFileNames {
			candidate := filepath.Join(expanded, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	return "", nil
}

// Load reads and validates the config, searching for a file as documented in
// CLAUDE.md's "Config Loading Priority": configFile (exact file) takes
// priority over configDir (directory holding ConfigFileNames), which takes
// priority over the built-in search paths.
func Load(configFile, configDir string) (*Config, error) {
	return load(configFile, configDir, defaultSearchPaths)
}

// load is Load with the search paths taken as a parameter, so tests can
// point it at a temp directory without touching package state.
func load(configFile, configDir string, paths []string) (*Config, error) {
	var cfg Config
	// Pre-Decode defaults; yaml keys absent from the file leave these untouched.
	// Stun.Addresses must stay nil (not []) so "key absent" is distinguishable
	// from "explicitly empty list" after decoding.
	cfg.RefreshInterval = DefaultRefreshInterval
	cfg.PingMonitor.Interval = DefaultPingInterval
	cfg.PingMonitor.Timeout = DefaultPingTimeout
	cfg.PingMonitor.FixedRetries = DefaultPingFixedRetries
	cfg.Log.Format = DefaultLogFormat
	cfg.Log.Level = DefaultLogLevel

	path, err := findConfigFile(configFile, configDir, paths)
	if err != nil {
		return nil, err
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			// Any read failure is fatal: explicit overrides must fail hard,
			// and default-search paths already passed os.Stat.
			return nil, errors.Join(ErrReadConfig, err)
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, errors.Join(ErrReadConfig, err)
		}

		// Weakly typed input plus the duration and comma-separated-string-to-slice
		// hooks are required so quoted scalars and string list values still decode.
		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
			),
			WeaklyTypedInput: true,
			Result:           &cfg,
		})
		if err != nil {
			return nil, errors.Join(ErrUnmarshalConfig, err)
		}

		if err := decoder.Decode(raw); err != nil {
			return nil, errors.Join(ErrUnmarshalConfig, err)
		}
	}
	// path == "": no config file found; proceed with defaults.

	// STUN server semantics: key absent (nil) -> default + warn; explicitly
	// provided list with zero usable entries ("addresses: []", or only empty
	// strings, e.g. a "${STUN_SERVER}" template that external tooling expanded
	// to "" — stunmesh-go itself never expands env vars in config values) -> hard error;
	// otherwise leave the user-provided list untouched.
	effectiveAddresses := 0
	for _, addr := range cfg.Stun.Addresses {
		if addr != "" {
			effectiveAddresses++
		}
	}
	switch {
	case cfg.Stun.Addresses == nil && cfg.Stun.Address == "":
		// logger.NewLogger needs this config; use a throwaway console logger here.
		warnLog := entity.NewStartupLogger()
		warnLog.Warn().Msg("no STUN servers configured, defaulting to " + DefaultStunServer)
		cfg.Stun.Addresses = []string{DefaultStunServer}
	case effectiveAddresses == 0 && cfg.Stun.Address == "":
		return nil, ErrNoStunServers
	}

	cfg.Stun.Addresses = cfg.Stun.GetServers()
	cfg.Stun.Address = ""

	// An explicit `format: ""` / `level: ""` means unset, not invalid.
	if cfg.Log.Format == "" {
		cfg.Log.Format = DefaultLogFormat
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = DefaultLogLevel
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	return validateConfigForGOOS(cfg, runtime.GOOS)
}

// validateConfigForGOOS is validateConfig with goos taken as a parameter so
// the windows-only proxy.enabled rule is unit-testable from any platform.
// validateConfig (the real entry point) always calls it with runtime.GOOS.
func validateConfigForGOOS(cfg *Config, goos string) error {
	// Empty means unset, as it does for the protocol fields below; Load has
	// already replaced it with the default on the path that reads a file.
	if cfg.Log.Format != "" && !slices.Contains(LogFormats, cfg.Log.Format) {
		return fmt.Errorf("invalid log format '%s', must be one of: %s", cfg.Log.Format, strings.Join(LogFormats, ", "))
	}

	// ParseLevel is the authority on what it accepts, including case; LogLevels
	// only supplies the list its own error message leaves out.
	if cfg.Log.Level != "" {
		if _, err := zerolog.ParseLevel(cfg.Log.Level); err != nil {
			return fmt.Errorf("invalid log level '%s', must be one of: %s", cfg.Log.Level, strings.Join(LogLevels, ", "))
		}
	}

	for ifaceName, iface := range cfg.Interfaces {
		// 0 means unset (ephemeral); reject anything outside the port range.
		if iface.Proxy.Listen < 0 || iface.Proxy.Listen > 65535 {
			return fmt.Errorf("invalid proxy listen port %d for interface '%s', must be between 0 and 65535", iface.Proxy.Listen, ifaceName)
		}

		// 0 means unset (escape off); the true kernel limit is net.fibs-1,
		// which is unknowable at config-parse time, so this is only a range
		// check -- a fib the running kernel rejects surfaces as a setsockopt
		// error at runtime instead.
		if iface.Proxy.Fib < 0 || iface.Proxy.Fib > 65535 {
			return fmt.Errorf("invalid proxy fib %d for interface '%s', must be between 0 and 65535", iface.Proxy.Fib, ifaceName)
		}

		// Windows has no non-proxy mode; an explicit opt-out can't be honored.
		if goos == "windows" && iface.Proxy.Enabled != nil && !*iface.Proxy.Enabled {
			return fmt.Errorf("invalid proxy.enabled 'false' for interface '%s': Windows has no non-proxy mode", ifaceName)
		}

		if iface.Protocol != "" {
			switch iface.Protocol {
			case "ipv4", "ipv6", "dualstack":
			default:
				return fmt.Errorf("invalid interface protocol '%s' for interface '%s', must be one of: ipv4, ipv6, dualstack", iface.Protocol, ifaceName)
			}
		}

		for peerName, peer := range iface.Peers {
			if peer.Protocol != "" {
				switch peer.Protocol {
				case "ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6":
				default:
					return fmt.Errorf("invalid peer protocol '%s' for peer '%s' on interface '%s', must be one of: ipv4, ipv6, prefer_ipv4, prefer_ipv6", peer.Protocol, peerName, ifaceName)
				}
			}
		}
	}

	return nil
}
