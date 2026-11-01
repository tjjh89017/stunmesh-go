package pluginapi

import (
	"context"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Key is a WireGuard public key, the same type wgctrl uses.
type Key = wgtypes.Key

// PluginRequest is what a Store receives for each Get or Set.
// A zero public key means unknown.
type PluginRequest struct {
	Device         string // wg interface or tunnel name; "" when unknown
	LocalPublicKey Key
	PeerPublicKey  Key

	Key   string // storage key, SHA1 hex
	Value string // Set only; empty on Get
}

type Store interface {
	Get(ctx context.Context, req PluginRequest) (string, error)
	Set(ctx context.Context, req PluginRequest) error
}
