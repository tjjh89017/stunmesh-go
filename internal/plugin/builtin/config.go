// Package builtin holds helpers shared across built-in plugin
// implementations (internal/plugin/builtin/<name>).
//
// This file carries no build tag: unlike each built-in's own
// implementation, which is compiled in only under its own tag or
// builtin_all, the config helper must be reachable regardless of which
// combination of built-ins is compiled in, so it is always compiled.
package builtin

import (
	"fmt"
	"time"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

// Config wraps a plugin's raw configuration map with typed accessors.
type Config struct {
	values pluginapi.PluginConfig
}

// NewConfig wraps a plugin configuration map for typed access.
func NewConfig(values pluginapi.PluginConfig) *Config {
	return &Config{values: values}
}

// GetString reads a string value, reporting whether it was present and of
// the right type.
func (c *Config) GetString(key string) (string, bool) {
	val, ok := c.values[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetStringRequired reads a string value, returning an error if it is
// absent or not a string.
func (c *Config) GetStringRequired(key string) (string, error) {
	val, ok := c.GetString(key)
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}

// GetStringSlice reads a list that YAML may deliver as []interface{}, or
// that mapstructure's weak typing may have already turned into []string.
func (c *Config) GetStringSlice(key string) ([]string, error) {
	val, ok := c.values[key]
	if !ok {
		return nil, nil
	}

	switch v := val.(type) {
	case []string:
		return v, nil
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a list of strings", key)
			}
			items = append(items, str)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%s must be a list of strings", key)
	}
}

// GetDuration reads a timeout expressed either as a duration string such as
// "20s" or as a plain number of seconds, since a YAML scalar may arrive as
// either depending on how it was written.
func (c *Config) GetDuration(key string) (time.Duration, bool, error) {
	val, ok := c.values[key]
	if !ok {
		return 0, false, nil
	}

	switch v := val.(type) {
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, false, fmt.Errorf("%s is not a valid duration: %w", key, err)
		}
		return d, true, nil
	case int:
		return time.Duration(v) * time.Second, true, nil
	case float64:
		return time.Duration(v * float64(time.Second)), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be a duration or a number of seconds", key)
	}
}
