package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/wire"
	"go.yaml.in/yaml/v3"
)

var DefaultSet = wire.NewSet(
	Load,
	NewDeviceConfig,
)

const Name = "config"

// File, when non-empty, is the exact config file path to read. It takes
// priority over Dir and the default search paths. Reading it must succeed
// (no fallback to defaults).
var File string

// Dir, when non-empty, points at a directory containing config.yaml. It
// takes priority over the default search paths. Reading it must succeed
// (no fallback to defaults).
var Dir string

// Paths is the ordered list of directories (before $-expansion) searched
// for a config.yaml/config.yml file when neither File nor Dir is set.
var Paths []string = []string{
	"$STUNMESH_CONFIG_DIR",
	"/etc/stunmesh",
	"$HOME/.stunmesh",
	".",
}

var (
	ErrReadConfig      = errors.New("failed to read config")
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
)

type Logger struct {
	Level string `mapstructure:"level"`
}

type Stun struct {
	Address   string   `mapstructure:"address"`
	Addresses []string `mapstructure:"addresses"`
}

// GetServers returns the final merged and deduplicated list of STUN server addresses.
// It prepends the deprecated Address field (if non-empty) to Addresses, deduplicates
// while preserving order, and falls back to the Google STUN server if the result is empty.
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
		return []string{"stun.l.google.com:19302"}
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

// findConfigFile resolves which config file (if any) to read, honoring
// File and Dir overrides before falling back to the default search paths.
// It returns an empty string (with no error) when no config file is found
// via the default search, matching prior viper.ConfigFileNotFoundError
// behavior of proceeding with defaults.
func findConfigFile() (string, error) {
	if File != "" {
		return File, nil
	}

	if Dir != "" {
		return filepath.Join(Dir, "config.yaml"), nil
	}

	for _, path := range Paths {
		expanded := os.ExpandEnv(path)
		if expanded == "" {
			continue
		}
		for _, name := range []string{"config.yaml", "config.yml"} {
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
	// Defaults: filled in before Decode so that yaml keys not present in
	// the file leave the corresponding struct field untouched.
	cfg.RefreshInterval = 10 * time.Minute
	cfg.Stun.Address = "stun.l.google.com:19302"
	cfg.Stun.Addresses = []string{}
	cfg.PingMonitor.Interval = 1 * time.Second
	cfg.PingMonitor.Timeout = 1 * time.Second
	cfg.PingMonitor.FixedRetries = 3

	path, err := findConfigFile()
	if err != nil {
		return nil, err
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			// Explicit File/Dir overrides must fail hard; the default
			// search only reaches here for a path that os.Stat already
			// confirmed exists, so a read failure here is also an error.
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
	// path == "" means no config file was found via the default search;
	// proceed with defaults, matching prior viper.ConfigFileNotFoundError
	// behavior.

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
