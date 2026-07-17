//go:build builtin_cloudflare

package plugin

import (
	"github.com/tjjh89017/stunmesh-go/internal/plugin/builtin/cloudflare"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func init() {
	// Register built-in plugins
	// This file is only compiled when build tags are present
	RegisterBuiltin("cloudflare", func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return cloudflare.NewCloudflarePlugin(config)
	})
}
