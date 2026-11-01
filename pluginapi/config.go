package pluginapi

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-viper/mapstructure/v2"
	"github.com/rs/zerolog"
)

// reservedKeys are read by the core, not by the plugin.
var reservedKeys = []string{"type", "name", "dedup"}

// DecodeConfig decodes a plugin's configuration map into the struct that out
// points to. The struct's `mapstructure` tags name the YAML keys. Duration
// fields accept strings such as "10s". A value of the wrong type is an error.
//
// A key the struct does not declare is logged as a warning through the
// logger in ctx, so a config written for a newer plugin still loads; check
// the log for typos. The core's reserved keys ("type", "name", "dedup") never
// warn, so the struct does not declare them. Required fields are the plugin's
// job: check them for their zero value after decoding.
func DecodeConfig(ctx context.Context, config PluginConfig, out any) error {
	var md mapstructure.Metadata
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		WeaklyTypedInput: false,
		Metadata:         &md,
		Result:           out,
	})
	if err != nil {
		return fmt.Errorf("plugin config: %w", err)
	}
	if err := decoder.Decode(map[string]any(config)); err != nil {
		return fmt.Errorf("plugin config: %w", err)
	}

	unused := slices.DeleteFunc(md.Unused, func(k string) bool {
		return slices.Contains(reservedKeys, k)
	})
	if len(unused) > 0 {
		slices.Sort(unused)
		zerolog.Ctx(ctx).Warn().Strs("keys", unused).Msg("plugin config: unknown keys")
	}
	return nil
}
