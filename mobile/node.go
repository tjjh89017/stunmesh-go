//go:build linux || android

package mobile

import (
	"errors"
	"fmt"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/mobilebind"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// Node is one running STUNMESH instance: an embedded wireguard-go device on
// a demuxing bind, plus (later) the STUNMESH controllers for discovery,
// publish and establish.
type Node struct {
	mu        sync.Mutex
	cfg       *Config
	tunP      TunProvider
	protector SocketProtector
	listener  EventListener

	dev     *device.Device
	bind    *mobilebind.Bind
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
	tunDev, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return fail(fmt.Errorf("create tun from fd: %w", err))
	}

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

	n.dev = dev
	n.bind = bind
	n.running = true
	n.listener.OnLog("info", "wireguard device up")
	n.listener.OnStateChanged(StateUp)

	// TODO: start the STUNMESH controllers (bootstrap, publish, establish,
	// refresh) on top of bind.Registry() and SetPeerEndpoint.
	return nil
}

// Stop tears down the device and sockets. Idempotent.
func (n *Node) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return
	}
	n.listener.OnStateChanged(StateStopping)
	n.dev.Close() // closes the bind and the tun fd
	n.dev = nil
	n.bind = nil
	n.running = false
	n.listener.OnStateChanged(StateDown)
}

// RenewTun swaps in a fresh tun fd after a platform network change without
// restarting the WG device.
func (n *Node) RenewTun(fd int32) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return errors.New("node not running")
	}
	// TODO: wrap the device tun in a swappable adapter so the fd can be
	// replaced in place. Until then the new fd is rejected and the caller
	// should restart the tunnel.
	return errors.New("tun renewal not implemented yet")
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
