//go:build mobile && (linux || android)

package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/mobilebind"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
	"golang.org/x/crypto/curve25519"
)

// stunDiscoverer resolves a reflexive address for one address family via
// STUN. *mobilebind.Bind implements it in production over the shared WG
// socket; tests substitute a fake to exercise discover/discoverFamily
// without a real network.
type stunDiscoverer interface {
	Discover(ctx context.Context, dst netip.AddrPort) (netip.AddrPort, error)
}

// How long one STUN server gets: first to resolve its name, then to answer
// the binding request. Separate budgets so a slow lookup cannot eat the
// probe's retransmit schedule (RFC 8489 RTO doubling, ~7.5s worst case).
const (
	stunResolveTimeout = 5 * time.Second
	stunProbeTimeout   = 10 * time.Second
)

// controller runs the STUNMESH publish/establish cycle on top of the running
// device: discover the reflexive address through the shared socket, encrypt
// and store it via each peer's plugin, fetch and decrypt the peers' records,
// and apply their endpoints over UAPI.
//
// This is a compact mobile counterpart of the desktop controllers
// (internal/ctrl); it reuses the same crypto, storage-key derivation, plugin
// manager and endpoint JSON, so mobile and desktop nodes interoperate.
type controller struct {
	node    *Node
	cfg     *tunnelConfig
	bind    stunDiscoverer
	manager *plugin.Manager
	crypt   *crypto.Endpoint

	priv [32]byte
	pub  [32]byte

	pluginDefs   map[string]pluginapi.PluginDefinition
	pluginsReady bool

	lastPublished map[string]string // peer LocalId -> plaintext JSON last stored
	lastApplied   map[string]string // peer public key -> endpoint last set

	cancel context.CancelFunc
	done   chan struct{}
}

func newController(node *Node, bind *mobilebind.Bind) (*controller, error) {
	cfg := node.cfg
	priv, err := keyToBytes(cfg.Interface.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("private key: %w", err)
	}
	pubSlice, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	var pub [32]byte
	copy(pub[:], pubSlice)

	defs := make(map[string]pluginapi.PluginDefinition, len(cfg.Plugins))
	for _, d := range cfg.Plugins {
		conf := pluginapi.PluginConfig{"name": d.Name}
		for k, v := range d.Config {
			conf[k] = v
		}
		defs[d.Instance] = pluginapi.PluginDefinition{Type: d.Type, Config: conf}
	}

	return &controller{
		node:          node,
		cfg:           cfg,
		bind:          bind,
		manager:       plugin.NewManager(),
		crypt:         crypto.NewEndpoint(),
		priv:          priv,
		pub:           pub,
		pluginDefs:    defs,
		lastPublished: make(map[string]string),
		lastApplied:   make(map[string]string),
		done:          make(chan struct{}),
	}, nil
}

func (c *controller) start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(ctx)
}

// stop cancels the loop and waits for the current cycle to finish. Must not
// be called with the node mutex held: a running cycle may be blocked on it.
func (c *controller) stop() {
	c.cancel()
	<-c.done
}

func (c *controller) run(ctx context.Context) {
	defer close(c.done)
	interval := time.Duration(c.cfg.RefreshIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cycle(ctx)
		}
	}
}

func (c *controller) cycle(ctx context.Context) {
	listener := c.node.listener

	// Plugin init can need the network (e.g. a zone lookup), so it retries
	// each cycle until it succeeds. This only protects the LoadPlugins call
	// boundary -- factory ctors aren't ctx-threaded, so Store.Get/Set (via
	// publish/establish's storeCtx) remain the real protected network path.
	if !c.pluginsReady {
		loadCtx := protectedContext(ctx, c.node.protector, c.node.pluginDNSServers())
		if err := c.manager.LoadPlugins(loadCtx, c.pluginDefs); err != nil {
			listener.OnLog("warn", "plugin init: "+err.Error())
			return
		}
		c.pluginsReady = true
	}

	data, err := c.discover(ctx)
	if err != nil {
		listener.OnLog("warn", "endpoint discovery failed, skipping publish: "+err.Error())
	} else {
		c.publish(ctx, data)
	}
	c.establish(ctx)
}

