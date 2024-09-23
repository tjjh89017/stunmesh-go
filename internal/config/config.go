package config

import (
	"errors"
	"time"

	"github.com/spf13/viper"
)

const Name = "config"

var Paths []string = []string{
	"/etc/stunmesh",
	"$HOME/.stunmesh",
	".",
}

var (
	ErrBindEnv         = errors.New("failed to bind env")
	ErrReadConfig      = errors.New("failed to read config")
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
)

var envs = map[string][]string{
	"wg":                   {"WG", "WIREGUARD"},
	"cloudflare.api_key":   {"CF_API_KEY", "CLOUDFLARE_API_KEY"},
	"cloudflare.api_email": {"CF_API_EMAIL", "CLOUDFLARE_API_EMAIL"},
	"cloudflare.api_token": {"CF_API_TOKEN", "CLOUDFLARE_API_TOKEN"},
	"cloudflare.zone_name": {"CF_ZONE_NAME", "CLOUDFLARE_ZONE_NAME"},
}

type Config struct {
	WireGuard  string `mapstructure:"wg"`
	Interfaces map[string]struct {
	} `mapstructure:"interfaces"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	Log             struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`
	Stun struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"stun"`
	Cloudflare struct {
		ApiKey   string `mapstructure:"api_key"`
		ApiEmail string `mapstructure:"api_email"`
		ApiToken string `mapstructure:"api_token"`
		ZoneName string `mapstructure:"zone_name"`
	} `mapstructure:"cloudflare"`
}

func Load() (*Config, error) {
	viper.SetConfigName(Name)
	for _, path := range Paths {
		viper.AddConfigPath(path)
	}
	viper.AutomaticEnv()

	viper.SetDefault("refresh_interval", time.Duration(10)*time.Minute)
	viper.SetDefault("stun.addr", "stun.l.google.com:19302")

	for envName, keys := range envs {
		binding := []string{envName}
		binding = append(binding, keys...)

		if err := viper.BindEnv(binding...); err != nil {
			return nil, errors.Join(ErrBindEnv, err)
		}
	}

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
