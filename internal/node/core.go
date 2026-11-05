package node

import (
	"errors"
	"fmt"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/udpbind"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// TunFactory opens the tun the device runs on, sized to mtu. Start calls it
// once per start; the device closes what it returns on Stop.
type TunFactory func(mtu int) (tun.Device, error)

// Options carries the optional platform hooks of a Core.
type Options struct {
	// Protector excludes the node's outer sockets from the tunnel. Nil is a
	// no-op.
	Protector SocketProtector
	// DNSServers returns the nameservers plugin and STUN lookups use, as
	// "host" or "host:port" IP literals. Nil, or an empty result, uses the
	// system resolver configuration.
	DNSServers func() []string
	// Listener receives state, log and STUNMESH events. Nil drops them.
	Listener EventListener
}

// Core is one STUNMESH node: a wireguard-go device on a demuxing bind and
// the controller that keeps its peer endpoints current.
type Core struct {
	mu         sync.Mutex
	cfg        *Config
	newTun     TunFactory
	protector  SocketProtector
	dnsServers func() []string
	listener   EventListener

	dev     *device.Device
	ctrl    *controller
	running bool
}

// New validates cfg and prepares a node. No sockets or devices are created
// until Start. cfg is copied; later changes to it have no effect.
func New(cfg *Config, newTun TunFactory, opts Options) (*Core, error) {
	if newTun == nil {
		return nil, errors.New("tun factory is required")
	}
	c, err := cfg.normalize()
	if err != nil {
		return nil, err
	}
	listener := opts.Listener
	if listener == nil {
		listener = noListener{}
	}
	return &Core{
		cfg:        c,
		newTun:     newTun,
		protector:  opts.Protector,
		dnsServers: opts.DNSServers,
		listener:   listener,
	}, nil
}

// Start brings up the data plane: tun from the factory, wireguard-go device
// on the demuxing bind, config over UAPI, device up, then the controller.
func (n *Core) Start() error {
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

	tunDev, err := n.newTun(n.cfg.Interface.MTU)
	if err != nil {
		return fail(fmt.Errorf("open tun: %w", err))
	}
	if tunDev == nil {
		return fail(errors.New("open tun: factory returned no device"))
	}

	bind := udpbind.New(n.protector)
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
	n.ctrl = ctrl
	n.running = true
	n.listener.OnLog("info", "wireguard device up")
	n.listener.OnStateChanged(StateUp)

	ctrl.start()
	return nil
}

// Stop tears down the controller, device, sockets and tun. Idempotent.
func (n *Core) Stop() {
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
		n.dev.Close() // closes the bind and the tun
	}
	n.dev = nil
	n.running = false
	n.mu.Unlock()
	n.listener.OnStateChanged(StateDown)
}

// IsRunning reports whether the data plane is up.
func (n *Core) IsRunning() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// SetPeerEndpoint applies an endpoint to one peer at run time. The device
// does not restart.
func (n *Core) SetPeerEndpoint(publicKeyB64, endpoint string) error {
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

// SetPeerKeepalive sets one peer's persistent keepalive interval at run
// time, in seconds, 0 to turn it off. The device does not restart.
func (n *Core) SetPeerKeepalive(publicKeyB64 string, seconds int) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return errors.New("node not running")
	}
	uapi, err := buildPeerKeepaliveUAPI(publicKeyB64, seconds)
	if err != nil {
		return err
	}
	if err := n.dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("ipc set keepalive: %w", err)
	}
	return nil
}

// IpcGet returns the device's UAPI state, private_key included.
func (n *Core) IpcGet() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return "", errors.New("node not running")
	}
	return n.dev.IpcGet()
}

// pluginDNSServers is the nameserver list handed to the plugin dialer.
func (n *Core) pluginDNSServers() []string {
	if n.dnsServers == nil {
		return nil
	}
	return n.dnsServers()
}

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