// discover resolves the reflexive addresses the interface protocol asks for,
// trying each configured STUN server until one answers. The resolution and
// dualstack partial-failure policy live in the shared ctrl.DiscoverEndpoints
// (internal/ctrl/discover.go), which also backs the desktop publish
// controller: a single-family protocol errors out on that family's failure,
// while dualstack tolerates one family failing as long as the other
// succeeds, warning about the failed one instead of erroring.
func (c *controller) discover(ctx context.Context) (ctrl.EndpointData, error) {
	listener := c.node.listener

	resolve := func(network, family string) ctrl.FamilyResolver {
		return func(ctx context.Context) (string, error) {
			ep, err := c.discoverFamily(ctx, network)
			if err != nil {
				return "", err
			}
			listener.OnEvent("endpoint_discovered", "", family+" "+ep)
			return ep, nil
		}
	}
	warn := func(family string, err error) {
		listener.OnLog("warn", family+" discovery: "+err.Error())
	}

	var data ctrl.EndpointData
	var err error
	data.IPv4, data.IPv6, err = ctrl.DiscoverEndpoints(ctx, c.cfg.Interface.Protocol, warn, resolve("udp4", "ipv4"), resolve("udp6", "ipv6"))
	return data, err
}

func (c *controller) discoverFamily(ctx context.Context, network string) (string, error) {
	var lastErr error
	for _, server := range c.cfg.Stun.Addresses {
		addr, err := c.probeServer(ctx, network, server)
		if err != nil {
			lastErr = err
			continue
		}
		return addr.String(), nil
	}
	return "", fmt.Errorf("all stun servers failed: %w", lastErr)
}

// probeServer resolves one configured STUN server for the family and sends it
// a binding request from the shared socket. A server configured for the other
// family resolves to nothing here and is reported as a failure, which is how
// discoverFamily walks a mixed-family list down to the entries that apply.
func (c *controller) probeServer(ctx context.Context, network, server string) (netip.AddrPort, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, stunResolveTimeout)
	dst, err := c.resolveSTUN(resolveCtx, network, server)
	cancel()
	if err != nil {
		return netip.AddrPort{}, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, stunProbeTimeout)
	defer cancel()
	return c.bind.Discover(probeCtx, dst)
}

// resolveSTUN turns a configured STUN server ("host:port", host a name or an
// IP literal) into the address to probe for network ("udp4" or "udp6").
//
// The lookup takes the plugin dialer's escaped path -- a protected socket
// aimed at the underlay's resolvers (Node.SetDNSServers) -- rather than the
// platform's. On android the platform resolver goes through Bionic's
// getaddrinfo, which resolves over the default network; once the tunnel is up
// that is the tunnel, so discovery's own lookup gets routed into the very
// tunnel it is trying to establish. That fails until something else happens
// to bring the tunnel up, which is exactly the ordering that left a
// dualstack node with no IPv6 endpoint on its first cycle: IPv4 discovery ran
// while the tunnel was still down and answered, IPv6 discovery ran after and
// could not resolve.
//
// An IP literal short-circuits inside net.Resolver, so a config that lists
// addresses instead of names needs no working DNS at all.
func (c *controller) resolveSTUN(ctx context.Context, network, server string) (netip.AddrPort, error) {
	host, portStr, err := net.SplitHostPort(server)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("stun server %q: %w", server, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("stun server %q: port: %w", server, err)
	}

	escaped := protectedContext(ctx, c.node.protector, c.node.pluginDNSServers())
	ips, err := dialer.Resolver().LookupNetIP(escaped, ipNetwork(network), host)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("resolve %s: %w", server, err)
	}
	// LookupNetIP reports IPv4 results in 4-in-6 form; unmapping keeps the
	// endpoint string and the bind's socket choice in the right family.
	return netip.AddrPortFrom(ips[0].Unmap(), uint16(port)), nil
}

