package dialer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFirewallMarkRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := firewallMark(ctx); got != 0 {
		t.Errorf("bare context reported mark %d, want 0", got)
	}

	// Zero is "no mark", so it must not be stored: the control hook uses it to
	// decide there is nothing to apply.
	if got := firewallMark(WithFirewallMark(ctx, 0)); got != 0 {
		t.Errorf("mark 0 reported %d, want 0", got)
	}

	if got := firewallMark(WithFirewallMark(ctx, 0x2a)); got != 0x2a {
		t.Errorf("mark reported %#x, want 0x2a", got)
	}
}

// The transport has to carry the request's context into the dial, since that
// is what the mark rides in.
func TestTransportDialsWithRequestContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	transport := Transport()
	dialed := make(chan int, 1)
	inner := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- firewallMark(ctx)
		return inner(ctx, network, address)
	}

	req, err := http.NewRequestWithContext(
		WithFirewallMark(context.Background(), 0x2a), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := <-dialed; got != 0x2a {
		t.Errorf("dial saw mark %#x, want 0x2a", got)
	}
}
