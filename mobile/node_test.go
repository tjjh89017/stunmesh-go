//go:build mobile && (linux || android)

package mobile

import (
	"encoding/base64"
	"os"
	"reflect"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func validKeyB64(fill byte) string {
	var raw [32]byte
	for i := range raw {
		raw[i] = fill
	}
	return base64.StdEncoding.EncodeToString(raw[:])
}

func minimalConfigJSON(privateKey, peerPublicKey string) string {
	return `{
		"name": "wg0",
		"interface": {"private_key": "` + privateKey + `"},
		"peers": [{"name": "peer1", "public_key": "` + peerPublicKey + `"}]
	}`
}

type quietListener struct{}

func (quietListener) OnStateChanged(string)          {}
func (quietListener) OnLog(string, string)           {}
func (quietListener) OnEvent(string, string, string) {}

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

// fakeTunProvider hands out fd on every OpenTun and counts the calls.
type fakeTunProvider struct {
	mu    sync.Mutex
	fd    int32
	calls int
}

func (p *fakeTunProvider) OpenTun(int32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.fd
}

type okProtector struct{}

func (okProtector) Protect(int32) bool { return true }

// recordingListener keeps the state changes.
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

func newTestNode(t *testing.T, tp TunProvider, l EventListener) *Node {
	t.Helper()
	n, err := NewNode(minimalConfigJSON(validKeyB64(1), validKeyB64(2)), tp, okProtector{}, l)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	t.Cleanup(n.Stop)
	return n
}

// A platform with no tun fd fails Start cleanly: starting, then down, and
// the node stays stopped.
func TestStartWithoutTunFd(t *testing.T) {
	rec := &recordingListener{}
	tp := &fakeTunProvider{fd: -1}
	n := newTestNode(t, tp, rec)
	if err := n.Start(); err == nil {
		t.Fatal("Start without a tun fd succeeded")
	}
	if n.IsRunning() {
		t.Error("node running after a failed Start")
	}
	if got, want := rec.take(), []string{StateStarting, StateDown}; !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
	if tp.calls != 1 {
		t.Errorf("OpenTun calls = %d, want 1", tp.calls)
	}
}

// RenewTun on a node that is not running refuses before touching the fd
// (a tun wrapper would first make it nonblocking).
func TestRenewTunNotRunning(t *testing.T) {
	n := newTestNode(t, &fakeTunProvider{fd: -1}, quietListener{})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	fd := int(r.Fd()) // Fd leaves the pipe blocking
	if err := n.RenewTun(int32(fd)); err == nil {
		t.Fatal("RenewTun on a stopped node succeeded")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Error("RenewTun changed the fd")
	}
}

func TestNewNodeRequiresHooks(t *testing.T) {
	js := minimalConfigJSON(validKeyB64(1), validKeyB64(2))
	tp := &fakeTunProvider{fd: -1}
	if _, err := NewNode(js, nil, okProtector{}, quietListener{}); err == nil {
		t.Error("nil tun provider accepted")
	}
	if _, err := NewNode(js, tp, nil, quietListener{}); err == nil {
		t.Error("nil protector accepted")
	}
	if _, err := NewNode(js, tp, okProtector{}, nil); err == nil {
		t.Error("nil listener accepted")
	}
	if _, err := NewNode("{", tp, okProtector{}, quietListener{}); err == nil {
		t.Error("malformed config accepted")
	}
}
