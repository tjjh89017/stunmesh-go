//go:build mobile && (linux || android)

package mobile

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

// TestSelectEndpoint mirrors the four-protocol matrix covered by
// internal/ctrl/establish_test.go's Execute_*Selection tests, since the
// mobile controller calls the same shared ctrl.SelectEndpoint the desktop
// EstablishController uses (see internal/ctrl/endpoint_select.go).
func TestSelectEndpoint(t *testing.T) {
	both := ctrl.EndpointData{IPv4: "1.2.3.4:51820", IPv6: "[2001:db8::1]:51820"}
	ipv4Only := ctrl.EndpointData{IPv4: "1.2.3.4:51820"}
	ipv6Only := ctrl.EndpointData{IPv6: "[2001:db8::1]:51820"}
	empty := ctrl.EndpointData{}

	tests := []struct {
		name     string
		data     ctrl.EndpointData
		protocol string
		want     string
		wantErr  bool
	}{
		{"ipv4 selects ipv4 when both present", both, "ipv4", "1.2.3.4:51820", false},
		{"ipv4 errors when ipv4 absent", ipv6Only, "ipv4", "", true},
		{"empty protocol defaults to ipv4", both, "", "1.2.3.4:51820", false},

		{"ipv6 selects ipv6 when both present", both, "ipv6", "[2001:db8::1]:51820", false},
		{"ipv6 errors when ipv6 absent", ipv4Only, "ipv6", "", true},

		{"prefer_ipv4 selects ipv4 when both present", both, "prefer_ipv4", "1.2.3.4:51820", false},
		{"prefer_ipv4 falls back to ipv6", ipv6Only, "prefer_ipv4", "[2001:db8::1]:51820", false},
		{"prefer_ipv4 errors when both absent", empty, "prefer_ipv4", "", true},

		{"prefer_ipv6 selects ipv6 when both present", both, "prefer_ipv6", "[2001:db8::1]:51820", false},
		{"prefer_ipv6 falls back to ipv4", ipv4Only, "prefer_ipv6", "1.2.3.4:51820", false},
		{"prefer_ipv6 errors when both absent", empty, "prefer_ipv6", "", true},

		{"unknown protocol errors", both, "carrier-pigeon", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ctrl.SelectEndpoint(tt.data, tt.protocol)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SelectEndpoint(%+v, %q) = %q, nil; want error", tt.data, tt.protocol, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectEndpoint(%+v, %q) returned unexpected error: %v", tt.data, tt.protocol, err)
			}
			if got != tt.want {
				t.Errorf("SelectEndpoint(%+v, %q) = %q, want %q", tt.data, tt.protocol, got, tt.want)
			}
		})
	}
}

// fakeDiscoverer is a stunDiscoverer test double keyed by address family
// ("udp4"/"udp6"), letting tests control per-family success/failure without
// a real mobilebind.Bind or network access. The family now comes from the
// destination the controller resolved, which probed also records so tests can
// assert what was actually aimed at.
type fakeDiscoverer struct {
	results map[string]netip.AddrPort
	errs    map[string]error
	probed  map[string]netip.AddrPort
}

func (f *fakeDiscoverer) Discover(_ context.Context, dst netip.AddrPort) (netip.AddrPort, error) {
	network := "udp6"
	if dst.Addr().Is4() {
		network = "udp4"
	}
	if f.probed == nil {
		f.probed = make(map[string]netip.AddrPort)
	}
	f.probed[network] = dst
	if err, ok := f.errs[network]; ok {
		return netip.AddrPort{}, err
	}
	return f.results[network], nil
}

// fakeListener is a no-op EventListener that records OnLog/OnEvent calls so
// tests can assert warnings and discovery events.
type fakeListener struct {
	logs   []string
	events []string
}

