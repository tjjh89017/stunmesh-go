package dialer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

func TestEscapeRoundTrip(t *testing.T) {
	if got := escapeFrom(context.Background()); got.FirewallMark != 0 || got.Fib != 0 {
		t.Errorf("bare context reported %+v, want a zero Escape", got)
	}

	want := Escape{FirewallMark: 0x2a, Fib: 2}
	got := escapeFrom(WithEscape(context.Background(), want))
	if got.FirewallMark != want.FirewallMark || got.Fib != want.Fib {
		t.Errorf("reported %+v, want %+v", got, want)
	}
}

// fakeProtector is a minimal Protector for tests, independent of mobile's
// SocketProtector -- this package must not import mobile.
type fakeProtector struct{ protected bool }

func (p *fakeProtector) Protect(int32) bool {
	p.protected = true
	return true
}

// A Protector is carried the same way as the rest of Escape: mobile is the
// only caller today, but the round trip itself is platform-independent.
func TestEscapeRoundTripProtector(t *testing.T) {
	if got := escapeFrom(context.Background()).Protector; got != nil {
		t.Errorf("bare context reported a Protector, want nil")
	}

	p := &fakeProtector{}
	got := escapeFrom(WithEscape(context.Background(), Escape{Protector: p})).Protector
	if got != Protector(p) {
		t.Errorf("reported %+v, want %+v", got, p)
	}
}

// EscapeFrom is the exported introspection point other packages' tests (e.g.
// mobile's) use to verify they wired WithEscape correctly.
func TestEscapeFromExported(t *testing.T) {
	want := Escape{FirewallMark: 7}
	if got := EscapeFrom(WithEscape(context.Background(), want)); got.FirewallMark != want.FirewallMark {
		t.Errorf("EscapeFrom reported %+v, want %+v", got, want)
	}
}

// The bind-to-interface options are per-family, so the network of the attempt
// has to pick one. Control sees "tcp4"/"tcp6", never the "tcp" a caller asked
// for.
func TestFamilyFromNetwork(t *testing.T) {
	for network, want := range map[string]routeprobe.Family{
		"tcp4": routeprobe.IPv4,
		"tcp6": routeprobe.IPv6,
		"udp4": routeprobe.IPv4,
		"udp6": routeprobe.IPv6,
		"tcp":  routeprobe.IPv4,
	} {
		if got := family(network); got != want {
			t.Errorf("family(%q) = %v, want %v", network, got, want)
		}
	}
}

