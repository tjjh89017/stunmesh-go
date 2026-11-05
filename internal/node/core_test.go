package node

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/tuntest"
)

// quietListener drops everything; the controller calls it from its own
// goroutine, so it must not share unsynchronized state with the test.
type quietListener struct{}

func (quietListener) OnStateChanged(string)          {}
func (quietListener) OnLog(string, string)           {}
func (quietListener) OnEvent(string, string, string) {}

// recordingListener keeps the state changes, for tests that check them.
type recordingListener struct {
	quietListener
	mu     sync.Mutex
	states []string
}

func (r *recordingListener) OnStateChanged(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, state)
}

func (r *recordingListener) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states
	r.states = nil
	return s
}

// listenPort reads the port the node's device bound from its UAPI state.
func listenPort(t *testing.T, n *Core) int {
	t.Helper()
	uapi, err := n.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(uapi, "\n") {
		if v, ok := strings.CutPrefix(line, "listen_port="); ok {
			port, err := strconv.Atoi(v)
			if err != nil {
				t.Fatal(err)
			}
			return port
		}
	}
	t.Fatalf("no listen_port in UAPI state:\n%s", uapi)
	return 0
}

func pubKeyB64(t *testing.T, privB64 string) string {
	t.Helper()
	priv, err := keyToBytes(privB64)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

// onceTun is a factory that hands out dev on the first call only: a
// channel tun cannot be reopened once the device closes it.
func onceTun(dev tun.Device) TunFactory {
	var mu sync.Mutex
	return func(int) (tun.Device, error) {
		mu.Lock()
		defer mu.Unlock()
		if dev == nil {
			return nil, errors.New("tun already used")
		}
		d := dev
		dev = nil
		return d, nil
	}
}

// tunPeer is one side of a two-node tunnel over loopback.
type tunPeer struct {
	node *Core
	tun  *tuntest.ChannelTUN
	ip   netip.Addr
	pub  string
}

// startTunPair starts two nodes on channel tuns, each with the other as
// its only peer. Both listen on a port the kernel picks, so no port is
// reserved in advance and lost to a race: b is configured with a's
// endpoint, and a learns b's through SetPeerEndpoint, as the controller
// applies a discovered one. STUN points at a closed port and no plugins
// are configured, so no traffic leaves the host.
func startTunPair(t *testing.T, listener EventListener) (a, b *tunPeer) {
	t.Helper()
	privs := []string{validKeyB64(1), validKeyB64(2)}
	ips := []netip.Addr{netip.MustParseAddr("10.99.0.1"), netip.MustParseAddr("10.99.0.2")}
	start := func(i int, peerEndpoint string) *tunPeer {
		o := 1 - i
		cfg := &Config{
			Name: fmt.Sprintf("wgtest%d", i),
			Interface: InterfaceConfig{
				PrivateKey: privs[i],
				Addresses:  []string{ips[i].String() + "/24"},
			},
			Peers: []PeerConfig{{
				PublicKey:  pubKeyB64(t, privs[o]),
				AllowedIPs: []string{ips[o].String() + "/32"},
				Endpoint:   peerEndpoint,
			}},
			Stun: StunConfig{Addresses: []string{"127.0.0.1:9"}},
			Log:  LogConfig{Level: "silent"},
		}
		ct := tuntest.NewChannelTUN()
		n, err := New(cfg, onceTun(ct.TUN()), Options{Listener: listener})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := n.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(n.Stop)
		return &tunPeer{node: n, tun: ct, ip: ips[i], pub: pubKeyB64(t, privs[i])}
	}
	a = start(0, "")
	b = start(1, fmt.Sprintf("127.0.0.1:%d", listenPort(t, a.node)))
	if err := a.node.SetPeerEndpoint(b.pub, fmt.Sprintf("127.0.0.1:%d", listenPort(t, b.node))); err != nil {
		t.Fatalf("SetPeerEndpoint: %v", err)
	}
	return a, b
}

// ping sends one ICMP echo from a's tun and waits for it on b's.
func ping(t *testing.T, a, b *tunPeer) {
	t.Helper()
	select {
	case a.tun.Outbound <- tuntest.Ping(b.ip, a.ip):
	case <-time.After(5 * time.Second):
		t.Fatal("tun did not take the packet")
	}
	select {
	case got := <-b.tun.Inbound:
		if want := tuntest.Ping(b.ip, a.ip); !bytes.Equal(got, want) {
			t.Fatalf("packet changed in transit: got %x, want %x", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("packet did not arrive")
	}
}

// A packet written into one node's tun comes out of the other's, through a
// real handshake on the loopback WireGuard sockets.
func TestCoreCarriesTraffic(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	ping(t, a, b)
	ping(t, b, a)
}

// Stop closes the tun; when the factory cannot open another, the second
// Start reports starting then down and leaves the node stopped.
func TestCoreStartFailsWhenTunFactoryFails(t *testing.T) {
	rec := &recordingListener{}
	a, _ := startTunPair(t, rec)
	a.node.Stop()
	rec.take()
	if err := a.node.Start(); err == nil {
		t.Fatal("second Start with a spent tun factory succeeded")
	}
	if a.node.IsRunning() {
		t.Error("node running after a failed Start")
	}
	if got, want := rec.take(), []string{StateStarting, StateDown}; !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

// uapiPeer returns the keys of the peer with key pubB64 in UAPI state,
// or nil. A peer's lines run from its public_key line to the next one.
func uapiPeer(t *testing.T, uapi, pubB64 string) map[string]string {
	t.Helper()
	pubHex, err := keyToHex(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	var found map[string]string
	inPeer := false
	for _, line := range strings.Split(uapi, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if k == "public_key" {
			inPeer = v == pubHex
			if inPeer {
				found = map[string]string{}
			}
			continue
		}
		if inPeer {
			found[k] = v
		}
	}
	return found
}

// IpcGet reports the live peer state: after traffic, the peer has a
// handshake, the endpoint it was given and nonzero byte counters.
func TestCoreIpcGet(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	ping(t, a, b)

	uapi, err := a.node.IpcGet()
	if err != nil {
		t.Fatalf("IpcGet: %v", err)
	}
	// The private key is part of the state, as documented, in the clamped
	// form the device stores.
	priv, _ := keyToBytes(validKeyB64(1))
	priv[0] &= 248
	priv[31] = priv[31]&127 | 64
	if !strings.HasPrefix(uapi, fmt.Sprintf("private_key=%x\n", priv)) {
		t.Errorf("IpcGet does not lead with the private key:\n%s", uapi)
	}
	p := uapiPeer(t, uapi, b.pub)
	if p == nil {
		t.Fatalf("peer missing from IpcGet:\n%s", uapi)
	}
	if p["last_handshake_time_sec"] == "0" || p["last_handshake_time_sec"] == "" {
		t.Errorf("no handshake recorded: %v", p)
	}
	if p["tx_bytes"] == "0" || p["rx_bytes"] == "0" {
		t.Errorf("byte counters not moving: %v", p)
	}
	if !strings.HasPrefix(p["endpoint"], "127.0.0.1:") {
		t.Errorf("endpoint = %q, want the static loopback one", p["endpoint"])
	}

	a.node.Stop()
	if _, err := a.node.IpcGet(); err == nil {
		t.Error("IpcGet on a stopped node succeeded")
	}
}

// SetPeerKeepalive changes the live device: IpcGet reads the new interval
// back, and 0 turns it off again.
func TestCoreSetPeerKeepalive(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	for _, sec := range []int{25, 0} {
		if err := a.node.SetPeerKeepalive(b.pub, sec); err != nil {
			t.Fatalf("SetPeerKeepalive(%d): %v", sec, err)
		}
		uapi, err := a.node.IpcGet()
		if err != nil {
			t.Fatal(err)
		}
		if got := uapiPeer(t, uapi, b.pub)["persistent_keepalive_interval"]; got != fmt.Sprint(sec) {
			t.Errorf("after SetPeerKeepalive(%d): interval = %q", sec, got)
		}
	}

	a.node.Stop()
	if err := a.node.SetPeerKeepalive(b.pub, 25); err == nil {
		t.Error("SetPeerKeepalive on a stopped node succeeded")
	}
}
