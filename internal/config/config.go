package config

import (
	"errors"
	"time"

	"github.com/google/wire"
	"github.com/spf13/viper"
)

var DefaultSet = wire.NewSet(
	Load,
	NewDeviceConfig,
)

const Name = "config"

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
	Address string `mapstructure:"address"`
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

func Load() (*Config, error) {
	viper.SetConfigName(Name)
	for _, path := range Paths {
		viper.AddConfigPath(path)
	}
	viper.AutomaticEnv()

	viper.SetDefault("refresh_interval", time.Duration(10)*time.Minute)
	viper.SetDefault("stun.addr", "stun.l.google.com:19302")
	viper.SetDefault("ping_monitor.interval", time.Duration(1)*time.Second)
	viper.SetDefault("ping_monitor.timeout", time.Duration(1)*time.Second)
	viper.SetDefault("ping_monitor.fixed_retries", 3)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, errors.Join(ErrReadConfig, err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, errors.Join(ErrUnmarshalConfig, err)
	}

	return &cfg, nil
}
