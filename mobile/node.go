//go:build mobile && (linux || android)

package mobile

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/node"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	"golang.zx2c4.com/wireguard/tun"
)

// defaultPluginDNSServers backs plugin hostname lookups until the app calls
// SetDNSServers: public resolvers reachable from most underlay networks.
// Without them, the pure-Go resolver the plugin dialer forces (see
// internal/plugin/dialer) has no /etc/resolv.conf to read on android and
// falls back to localhost, where nothing answers.
var defaultPluginDNSServers = []string{"8.8.8.8:53", "1.1.1.1:53", "[2001:4860:4860::8888]:53"}

// Node is one running STUNMESH instance: an embedded wireguard-go device on
// a demuxing bind, plus the STUNMESH controller for discovery, publish and
// establish.
type Node struct {
	core     *node.Core
	tunP     TunProvider
	listener EventListener

	mu         sync.Mutex
	dnsServers []string

	tunMu sync.Mutex
	tun   *swappableTun
}

// NewNode parses the config and prepares a node. No sockets or devices are
// created until Start.
func NewNode(configJSON string, tunProvider TunProvider, protector SocketProtector, listener EventListener) (*Node, error) {
	if tunProvider == nil || protector == nil || listener == nil {
		return nil, errors.New("tunProvider, protector and listener are required")
	}
	cfg, err := node.ParseConfig([]byte(configJSON))
	if err != nil {
		return nil, err
	}
	n := &Node{tunP: tunProvider, listener: listener}
	n.core, err = node.New(cfg, n.openTun, node.Options{
		Protector:  protector,
		DNSServers: n.pluginDNSServers,
		Listener:   listener,
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// NewNodeWithTun is NewNode for a caller that brings its own tun.Device,
// such as wireguard-go's tun/netstack, so the node runs entirely in
// userspace without a platform VPN. No socket protection applies: with no
// VPN there is no tunnel for the outer sockets to loop through. Start takes
// dev; Stop, or a Start that fails after taking it, closes it, so a node
// built this way starts once. A node that never starts leaves dev to the
// caller. RenewTun does not apply. gomobile skips this constructor; it is
// for Go callers.
func NewNodeWithTun(configJSON string, dev tun.Device, listener EventListener) (*Node, error) {
	if dev == nil || listener == nil {
		return nil, errors.New("dev and listener are required")
	}
	cfg, err := node.ParseConfig([]byte(configJSON))
	if err != nil {
		return nil, err
	}
	var once sync.Mutex
	newTun := func(int) (tun.Device, error) {
		once.Lock()
		defer once.Unlock()
		if dev == nil {
			return nil, errors.New("tun already consumed by an earlier Start")
		}
		d := dev
		dev = nil
		return d, nil
	}
	n := &Node{listener: listener}
	n.core, err = node.New(cfg, newTun, node.Options{
		DNSServers: n.pluginDNSServers,
		Listener:   listener,
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// openTun is the tun factory of a node on a platform VPN: it takes a fresh
// fd from the TunProvider and keeps the wrapper so RenewTun can swap it.
func (n *Node) openTun(mtu int) (tun.Device, error) {
	fd := n.tunP.OpenTun(int32(mtu))
	if fd < 0 {
		return nil, errors.New("tun fd not available")
	}
	raw, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return nil, fmt.Errorf("create tun from fd: %w", err)
	}
	t := newSwappableTun(raw)
	n.tunMu.Lock()
	n.tun = t
	n.tunMu.Unlock()
	return t, nil
}

// Start brings up the data plane: tun from the platform fd or the caller,
// wireguard-go device on the demuxing bind, config over UAPI, device up.
func (n *Node) Start() error { return n.core.Start() }

// Stop tears down the controller, device and sockets. Idempotent.
func (n *Node) Stop() { n.core.Stop() }

// SetDNSServers sets the nameservers plugin hostname lookups use, as a
// comma-separated list of "host" or "host:port" entries (port defaults to
// 53; bare IPv6 is fine). The app should pass the underlay network's own
// resolvers -- LinkProperties.dnsServers of the network the VPN runs over --
// and call again from its network callback whenever that network changes.
// The tunnel's dns_servers are deliberately not used: plugin sockets are
// protected out of the tunnel, so a tunnel-internal resolver would be
// unreachable from them. Entries must be IP literals; anything else is
// dropped (a hostname would itself need resolving). An empty or all-invalid
// string reverts to built-in public resolvers. Callable at any time,
// including before Start.
func (n *Node) SetDNSServers(servers string) {
	var list []string
	for s := range strings.SplitSeq(servers, ",") {
		if s = strings.TrimSpace(s); s != "" && dialer.ValidNameserver(s) {
			list = append(list, s)
		}
	}
	n.mu.Lock()
	n.dnsServers = list
	n.mu.Unlock()
}

// pluginDNSServers is what the node hands the plugin dialer: the
// app-provided list, or the public fallback so lookups work before the app
// provides one. The slice is replaced wholesale by SetDNSServers, never
// mutated, so returning it unlocked is safe.
func (n *Node) pluginDNSServers() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.dnsServers) > 0 {
		return n.dnsServers
	}
	return defaultPluginDNSServers
}

// RenewTun swaps in a fresh tun fd after a platform network change without
// restarting the WG device. A node on a caller-supplied tun has no platform
// fd to renew, and swapping one in would close the caller's tun, so it
// refuses and leaves fd to the caller.
func (n *Node) RenewTun(fd int32) error {
	if n.tunP == nil {
		return errors.New("node has no platform tun")
	}
	n.tunMu.Lock()
	t := n.tun
	n.tunMu.Unlock()
	if t == nil || !n.core.IsRunning() {
		return errors.New("node not running")
	}
	newTun, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return fmt.Errorf("create tun from fd: %w", err)
	}
	if err := t.swap(newTun); err != nil {
		return errors.New("node not running")
	}
	n.listener.OnLog("info", "tun fd renewed")
	return nil
}

// SetPeerEndpoint applies a discovered endpoint to one peer at run time.
// The device does not restart.
func (n *Node) SetPeerEndpoint(publicKeyB64, endpoint string) error {
	return n.core.SetPeerEndpoint(publicKeyB64, endpoint)
}

// SetPeerKeepalive sets one peer's persistent keepalive interval at run
// time, in seconds, 0 to turn it off. Like SetPeerEndpoint, the device does
// not restart.
func (n *Node) SetPeerKeepalive(publicKeyB64 string, seconds int32) error {
	return n.core.SetPeerKeepalive(publicKeyB64, int(seconds))
}

// IpcGet returns the device's UAPI state: endpoints, handshake times, byte
// counts and keys, including the interface's private_key, as `wg showconf`
// prints it. Callers that only want status must not log or display the
// text as is.
func (n *Node) IpcGet() (string, error) { return n.core.IpcGet() }

// IsRunning reports whether the data plane is up.
func (n *Node) IsRunning() bool { return n.core.IsRunning() }
