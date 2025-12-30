//go:build builtin_cloudflare

package plugin

import (
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/builtin/cloudflare"
)

func init() {
	// Register built-in plugins
	// This file is only compiled when build tags are present
	RegisterBuiltin("cloudflare", func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return cloudflare.NewCloudflarePlugin(cloudflare.PluginConfig(config))
	})
}
