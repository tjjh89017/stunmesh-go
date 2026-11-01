package plugin

import pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"

// keyString leaves an unknown (zero) key empty so the field is omitted.
func keyString(k pluginapi.Key) string {
	if k == (pluginapi.Key{}) {
		return ""
	}
	return k.String()
}
