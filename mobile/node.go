//go:build mobile && (linux || android)

package mobile

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/mobilebind"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// defaultPluginDNSServers backs plugin hostname lookups until the app calls
// SetDNSServers: public resolvers reachable from most underlay networks.
// Without them, the pure-Go resolver the plugin dialer forces (see
// internal/plugin/dialer) has no /etc/resolv.conf to read on android and
// falls back to localhost, where nothing answers.
var defaultPluginDNSServers = []string{"8.8.8.8:53", "1.1.1.1:53", "[2001:4860:4860::8888]:53"}

// Node is one running STUNMESH instance: an embedded wireguard-go device on
// a demuxing bind, plus (later) the STUNMESH controllers for discovery,
// publish and establish.
type Node struct {
	mu         sync.Mutex
	cfg        *tunnelConfig
	tunP       TunProvider
	protector  SocketProtector
	listener   EventListener
	dnsServers []string

	dev     *device.Device
	bind    *mobilebind.Bind
	tunDev  *swappableTun
	ctrl    *controller
	running bool
}

// NewNode parses the config and prepares a node. No sockets or devices are
// created until Start.
func NewNode(configJSON string, tunProvider TunProvider, protector SocketProtector, listener EventListener) (*Node, error) {
	if tunProvider == nil || protector == nil || listener == nil {
		return nil, errors.New("tunProvider, protector and listener are required")
	}
	cfg, err := parseConfig(configJSON)
	if err != nil {
		return nil, err
	}
	return &Node{
		cfg:       cfg,
		tunP:      tunProvider,
		protector: protector,
		listener:  listener,
	}, nil
}

// Start brings up the data plane: tun fd from the platform, wireguard-go
// device on the demuxing bind, config over UAPI, device up.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running {
		return errors.New("node already running")
	}
	n.listener.OnStateChanged(StateStarting)

	fail := func(err error) error {
		n.listener.OnLog("error", err.Error())
		n.listener.OnStateChanged(StateDown)
		return err
	}

	fd := n.tunP.OpenTun(int32(n.cfg.Interface.MTU))
	if fd < 0 {
		return fail(errors.New("tun fd not available"))
	}
	rawTun, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return fail(fmt.Errorf("create tun from fd: %w", err))
	}
	tunDev := newSwappableTun(rawTun)

	bind := mobilebind.New(protectorAdapter{n.protector})
	logger := device.NewLogger(logLevel(n.cfg.Log.Level), fmt.Sprintf("(%s) ", n.cfg.Name))
	dev := device.NewDevice(tunDev, bind, logger)

	uapi, err := buildUAPI(n.cfg)
	if err != nil {
		dev.Close()
		return fail(err)
	}
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return fail(fmt.Errorf("ipc set: %w", err))
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return fail(fmt.Errorf("device up: %w", err))
	}

	ctrl, err := newController(n, bind)
	if err != nil {
		dev.Close()
		return fail(fmt.Errorf("controller: %w", err))
	}

	n.dev = dev
	n.bind = bind
	n.tunDev = tunDev
	n.ctrl = ctrl
	n.running = true
	n.listener.OnLog("info", "wireguard device up")
	n.listener.OnStateChanged(StateUp)

	ctrl.start()
	return nil
}

// Stop tears down the controller, device and sockets. Idempotent.
func (n *Node) Stop() {
	// The controller must stop outside the node mutex: a running cycle may
	// be blocked in SetPeerEndpoint waiting for it.
	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		return
	}
	ctrl := n.ctrl
	n.ctrl = nil
	n.mu.Unlock()

	n.listener.OnStateChanged(StateStopping)
	if ctrl != nil {
		ctrl.stop()
	}

	n.mu.Lock()
	if n.dev != nil {
		n.dev.Close() // closes the bind and the tun fd
	}
	n.dev = nil
	n.bind = nil
	n.tunDev = nil
	n.running = false
	n.mu.Unlock()
	n.listener.OnStateChanged(StateDown)
}

// SetDNSServers sets the nameservers plugin hostname lookups use, as a
// comma-separated list of "host" or "host:port" entries (port defaults to
// 53; bare IPv6 is fine). The app should pass the underlay network's own
// resolvers -- LinkProperties.dnsServers of the network the VPN runs over --
// and call again from its network callback whenever that network changes.
// The tunnel's dns_servers are deliberately not used: plugin sockets are
// protected out of the tunnel, so a tunnel-internal resolver would be
// unreachable from them. An empty string reverts to built-in public
// resolvers. Callable at any time, including before Start.
func (n *Node) SetDNSServers(servers string) {
	var list []string
	for _, s := range strings.Split(servers, ",") {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	n.mu.Lock()
	n.dnsServers = list
	n.mu.Unlock()
}

// pluginDNSServers is what the controller hands the plugin dialer: the
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
// restarting the WG device.
func (n *Node) RenewTun(fd int32) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return errors.New("node not running")
	}
	newTun, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return fmt.Errorf("create tun from fd: %w", err)
	}
	n.tunDev.swap(newTun)
	n.listener.OnLog("info", "tun fd renewed")
	return nil
}

// SetPeerEndpoint applies a discovered endpoint to one peer at run time.
// The device does not restart.
func (n *Node) SetPeerEndpoint(publicKeyB64, endpoint string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return errors.New("node not running")
	}
	uapi, err := buildPeerEndpointUAPI(publicKeyB64, endpoint)
	if err != nil {
		return err
	}
	if err := n.dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("ipc set endpoint: %w", err)
	}
	n.listener.OnEvent("peer_endpoint_updated", publicKeyB64, endpoint)
	return nil
}

// IsRunning reports whether the data plane is up.
func (n *Node) IsRunning() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// protectorAdapter bridges the gomobile-facing SocketProtector into the
// internal bind package.
type protectorAdapter struct{ p SocketProtector }

func (a protectorAdapter) Protect(fd int32) bool { return a.p.Protect(fd) }

func logLevel(level string) int {
	switch level {
	case "debug", "trace":
		return device.LogLevelVerbose
	case "error":
		return device.LogLevelError
	case "silent", "disabled":
		return device.LogLevelSilent
	default:
		return device.LogLevelError
	}
}
