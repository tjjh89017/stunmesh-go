//go:build builtin_opendht

package plugin

import (
	"github.com/tjjh89017/stunmesh-go/internal/plugin/builtin/opendht"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func init() {
	// Register built-in plugins
	// This file is only compiled when build tags are present
	RegisterBuiltin("opendht", func(config pluginapi.PluginConfig) (pluginapi.Store, error) {
		return opendht.NewOpenDHTPlugin(config)
	})
}
