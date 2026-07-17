package plugin

// Built-in plugins register themselves from their init function, so they only
// need to be linked in. Each one's implementation is behind its own build tag;
// without that tag the package is empty and nothing is registered, which is
// why these imports need no build tag of their own.
import (
	_ "github.com/tjjh89017/stunmesh-go/internal/plugin/builtin/cloudflare"
	_ "github.com/tjjh89017/stunmesh-go/internal/plugin/builtin/opendht"
)
