//go:build mobile && (linux || android)

package mobile

import (
	"errors"
	"strings"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	"github.com/tjjh89017/stunmesh-go/userspace"
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
	node     *userspace.Node
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
	cfg, err := userspace.ParseConfig([]byte(configJSON))
	if err != nil {
		return nil, err
	}
	n := &Node{tunP: tunProvider, listener: listener}
	n.node, err = userspace.New(cfg, n.openTun, userspace.Options{
		Protector:  protector,
		DNSServers: n.pluginDNSServers,
		Listener:   listener,
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// openTun is the node's tun factory: it takes a fresh fd from the
// TunProvider and keeps the wrapper so RenewTun can swap it.
func (n *Node) openTun(mtu int) (tun.Device, error) {
	raw, err := tunFromFD(n.tunP.OpenTun(int32(mtu)))
	if err != nil {
		return nil, err
	}
	t := newSwappableTun(raw)
	n.tunMu.Lock()
	n.tun = t
	n.tunMu.Unlock()
	return t, nil
}

// Start brings up the data plane: tun from the platform fd, wireguard-go
// device on the demuxing bind, config over UAPI, device up.
func (n *Node) Start() error { return n.node.Start() }

// Stop tears down the controller, device and sockets. Idempotent.
func (n *Node) Stop() { n.node.Stop() }

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
// restarting the WG device. When it returns "node not running" before using
// the fd, the caller still owns the fd; if a concurrent Stop closed the tun
// first, the fd has been closed.
func (n *Node) RenewTun(fd int32) error {
	n.tunMu.Lock()
	t := n.tun
	n.tunMu.Unlock()
	if t == nil || !n.node.IsRunning() {
		return errors.New("node not running")
	}
	newTun, err := tunFromFD(fd)
	if err != nil {
		return err
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
	return n.node.SetPeerEndpoint(publicKeyB64, endpoint)
}

// SetPeerKeepalive sets one peer's persistent keepalive interval at run
// time, in seconds, 0 to turn it off. Like SetPeerEndpoint, the device does
// not restart.
func (n *Node) SetPeerKeepalive(publicKeyB64 string, seconds int32) error {
	return n.node.SetPeerKeepalive(publicKeyB64, int(seconds))
}

// IsRunning reports whether the data plane is up.
func (n *Node) IsRunning() bool { return n.node.IsRunning() }