func (f *fakeListener) OnStateChanged(string) {}
func (f *fakeListener) OnLog(level, message string) {
	f.logs = append(f.logs, level+": "+message)
}
func (f *fakeListener) OnEvent(kind, _, detail string) {
	f.events = append(f.events, kind+": "+detail)
}

func (f *fakeListener) hasLogContaining(substr string) bool {
	for _, l := range f.logs {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// newDiscoverTestController builds a controller wired the same way
// newController/node.go wire the real one, except bind is a fakeDiscoverer
// instead of a real *mobilebind.Bind, so discover/discoverFamily can be
// exercised through the real controller.discover code path (which invokes
// the shared ctrl.DiscoverEndpoints) without opening a socket.
// The STUN servers are IP literals of both families: resolveSTUN short-
// circuits a literal inside net.Resolver, so discovery stays hermetic, and
// listing both families exercises discoverFamily walking past the entries
// that do not apply to the family it is resolving for.
func newDiscoverTestController(protocol string, disc *fakeDiscoverer, lst *fakeListener) *controller {
	return &controller{
		node: &Node{listener: lst},
		cfg: &tunnelConfig{
			Interface: ifaceConfig{Protocol: protocol},
			Stun: stunConfig{Addresses: []string{
				"192.0.2.1:19302",
				"[2001:db8:5747::1]:19302",
			}},
		},
		bind: disc,
	}
}

// TestControllerDiscover_DualstackBothSucceed proves controller.discover
// wires the shared ctrl.DiscoverEndpoints to the real discoverFamily/bind
// call path for the mobile happy-path dualstack case: both families
// resolve, and both come back in the result with no warnings logged.
func TestControllerDiscover_DualstackBothSucceed(t *testing.T) {
	disc := &fakeDiscoverer{
		results: map[string]netip.AddrPort{
			"udp4": netip.MustParseAddrPort("1.2.3.4:51820"),
			"udp6": netip.MustParseAddrPort("[2001:db8::1]:51820"),
		},
	}
	lst := &fakeListener{}
	c := newDiscoverTestController("dualstack", disc, lst)

	data, err := c.discover(context.Background())
	if err != nil {
		t.Fatalf("discover() returned unexpected error: %v", err)
	}
	if data.IPv4 != "1.2.3.4:51820" {
		t.Errorf("IPv4 = %q, want %q", data.IPv4, "1.2.3.4:51820")
	}
	if data.IPv6 != "[2001:db8::1]:51820" {
		t.Errorf("IPv6 = %q, want %q", data.IPv6, "[2001:db8::1]:51820")
	}
	if lst.hasLogContaining("discovery:") {
		t.Errorf("unexpected warning logged when both families succeeded: %v", lst.logs)
	}
}

// TestControllerDiscover_DualstackPartialFail proves the reconciled "soft"
// dualstack policy (the deliberate mobile behavior change) is actually
// wired through controller.discover/discoverFamily: with IPv4 resolving and
// IPv6 failing, discover must still return success with only the IPv4
// endpoint populated and a warning logged for IPv6 -- not a hard error, and
// not silent about the failure. This is the branch a revert of mobile's
// policy change would break.
func TestControllerDiscover_DualstackPartialFail(t *testing.T) {
	disc := &fakeDiscoverer{
		results: map[string]netip.AddrPort{
			"udp4": netip.MustParseAddrPort("1.2.3.4:51820"),
		},
		errs: map[string]error{
			"udp6": errors.New("stun: no response"),
		},
	}
	lst := &fakeListener{}
	c := newDiscoverTestController("dualstack", disc, lst)

	data, err := c.discover(context.Background())
	if err != nil {
		t.Fatalf("discover() returned unexpected error for a partial family failure: %v", err)
	}
	if data.IPv4 != "1.2.3.4:51820" {
		t.Errorf("IPv4 = %q, want %q", data.IPv4, "1.2.3.4:51820")
	}
	if data.IPv6 != "" {
		t.Errorf("IPv6 = %q, want empty since IPv6 resolution failed", data.IPv6)
	}
	if !lst.hasLogContaining("ipv6 discovery:") {
		t.Errorf("expected a warning about the failed ipv6 discovery, got logs: %v", lst.logs)
	}
}

// TestControllerDiscover_SingleFamilyHardFails proves single-family
// protocols keep the strict (hard-fail) policy through the real call path:
// unlike dualstack, a single configured family failing must surface as an
// error from discover, matching desktop's ipv4/ipv6-only behavior.
func TestControllerDiscover_SingleFamilyHardFails(t *testing.T) {
	disc := &fakeDiscoverer{
		errs: map[string]error{
			"udp4": errors.New("stun: no response"),
		},
	}
	lst := &fakeListener{}
	c := newDiscoverTestController("ipv4", disc, lst)

	_, err := c.discover(context.Background())
	if err == nil {
		t.Fatal("discover() succeeded despite the only requested family failing; want error")
	}
}

// TestControllerDiscoverFamily_PicksServerOfRequestedFamily proves the STUN
// server a family is probed at is one of that family: a mixed-family list is
// walked past the entries that cannot serve the family being discovered,
// rather than the first entry being probed regardless.
func TestControllerDiscoverFamily_PicksServerOfRequestedFamily(t *testing.T) {
	disc := &fakeDiscoverer{
		results: map[string]netip.AddrPort{
			"udp4": netip.MustParseAddrPort("1.2.3.4:51820"),
			"udp6": netip.MustParseAddrPort("[2001:db8::1]:51820"),
		},
	}
	c := newDiscoverTestController("dualstack", disc, &fakeListener{})

	if _, err := c.discoverFamily(context.Background(), "udp6"); err != nil {
		t.Fatalf("discoverFamily(udp6) returned unexpected error: %v", err)
	}
	want := netip.MustParseAddrPort("[2001:db8:5747::1]:19302")
	if got := disc.probed["udp6"]; got != want {
		t.Errorf("probed %v for udp6, want the v6 server %v", got, want)
	}
}

// TestControllerResolveSTUN_UnmapsIPv4 pins the 4-in-6 detail: LookupNetIP
// reports IPv4 as ::ffff:a.b.c.d, and carrying that through would publish an
// IPv4 endpoint formatted as "[::ffff:1.2.3.4]:19302".
func TestControllerResolveSTUN_UnmapsIPv4(t *testing.T) {
	c := newDiscoverTestController("ipv4", &fakeDiscoverer{}, &fakeListener{})

	got, err := c.resolveSTUN(context.Background(), "udp4", "192.0.2.1:19302")
	if err != nil {
		t.Fatalf("resolveSTUN returned unexpected error: %v", err)
	}
	if !got.Addr().Is4() {
		t.Errorf("resolveSTUN = %v (%q), want an unmapped IPv4 address", got, got.String())
	}
	if want := netip.MustParseAddrPort("192.0.2.1:19302"); got != want {
		t.Errorf("resolveSTUN = %v, want %v", got, want)
	}
}

// TestControllerResolveSTUN_RejectsMalformedServer covers the config-error
// path: a server string without a port never reaches the resolver.
func TestControllerResolveSTUN_RejectsMalformedServer(t *testing.T) {
	c := newDiscoverTestController("ipv4", &fakeDiscoverer{}, &fakeListener{})

	if _, err := c.resolveSTUN(context.Background(), "udp4", "192.0.2.1"); err == nil {
		t.Fatal("resolveSTUN accepted a server string with no port; want error")
	}
}

// TestControllerResolveSTUN_UsesEscapedResolver is the regression test for
// the bug this path exists to avoid. The name resolved here exists in no real
// zone, and the only nameserver that can answer it is the one this test puts
// in Node.dnsServers -- which is reachable only if the lookup goes through
// dialer.Resolver()'s Dial hook (the escaped, protected path) rather than the
// platform resolver. A regression to net.Resolve*/net.DefaultResolver fails
// here with a lookup error instead of the fake server's answer.
func TestControllerResolveSTUN_UsesEscapedResolver(t *testing.T) {
	dns := startFakeDNS(t, netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("2001:db8::7"))
	c := newDiscoverTestController("dualstack", &fakeDiscoverer{}, &fakeListener{})
	c.node.dnsServers = []string{dns.addr()}

	for _, tt := range []struct {
		network string
		want    string
	}{
		{"udp4", "203.0.113.7:19302"},
		{"udp6", "[2001:db8::7]:19302"},
	} {
		got, err := c.resolveSTUN(context.Background(), tt.network, "stun.invalid.example:19302")
		if err != nil {
			t.Fatalf("resolveSTUN(%s) returned unexpected error: %v", tt.network, err)
		}
		if want := netip.MustParseAddrPort(tt.want); got != want {
			t.Errorf("resolveSTUN(%s) = %v, want %v", tt.network, got, want)
		}
	}
}

// fakeDNS is a minimal UDP nameserver that answers every A/AAAA question with
// one fixed address, whatever the name. Tests point Node.dnsServers at it to
// prove a lookup travelled the escaped path: nothing else routes a query here.
type fakeDNS struct {
	conn *net.UDPConn
	ip4  netip.Addr
	ip6  netip.Addr
	done chan struct{}
}

func startFakeDNS(t *testing.T, ip4, ip6 netip.Addr) *fakeDNS {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen fake dns: %v", err)
	}
	f := &fakeDNS{conn: conn, ip4: ip4, ip6: ip6, done: make(chan struct{})}
	go f.serve()
	t.Cleanup(func() {
		_ = conn.Close() // unblocks serve, whose read then fails and returns
		<-f.done
	})
	return f
}

func (f *fakeDNS) addr() string { return f.conn.LocalAddr().String() }

func (f *fakeDNS) serve() {
	defer close(f.done)
	buf := make([]byte, 512)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			return // the test closed the socket
		}
		resp, ok := f.answer(buf[:n])
		if !ok {
			continue
		}
		if _, err := f.conn.WriteToUDP(resp, from); err != nil {
			return
		}
	}
}

