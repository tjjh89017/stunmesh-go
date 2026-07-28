package dialer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

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