// Without any managed interfaces there is nothing to tell a tunnel from the
// physical path, so no probe runs and nothing is bound.
func TestBoundInterfaceWithoutTunnelIfaces(t *testing.T) {
	_, ok, err := boundInterface(Escape{}, routeprobe.IPv4)
	if err != nil || ok {
		t.Errorf("got (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

// The transport has to carry the request's context into the dial, since that
// is what the escape rides in.
func TestTransportDialsWithRequestContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	transport := Transport()
	dialed := make(chan Escape, 1)
	inner := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- escapeFrom(ctx)
		return inner(ctx, network, address)
	}

	req, err := http.NewRequestWithContext(
		WithEscape(context.Background(), Escape{FirewallMark: 0x2a}), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := <-dialed; got.FirewallMark != 0x2a {
		t.Errorf("dial saw mark %#x, want 0x2a", got.FirewallMark)
	}
}

// DNS lookups must not fall through to net.DefaultResolver: on android that
// resolves via Bionic getaddrinfo/netd, which cannot be protected, so a DNS
// query would fail outright whenever the covering tunnel it should escape is
// down. Routing the resolver's own dial through DialContext puts DNS on the
// same protected path as the TCP connect. http.Transport has no Resolver
// field of its own -- the wiring belongs on the net.Dialer DialContext uses.
func TestDialerResolverDialsThroughProtectedPath(t *testing.T) {
	d := newDialer()

	if d.Resolver == nil {
		t.Fatal("newDialer().Resolver is nil, want a resolver wired to the protected dial path")
	}
	if !d.Resolver.PreferGo {
		t.Error("newDialer().Resolver.PreferGo = false, want true so DNS queries use the Go resolver's Dial hook instead of the platform resolver")
	}

	got := reflect.ValueOf(d.Resolver.Dial).Pointer()
	want := reflect.ValueOf(dialDNS).Pointer()
	if got != want {
		t.Error("newDialer().Resolver.Dial is not dialDNS, want DNS lookups to share the protected dial path with TCP connects")
	}
}

// Escape.DNSServers entries arrive the way platforms report them; the dial
// needs "host:port", and anything that is not an IP literal must be refused
// -- a hostname here would send the dial back through the resolver whose
// nameserver it was supposed to be, recursing without end.
func TestNormalizeDNSAddr(t *testing.T) {
	for in, want := range map[string]string{
		"8.8.8.8":            "8.8.8.8:53",
		"8.8.8.8:":           "8.8.8.8:53",
		"8.8.8.8:5353":       "8.8.8.8:5353",
		"2001:db8::1":        "[2001:db8::1]:53",
		"[2001:db8::1]":      "[2001:db8::1]:53",
		"[2001:db8::1]:5353": "[2001:db8::1]:5353",
	} {
		got, ok := normalizeDNSAddr(in)
		if !ok || got != want {
			t.Errorf("normalizeDNSAddr(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"dns.example", "dns.example:5353", "", ":53", "not an address",
		"8.8.8.8:0", "8.8.8.8:99999", "8.8.8.8:-1", "8.8.8.8:notaport",
	} {
		if got, ok := normalizeDNSAddr(in); ok {
			t.Errorf("normalizeDNSAddr(%q) = (%q, true), want rejection", in, got)
		}
	}
}

// ValidNameserver is the exported face of the same rule, for callers
// (mobile's SetDNSServers) filtering platform-reported strings.
func TestValidNameserver(t *testing.T) {
	if !ValidNameserver("8.8.8.8") || ValidNameserver("dns.example") {
		t.Error("ValidNameserver should accept IP literals and reject hostnames")
	}
}

// Hostname entries that slip into the list anyway (only mobile filters
// today) are skipped, not dialed: recursion protection lives here, at the
// point of use, not only in the callers.
func TestPickDNSServerSkipsHostnames(t *testing.T) {
	addr, ok := pickDNSServer([]string{"dns.example", "192.0.2.7"})
	if !ok || addr != "192.0.2.7:53" {
		t.Errorf("pickDNSServer = (%q, %v), want (\"192.0.2.7:53\", true)", addr, ok)
	}
	if addr, ok := pickDNSServer([]string{"dns.example", "other.example"}); ok {
		t.Errorf("all-hostname list picked %q, want ok=false so the resolv.conf address stands", addr)
	}
	if addr, ok := pickDNSServer(nil); ok {
		t.Errorf("empty list picked %q, want ok=false", addr)
	}
}

// The rotation counter lives as long as the process; past 2^31 dials a plain
// int conversion of it goes negative on 32-bit targets (armeabi-v7a is a
// shipping mobile ABI) and a negative modulo would index out of range.
func TestPickDNSServerCounterWrap(t *testing.T) {
	old := dnsRotation.Load()
	defer dnsRotation.Store(old)

	dnsRotation.Store(1<<31 + 1)
	if addr, ok := pickDNSServer([]string{"192.0.2.1", "192.0.2.2"}); !ok || addr == "" {
		t.Errorf("pickDNSServer after counter wrap = (%q, %v), want a server", addr, ok)
	}
}

// With DNSServers in the Escape, dialDNS must ignore the address Go derived
// from /etc/resolv.conf -- on android that is a localhost fallback nothing
// answers -- and rotate across the configured servers so a lookup's retries
// reach a second server instead of re-dialing a dead first one. A UDP dial
// sends nothing, so RemoteAddr shows where a query would have gone.
func TestDialDNSUsesEscapeServers(t *testing.T) {
	ctx := WithEscape(context.Background(), Escape{
		DNSServers: []string{"192.0.2.1", "192.0.2.2:5353"},
	})

	seen := make(map[string]bool)
	for range 2 {
		conn, err := dialDNS(ctx, "udp", "127.0.0.1:53")
		if err != nil {
			t.Fatal(err)
		}
		seen[conn.RemoteAddr().String()] = true
		_ = conn.Close()
	}
	if !seen["192.0.2.1:53"] || !seen["192.0.2.2:5353"] {
		t.Errorf("two dials reached %v, want both 192.0.2.1:53 and 192.0.2.2:5353", seen)
	}
}

// Without DNSServers the resolv.conf-derived address is dialed unchanged --
// the desktop path, where resolv.conf is trustworthy.
func TestDialDNSWithoutEscapeServers(t *testing.T) {
	conn, err := dialDNS(context.Background(), "udp", "127.0.0.1:5353")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if got := conn.RemoteAddr().String(); got != "127.0.0.1:5353" {
		t.Errorf("dial went to %s, want 127.0.0.1:5353", got)
	}
}

// End to end through the real Go resolver: a lookup with Escape.DNSServers
// pointing at a local fake must query that server, not whatever
// /etc/resolv.conf says. This is the android scenario in miniature -- the
// configured list is the only thing standing between the resolver and a
// dead localhost fallback.
func TestResolverUsesEscapeDNSServer(t *testing.T) {
	server := fakeDNSServer(t)

	ctx, cancel := context.WithTimeout(
		WithEscape(context.Background(), Escape{DNSServers: []string{server}}),
		5*time.Second)
	defer cancel()

	addrs, err := newDialer().Resolver.LookupHost(ctx, "escape-test.invalid")
	if err != nil {
		t.Fatalf("LookupHost: %v", err)
	}
	if !slices.Contains(addrs, "127.0.0.99") {
		t.Errorf("LookupHost = %v, want to contain the fake server's answer 127.0.0.99", addrs)
	}
}

// fakeDNSServer answers every A question with 127.0.0.99 and everything else
// with an empty success, and returns its "host:port".
func fakeDNSServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buf[:n]); err != nil {
				continue
			}
			resp := dnsmessage.Message{
				Header: dnsmessage.Header{
					ID:            query.ID,
					Response:      true,
					Authoritative: true,
				},
				Questions: query.Questions,
			}
			if len(query.Questions) == 1 && query.Questions[0].Type == dnsmessage.TypeA {
				resp.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{
						Name:  query.Questions[0].Name,
						Type:  dnsmessage.TypeA,
						Class: dnsmessage.ClassINET,
						TTL:   60,
					},
					Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 99}},
				}}
			}
			packed, err := resp.Pack()
			if err != nil {
				continue
			}
			_, _ = pc.WriteTo(packed, addr)
		}
	}()
	return pc.LocalAddr().String()
}
