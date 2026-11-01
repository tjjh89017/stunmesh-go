package pluginapi

import (
	"context"
	"encoding/base64"
)

// Key is a WireGuard public key.
type Key [32]byte

// String returns the key in WireGuard's standard base64 form.
func (k Key) String() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// PluginRequest is what a Store receives for each Get or Set.
// A zero public key means unknown.
type PluginRequest struct {
	Device         string // wg interface or tunnel name; "" when unknown
	LocalPublicKey Key    // this host's interface public key
	PeerPublicKey  Key    // the remote peer's public key

	Key   string // storage key, SHA1 hex: the local id on Set, the remote id on Get
	Value string // Set only; empty on Get
}

// Store keeps encrypted endpoint values for the stunmesh core.
//
// Get returns ("", nil) or an error when nothing is stored under the key.
// The ctx passed to Get and Set carries a zerolog logger and the platform
// dialer; pass it to every outbound call.
type Store interface {
	Get(ctx context.Context, req PluginRequest) (string, error)
	Set(ctx context.Context, req PluginRequest) error
}
