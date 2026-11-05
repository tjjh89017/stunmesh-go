package userspace

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
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

// listenPort reads the port the node's device bound.
func listenPort(t *testing.T, n *Node) int {
	t.Helper()
	st, err := n.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.ListenPort == 0 {
		t.Fatal("no listen port in status")
	}
	return st.ListenPort
}

func pubKeyB64(t *testing.T, privB64 string) string {
	t.Helper()
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

// onceTun is a factory that hands out dev on the first call only: a
// channel tun cannot be reopened once the device closes it.
func onceTun(dev tun.Device) func(int) (tun.Device, error) {
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

// validKeyB64 returns a base64 32-byte key with every byte set to fill.
func validKeyB64(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

// tunPeer is one side of a two-node tunnel over loopback.
type tunPeer struct {
	node *Node
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
func TestNodeCarriesTraffic(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	ping(t, a, b)
	ping(t, b, a)
}

// Stop closes the tun; when the factory cannot open another, the second
// Start reports starting then down and leaves the node stopped.
func TestNodeStartFailsWhenTunFactoryFails(t *testing.T) {
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

// peerStatus returns the status of the peer with key pubB64, or fails.
func peerStatus(t *testing.T, n *Node, pubB64 string) PeerStatus {
	t.Helper()
	st, err := n.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, p := range st.Peers {
		if p.PublicKey == pubB64 {
			return p
		}
	}
	t.Fatalf("peer %s missing from status %+v", pubB64, st)
	return PeerStatus{}
}

// Status reports the live peer state: after traffic, the peer has a
// handshake, the endpoint it was given and nonzero byte counters.
func TestNodeStatus(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	ping(t, a, b)

	p := peerStatus(t, a.node, b.pub)
	if p.LastHandshake.IsZero() {
		t.Errorf("no handshake recorded: %+v", p)
	}
	if p.TxBytes == 0 || p.RxBytes == 0 {
		t.Errorf("byte counters not moving: %+v", p)
	}
	if !strings.HasPrefix(p.Endpoint, "127.0.0.1:") {
		t.Errorf("endpoint = %q, want the loopback one", p.Endpoint)
	}

	a.node.Stop()
	if _, err := a.node.Status(); err == nil {
		t.Error("Status on a stopped node succeeded")
	}
}

// SetPeerKeepalive changes the live device: Status reads the new interval
// back, and 0 turns it off again.
func TestNodeSetPeerKeepalive(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	for _, sec := range []int{25, 0} {
		if err := a.node.SetPeerKeepalive(b.pub, sec); err != nil {
			t.Fatalf("SetPeerKeepalive(%d): %v", sec, err)
		}
		if got := peerStatus(t, a.node, b.pub).Keepalive; got != sec {
			t.Errorf("after SetPeerKeepalive(%d): interval = %d", sec, got)
		}
	}

	a.node.Stop()
	if err := a.node.SetPeerKeepalive(b.pub, 25); err == nil {
		t.Error("SetPeerKeepalive on a stopped node succeeded")
	}
}

// The device ignores an update for a key it does not know, so the node
// must refuse one itself instead of reporting success.
func TestNodeUnknownPeer(t *testing.T) {
	a, _ := startTunPair(t, quietListener{})
	stranger := pubKeyB64(t, validKeyB64(9))
	if err := a.node.SetPeerEndpoint(stranger, "127.0.0.1:1"); !errors.Is(err, ErrUnknownPeer) {
		t.Errorf("SetPeerEndpoint(unknown) = %v, want ErrUnknownPeer", err)
	}
	if err := a.node.SetPeerKeepalive(stranger, 25); !errors.Is(err, ErrUnknownPeer) {
		t.Errorf("SetPeerKeepalive(unknown) = %v, want ErrUnknownPeer", err)
	}
	st, err := a.node.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Peers) != 1 {
		t.Errorf("peers = %+v, want only the configured one", st.Peers)
	}
}
