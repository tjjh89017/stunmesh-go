// Package all links in the built-in plugins the release binary ships.
// Import it for its side effect; a custom binary may import a subset of
// builtin/* instead.
package all

import (
	_ "github.com/tjjh89017/stunmesh-go/builtin/cloudflare"
	_ "github.com/tjjh89017/stunmesh-go/builtin/mirror"
	_ "github.com/tjjh89017/stunmesh-go/builtin/opendht"
	_ "github.com/tjjh89017/stunmesh-go/builtin/passthrough"
)
