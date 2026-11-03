//go:build mobile && (linux || android)

package mobile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun/tuntest"
)

// SetDNSServers takes the list as one gomobile-safe string; entries are
// comma-separated with optional whitespace, and empties are dropped so a
// trailing comma or a plain "" cannot smuggle in an unusable entry.
func TestSetDNSServersParsing(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "192.0.2.1,192.0.2.2", []string{"192.0.2.1", "192.0.2.2"}},
		{"spaces and trailing comma", " 192.0.2.1 , 2001:db8::1 ,", []string{"192.0.2.1", "2001:db8::1"}},
		{"with ports", "192.0.2.1:53,[2001:db8::1]:53", []string{"192.0.2.1:53", "[2001:db8::1]:53"}},
		{"hostnames dropped", "dns.example,192.0.2.1", []string{"192.0.2.1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &Node{}
			n.SetDNSServers(tc.in)
			if got := n.pluginDNSServers(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pluginDNSServers() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Before the app supplies a list -- and again after it clears one -- lookups
// fall back to the public resolvers, so plugins work out of the box.
func TestPluginDNSServersFallback(t *testing.T) {
	n := &Node{}
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("zero-value node: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}

	n.SetDNSServers("192.0.2.1")
	n.SetDNSServers("  ,, ")
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("after clearing: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}

	n.SetDNSServers("dns.example,other.example")
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("all-invalid list: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}
}

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
func listenPort(t *testing.T, n *Node) int {
	t.Helper()
	n.mu.Lock()
	uapi, err := n.dev.IpcGet()
	n.mu.Unlock()
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
		cfg := tunnelConfig{
			Name: fmt.Sprintf("wgtest%d", i),
			Interface: ifaceConfig{
				PrivateKey: privs[i],
				Addresses:  []string{ips[i].String() + "/24"},
			},
			Peers: []peerConfig{{
				PublicKey:  pubKeyB64(t, privs[o]),
				AllowedIPs: []string{ips[o].String() + "/32"},
				Endpoint:   peerEndpoint,
			}},
			Stun: stunConfig{Addresses: []string{"127.0.0.1:9"}},
			Log:  logConfig{Level: "silent"},
		}
		js, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		ct := tuntest.NewChannelTUN()
		n, err := NewNodeWithTun(string(js), ct.TUN(), listener)
		if err != nil {
			t.Fatalf("NewNodeWithTun: %v", err)
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

// A caller-supplied tun carries traffic like a platform one: a packet
// written into one node's tun comes out of the other's, through a real
// handshake on the loopback WireGuard sockets.
func TestNewNodeWithTunCarriesTraffic(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	ping(t, a, b)
	ping(t, b, a)
}

// Stop closes the caller's tun, so a node built on one cannot start again:
// the second Start reports starting then down and leaves the node stopped.
func TestNewNodeWithTunStartsOnce(t *testing.T) {
	rec := &recordingListener{}
	a, _ := startTunPair(t, rec)
	a.node.Stop()
	rec.take()
	if err := a.node.Start(); err == nil {
		t.Fatal("second Start on a caller-supplied tun succeeded")
	}
	if a.node.IsRunning() {
		t.Error("node running after a failed Start")
	}
	if got, want := rec.take(), []string{StateStarting, StateDown}; !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

// RenewTun would close the caller's tun to install a platform fd, so a
// node without a TunProvider refuses it before touching the fd (an
// unguarded call would first make it nonblocking) and keeps running on its
// own tun.
func TestNewNodeWithTunRefusesRenewTun(t *testing.T) {
	a, b := startTunPair(t, quietListener{})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	fd := int(r.Fd()) // Fd leaves the pipe blocking
	if err := a.node.RenewTun(int32(fd)); err == nil {
		t.Fatal("RenewTun on a caller-supplied tun succeeded")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Error("RenewTun changed the caller's fd")
	}
	ping(t, a, b)
}

func TestNewNodeWithTunRequiresTunAndListener(t *testing.T) {
	js := minimalConfigJSON(validKeyB64(1), validKeyB64(2))
	if _, err := NewNodeWithTun(js, nil, quietListener{}); err == nil {
		t.Error("nil tun accepted")
	}
	if _, err := NewNodeWithTun(js, tuntest.NewChannelTUN().TUN(), nil); err == nil {
		t.Error("nil listener accepted")
	}
}
