//go:build mobile && (linux || android)

package mobile

import (
	"encoding/base64"
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun/tuntest"
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

// RenewTun would close the caller's tun to install a platform fd, so a
// node without a TunProvider refuses it before touching the fd (an
// unguarded call would first make it nonblocking).
func TestNewNodeWithTunRefusesRenewTun(t *testing.T) {
	n, err := NewNodeWithTun(minimalConfigJSON(validKeyB64(1), validKeyB64(2)), tuntest.NewChannelTUN().TUN(), quietListener{})
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	fd := int(r.Fd()) // Fd leaves the pipe blocking
	if err := n.RenewTun(int32(fd)); err == nil {
		t.Fatal("RenewTun on a caller-supplied tun succeeded")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Error("RenewTun changed the caller's fd")
	}
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
