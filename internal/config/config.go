package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/wire"
	"github.com/rs/zerolog"
	"go.yaml.in/yaml/v3"
)

var DefaultSet = wire.NewSet(
	Load,
	NewDeviceConfig,
)

// Defaults applied by Load when the config file omits the corresponding keys.
const (
	DefaultRefreshInterval  = 10 * time.Minute
	DefaultStunServer       = "stun.l.google.com:19302"
	DefaultPingInterval     = 1 * time.Second
	DefaultPingTimeout      = 1 * time.Second
	DefaultPingFixedRetries = 3
)

// File, when non-empty, is the exact config file to read; it overrides Dir
// and Paths and must be readable (no fallback to defaults).
var File string

// Dir, when non-empty, is a directory searched for ConfigFileNames; it
// overrides Paths and must contain a config file (no fallback to defaults).
var Dir string

// ConfigFileNames lists candidate file names inside a directory; the first
// one that exists wins.
var ConfigFileNames = []string{"config.yaml", "config.yml"}

// Paths lists directories (env-expanded) searched when neither File nor Dir is set.
var Paths []string = []string{
	"$STUNMESH_CONFIG_DIR",
	"/etc/stunmesh",
	"$HOME/.stunmesh",
	".",
}

var (
	ErrReadConfig      = errors.New("failed to read config")
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrNoStunServers   = errors.New("stun.addresses is explicitly empty and no stun.address is set")
)

type Logger struct {
	Level string `mapstructure:"level"`
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

type PluginConfig map[string]interface{}

type PluginDefinition struct {
	Type   string       `mapstructure:"type"`
	Config PluginConfig `mapstructure:",remain"`
}

type Config struct {
	Interfaces      Interfaces                  `mapstructure:"interfaces"`
	Plugins         map[string]PluginDefinition `mapstructure:"plugins"`
	RefreshInterval time.Duration               `mapstructure:"refresh_interval"`
	Log             Logger                      `mapstructure:"log"`
	Stun            Stun                        `mapstructure:"stun"`
	PingMonitor     PingMonitor                 `mapstructure:"ping_monitor"`
}

// findConfigFile resolves the config file path, honoring File and Dir before
// the Paths search; "" with nil error means not found (proceed with defaults).
func findConfigFile() (string, error) {
	if File != "" {
		return File, nil
	}

	if Dir != "" {
		for _, name := range ConfigFileNames {
			candidate := filepath.Join(Dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		// Explicit Dir override must succeed: return the primary name so the read fails hard.
		return filepath.Join(Dir, ConfigFileNames[0]), nil
	}

	for _, path := range Paths {
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

func Load() (*Config, error) {
	var cfg Config
	// Pre-Decode defaults; yaml keys absent from the file leave these untouched.
	// Stun.Addresses must stay nil (not []) so "key absent" is distinguishable
	// from "explicitly empty list" after decoding.
	cfg.RefreshInterval = DefaultRefreshInterval
	cfg.PingMonitor.Interval = DefaultPingInterval
	cfg.PingMonitor.Timeout = DefaultPingTimeout
	cfg.PingMonitor.FixedRetries = DefaultPingFixedRetries

	path, err := findConfigFile()
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

		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
			Result:     &cfg,
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
	// empty list ("addresses: []") -> hard error; otherwise leave the
	// user-provided list untouched.
	switch {
	case cfg.Stun.Addresses == nil && cfg.Stun.Address == "":
		// logger.NewLogger needs this config; use a throwaway console logger here.
		warnLog := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
		warnLog.Warn().Msg("no STUN servers configured, defaulting to " + DefaultStunServer)
		cfg.Stun.Addresses = []string{DefaultStunServer}
	case len(cfg.Stun.Addresses) == 0 && cfg.Stun.Address == "":
		return nil, ErrNoStunServers
	}

	// Merge deprecated Address into Addresses: prepend Address if non-empty, then deduplicate.
	cfg.Stun.Addresses = cfg.Stun.GetServers()
	cfg.Stun.Address = ""

	// Validate protocol configurations
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateConfig validates protocol configurations and returns error if invalid
func validateConfig(cfg *Config) error {
	for ifaceName, iface := range cfg.Interfaces {
		// Validate interface protocol
		if iface.Protocol != "" {
			switch iface.Protocol {
			case "ipv4", "ipv6", "dualstack":
				// Valid
			default:
				return errors.New("invalid interface protocol '" + iface.Protocol + "' for interface '" + ifaceName + "', must be one of: ipv4, ipv6, dualstack")
			}
		}

		// Validate peer protocols
		for peerName, peer := range iface.Peers {
			if peer.Protocol != "" {
				switch peer.Protocol {
				case "ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6":
					// Valid
				default:
					return errors.New("invalid peer protocol '" + peer.Protocol + "' for peer '" + peerName + "' on interface '" + ifaceName + "', must be one of: ipv4, ipv6, prefer_ipv4, prefer_ipv6")
				}
			}
		}
	}

	return nil
}
