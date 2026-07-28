//go:build linux || android

package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/mobilebind"
	"github.com/tjjh89017/stunmesh-go/internal/plugin"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
	"golang.org/x/crypto/curve25519"
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
	bind    *mobilebind.Bind
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

	// TODO: route the plugins' HTTPS through a protected dialer so full-tunnel
	// configs (allowed IPs 0.0.0.0/0) do not loop plugin traffic into the
	// tunnel. Split-tunnel configs work as-is.
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
	// each cycle until it succeeds.
	if !c.pluginsReady {
		if err := c.manager.LoadPlugins(ctx, c.pluginDefs); err != nil {
			listener.OnLog("warn", "plugin init: "+err.Error())
			return
		}
		c.pluginsReady = true
	}

	data := c.discover(ctx)
	if data.IPv4 == "" && data.IPv6 == "" {
		listener.OnLog("warn", "no endpoint discovered, skipping publish")
	} else {
		c.publish(ctx, data)
	}
	c.establish(ctx)
}

// discover resolves the reflexive addresses the interface protocol asks for,
// trying each configured STUN server until one answers.
func (c *controller) discover(ctx context.Context) ctrl.EndpointData {
	listener := c.node.listener
	var data ctrl.EndpointData

	proto := c.cfg.Interface.Protocol
	if proto == "ipv4" || proto == "dualstack" {
		if ep, err := c.discoverFamily(ctx, "udp4"); err == nil {
			data.IPv4 = ep
			listener.OnEvent("endpoint_discovered", "", "ipv4 "+ep)
		} else {
			listener.OnLog("warn", "ipv4 discovery: "+err.Error())
		}
	}
	if proto == "ipv6" || proto == "dualstack" {
		if ep, err := c.discoverFamily(ctx, "udp6"); err == nil {
			data.IPv6 = ep
			listener.OnEvent("endpoint_discovered", "", "ipv6 "+ep)
		} else {
			listener.OnLog("warn", "ipv6 discovery: "+err.Error())
		}
	}
	return data
}

func (c *controller) discoverFamily(ctx context.Context, network string) (string, error) {
	var lastErr error
	for _, server := range c.cfg.Stun.Addresses {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		addr, err := c.bind.Discover(attemptCtx, network, server)
		cancel()
		if err == nil {
			return addr.String(), nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("all stun servers failed: %w", lastErr)
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
		if err := store.Set(ctx, localId, res.Data); err != nil {
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
		encrypted, err := store.Get(ctx, peerId.RemoteEndpointKey())
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
		endpoint, err := selectEndpoint(data, peer.Protocol)
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

// selectEndpoint applies the peer protocol preference, matching the desktop
// establish semantics: hard families error when absent, prefer_* fall back.
func selectEndpoint(data ctrl.EndpointData, protocol string) (string, error) {
	switch protocol {
	case "", "ipv4":
		if data.IPv4 == "" {
			return "", fmt.Errorf("no ipv4 endpoint in record")
		}
		return data.IPv4, nil
	case "ipv6":
		if data.IPv6 == "" {
			return "", fmt.Errorf("no ipv6 endpoint in record")
		}
		return data.IPv6, nil
	case "prefer_ipv4":
		if data.IPv4 != "" {
			return data.IPv4, nil
		}
		if data.IPv6 != "" {
			return data.IPv6, nil
		}
		return "", fmt.Errorf("record has no endpoints")
	case "prefer_ipv6":
		if data.IPv6 != "" {
			return data.IPv6, nil
		}
		if data.IPv4 != "" {
			return data.IPv4, nil
		}
		return "", fmt.Errorf("record has no endpoints")
	default:
		return "", fmt.Errorf("unknown peer protocol %q", protocol)
	}
}