// ipNetwork maps the UDP network discovery works in to the resolver's, so a
// lookup only returns addresses of the family being discovered.
func ipNetwork(network string) string {
	if network == "udp6" {
		return "ip6"
	}
	return "ip4"
}

func (c *controller) publish(ctx context.Context, data ctrl.EndpointData) {
	listener := c.node.listener
	jsonPlain, err := json.Marshal(data)
	if err != nil {
		listener.OnLog("error", "marshal endpoint data: "+err.Error())
		return
	}

	for _, peer := range c.cfg.Peers {
		peerPub, err := keyToBytes(peer.PublicKey)
		if err != nil {
			continue
		}
		peerId := entity.NewPeerId(c.pub[:], peerPub[:])
		localId := peerId.EndpointKey()

		if c.manager.IsDedup(peer.Plugin) && c.lastPublished[localId] == string(jsonPlain) {
			continue
		}
		store, err := c.manager.GetPlugin(peer.Plugin)
		if err != nil {
			listener.OnLog("warn", "peer "+peer.Name+": "+err.Error())
			continue
		}
		res, err := c.crypt.Encrypt(ctx, &ctrl.EndpointEncryptRequest{
			PeerPublicKey: peerPub,
			PrivateKey:    c.priv,
			Content:       string(jsonPlain),
		})
		if err != nil {
			listener.OnLog("error", "encrypt for "+peer.Name+": "+err.Error())
			continue
		}
		storeCtx := protectedContext(ctx, c.node.protector, c.node.pluginDNSServers())
		if err := store.Set(storeCtx, localId, res.Data); err != nil {
			listener.OnLog("warn", "publish for "+peer.Name+": "+err.Error())
			continue
		}
		c.lastPublished[localId] = string(jsonPlain)
		listener.OnEvent("publish_ok", peer.PublicKey, localId)
	}
}

func (c *controller) establish(ctx context.Context) {
	listener := c.node.listener
	for _, peer := range c.cfg.Peers {
		peerPub, err := keyToBytes(peer.PublicKey)
		if err != nil {
			continue
		}
		peerId := entity.NewPeerId(c.pub[:], peerPub[:])

		store, err := c.manager.GetPlugin(peer.Plugin)
		if err != nil {
			continue
		}
		storeCtx := protectedContext(ctx, c.node.protector, c.node.pluginDNSServers())
		encrypted, err := store.Get(storeCtx, peerId.RemoteEndpointKey())
		if err != nil {
			listener.OnLog("debug", "no record for "+peer.Name+": "+err.Error())
			continue
		}
		res, err := c.crypt.Decrypt(ctx, &ctrl.EndpointDecryptRequest{
			PeerPublicKey: peerPub,
			PrivateKey:    c.priv,
			Data:          encrypted,
		})
		if err != nil {
			listener.OnLog("warn", "decrypt for "+peer.Name+": "+err.Error())
			continue
		}
		var data ctrl.EndpointData
		if err := json.Unmarshal([]byte(res.Content), &data); err != nil {
			listener.OnLog("warn", "parse record for "+peer.Name+": "+err.Error())
			continue
		}
		endpoint, err := ctrl.SelectEndpoint(data, peer.Protocol)
		if err != nil {
			listener.OnLog("warn", "select endpoint for "+peer.Name+": "+err.Error())
			continue
		}
		if c.lastApplied[peer.PublicKey] == endpoint {
			continue
		}
		if err := c.node.SetPeerEndpoint(peer.PublicKey, endpoint); err != nil {
			listener.OnLog("error", "set endpoint for "+peer.Name+": "+err.Error())
			continue
		}
		c.lastApplied[peer.PublicKey] = endpoint
	}
}
