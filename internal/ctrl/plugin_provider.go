//go:generate mockgen -destination=./mock/mock_plugin_provider.go -package=mock_ctrl . PluginProvider

package ctrl

import pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"

// PluginProvider is the slice of *plugin.Manager that PublishController and
// EstablishController need: looking up a named plugin instance's Store, and
// checking whether that instance has dedup enabled.
type PluginProvider interface {
	GetPlugin(name string) (pluginapi.Store, error)
	IsDedup(name string) bool
}
