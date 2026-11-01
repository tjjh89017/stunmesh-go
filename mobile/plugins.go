//go:build mobile

package mobile

import (
	// builtin/all is the shipped plugin set. A custom binary imports the
	// builtin/* packages it wants, or its own plugin package, instead.
	_ "github.com/tjjh89017/stunmesh-go/builtin/all"
)