// answer echoes the query's header and question and appends one address
// record for it. Only the fields Go's resolver checks are filled in.
func (f *fakeDNS) answer(query []byte) ([]byte, bool) {
	if len(query) < dnsHeaderLen {
		return nil, false
	}
	// Walk the QNAME's length-prefixed labels to find the question's end.
	i := dnsHeaderLen
	for i < len(query) && query[i] != 0 {
		i += int(query[i]) + 1
	}
	end := i + 5 // the root label, then qtype and qclass
	if end > len(query) {
		return nil, false
	}

	qtype := binary.BigEndian.Uint16(query[i+1 : i+3])
	var rdata []byte
	switch {
	case qtype == dnsTypeA && f.ip4.Is4():
		b := f.ip4.As4()
		rdata = b[:]
	case qtype == dnsTypeAAAA && f.ip6.Is6():
		b := f.ip6.As16()
		rdata = b[:]
	default:
		return nil, false
	}

	resp := append([]byte(nil), query[:end]...)
	binary.BigEndian.PutUint16(resp[2:4], 0x8180) // response, recursion available
	binary.BigEndian.PutUint16(resp[6:8], 1)      // one answer
	resp = append(resp, 0xc0, byte(dnsHeaderLen)) // name: pointer to the question
	resp = binary.BigEndian.AppendUint16(resp, qtype)
	resp = binary.BigEndian.AppendUint16(resp, 1)  // class IN
	resp = binary.BigEndian.AppendUint32(resp, 60) // ttl
	resp = binary.BigEndian.AppendUint16(resp, uint16(len(rdata)))
	return append(resp, rdata...), true
}

const (
	dnsHeaderLen = 12
	dnsTypeA     = 1
	dnsTypeAAAA  = 28
)
