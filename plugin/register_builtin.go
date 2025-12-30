//go:build builtin_cloudflare

package plugin

import (
	"github.com/tjjh89017/stunmesh-go/plugin/builtin/cloudflare"
)

func init() {
	// Register built-in plugins
	// This file is only compiled when build tags are present
	RegisterBuiltin("cloudflare", func(config PluginConfig) (Store, error) {
		return cloudflare.NewCloudflarePlugin(cloudflare.PluginConfig(config))
	})
}
