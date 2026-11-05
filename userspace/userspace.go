// Package userspace runs a STUNMESH node entirely in userspace: an embedded
// wireguard-go device on a tun the caller provides, with the same STUN
// discovery, encrypted endpoint publishing and peer establishment as the
// stunmesh-go daemon. Desktop and mobile nodes interoperate.
//
// The package is experimental. The Config JSON shape comes from the Android
// app (TunnelConfig.toJson) and may still change.
//
// The caller owns the tun: Config.Interface.Addresses and
// Config.Interface.DNSServers are not applied by the node, only carried for
// the caller that sets up the tun.
//
// Like the app package, userspace registers no built-in plugin. Blank-import
// github.com/tjjh89017/stunmesh-go/builtin/all, or the builtin/* packages
// wanted, to make them available to Config.Plugins.
package userspace

import (
	"github.com/tjjh89017/stunmesh-go/internal/node"
	"golang.zx2c4.com/wireguard/tun"
)

// Config types. The JSON field names are in the struct tags.
type (
	Config          = node.Config
	InterfaceConfig = node.InterfaceConfig
	PeerConfig      = node.PeerConfig
	PluginDef       = node.PluginDef
	StunConfig      = node.StunConfig
	LogConfig       = node.LogConfig
)

// Platform hooks and status types.
type (
	// Options carries the optional hooks of a Node: Protector (nil is a
	// no-op), DNSServers (nil uses the system resolver configuration) and
	// Listener (nil drops events).
	Options         = node.Options
	SocketProtector = node.SocketProtector
	EventListener   = node.EventListener
	Status          = node.Status
	PeerStatus      = node.PeerStatus
)

// Node states reported through EventListener.OnStateChanged.
const (
	StateDown     = node.StateDown
	StateStarting = node.StateStarting
	StateUp       = node.StateUp
	StateStopping = node.StateStopping
)

// ErrUnknownPeer is returned by SetPeerEndpoint and SetPeerKeepalive for a
// public key that is not in Config.Peers.
var ErrUnknownPeer = node.ErrUnknownPeer

// ParseConfig decodes a JSON config and checks it as New does.
func ParseConfig(data []byte) (*Config, error) {
	return node.ParseConfig(data)
}

// Node is one STUNMESH instance.
type Node struct {
	core *node.Core
}

// New validates cfg and prepares a node. No sockets or devices are created
// until Start. cfg is copied; later changes to it have no effect.
//
// newTun opens the tun the device runs on, sized to mtu. Start calls it
// once per start and Stop closes what it returned, so a node can be stopped
// and started again when newTun can open a fresh tun each time.
func New(cfg *Config, newTun func(mtu int) (tun.Device, error), opts Options) (*Node, error) {
	core, err := node.New(cfg, newTun, opts)
	if err != nil {
		return nil, err
	}
	return &Node{core: core}, nil
}

// Start opens the tun, brings the device up and starts the STUNMESH cycle.
// It fails when the node is already running.
func (n *Node) Start() error { return n.core.Start() }

// Stop tears down the cycle, device, sockets and tun. Idempotent.
func (n *Node) Stop() { n.core.Stop() }

// IsRunning reports whether the data plane is up.
func (n *Node) IsRunning() bool { return n.core.IsRunning() }

// SetPeerEndpoint sets one peer's endpoint ("host:port") at run time,
// without restarting the device. The STUNMESH cycle overwrites it when the
// peer publishes a different one. A key not in the config is
// ErrUnknownPeer.
func (n *Node) SetPeerEndpoint(publicKeyB64, endpoint string) error {
	return n.core.SetPeerEndpoint(publicKeyB64, endpoint)
}

// SetPeerKeepalive sets one peer's persistent keepalive interval at run
// time, in seconds from 0 (off) to 65535, without restarting the device. A
// key not in the config is ErrUnknownPeer.
func (n *Node) SetPeerKeepalive(publicKeyB64 string, seconds int) error {
	return n.core.SetPeerKeepalive(publicKeyB64, seconds)
}

// Status reports the live device state. It holds no private key.
func (n *Node) Status() (Status, error) { return n.core.Status() }
