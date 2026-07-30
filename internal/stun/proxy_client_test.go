package stun

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	stunmsg "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
	"golang.org/x/net/dns/dnsmessage"
)

// Compile-time guard: *wgproxy.Proxy satisfies StunTransport structurally.
var _ StunTransport = (*wgproxy.Proxy)(nil)

// fakeTransport records Exchange calls and replies from a scripted function.
type fakeTransport struct {
	calls    int
	lastAddr netip.AddrPort
	respond  func(txnID [12]byte, packet []byte) ([]byte, error)
}

func (f *fakeTransport) Exchange(_ context.Context, server netip.AddrPort, txnID [12]byte, packet []byte) ([]byte, error) {
	f.calls++
	f.lastAddr = server
	return f.respond(txnID, packet)
}

func bindingSuccess(t *testing.T, txnID [12]byte, ip net.IP, port int) []byte {
	t.Helper()
	msg, err := stunmsg.Build(
		stunmsg.NewTransactionIDSetter(txnID),
		stunmsg.BindingSuccess,
		&stunmsg.XORMappedAddress{IP: ip, Port: port},
	)
	if err != nil {
		t.Fatalf("build binding success: %v", err)
	}
	return msg.Raw
}

func bindingError(t *testing.T, txnID [12]byte, code stunmsg.ErrorCode, reason string) []byte {
	t.Helper()
	msg, err := stunmsg.Build(
		stunmsg.NewTransactionIDSetter(txnID),
		stunmsg.BindingError,
		stunmsg.ErrorCodeAttribute{Code: code, Reason: []byte(reason)},
	)
	if err != nil {
		t.Fatalf("build binding error: %v", err)
	}
	return msg.Raw
}

func TestProxyBackedConnect_IPv4(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, packet []byte) ([]byte, error) {
			req := &stunmsg.Message{Raw: packet}
			if err := req.Decode(); err != nil {
				t.Fatalf("request does not decode as STUN: %v", err)
			}
			if req.Type != stunmsg.BindingRequest {
				t.Fatalf("request type = %v, want binding request", req.Type)
			}
			if req.TransactionID != txnID {
				t.Fatalf("txnID argument %x does not match packet txn %x", txnID, req.TransactionID)
			}
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.9"), 41414), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	host, port, err := client.Connect(context.Background(), "192.0.2.1:3478")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if host != "203.0.113.9" || port != 41414 {
		t.Fatalf("got %s:%d, want 203.0.113.9:41414", host, port)
	}
	want := netip.MustParseAddrPort("192.0.2.1:3478")
	if ft.lastAddr != want {
		t.Fatalf("server addr = %v, want %v", ft.lastAddr, want)
	}
}

func TestProxyBackedConnect_IPv6(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("2001:db8::9"), 5353), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv6", &logger)

	host, port, err := client.Connect(context.Background(), "[2001:db8::1]:3478")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if host != "2001:db8::9" || port != 5353 {
		t.Fatalf("got %s:%d, want [2001:db8::9]:5353", host, port)
	}
	if !ft.lastAddr.Addr().Is6() {
		t.Fatalf("server addr = %v, want IPv6", ft.lastAddr)
	}
}

func TestProxyBackedConnect_BindingErrorResponse(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingError(t, txnID, stunmsg.CodeServerError, "server error"), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	_, _, err := client.Connect(context.Background(), "192.0.2.1:3478")
	if err == nil {
		t.Fatal("Connect succeeded on a binding error response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q does not carry the STUN error code", err)
	}
}

func TestProxyBackedConnect_TxnIDMismatch(t *testing.T) {
	ft := &fakeTransport{
		respond: func(_ [12]byte, _ []byte) ([]byte, error) {
			other := [12]byte{0xde, 0xad, 0xbe, 0xef}
			return bindingSuccess(t, other, net.ParseIP("203.0.113.9"), 41414), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	_, _, err := client.Connect(context.Background(), "192.0.2.1:3478")
	if !errors.Is(err, ErrTxnIDMismatch) {
		t.Fatalf("err = %v, want ErrTxnIDMismatch", err)
	}
}

func TestProxyBackedConnect_TransportError(t *testing.T) {
	transportErr := errors.New("proxy exchange timed out")
	ft := &fakeTransport{
		respond: func(_ [12]byte, _ []byte) ([]byte, error) {
			return nil, transportErr
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	_, _, err := client.Connect(context.Background(), "192.0.2.1:3478")
	if !errors.Is(err, transportErr) {
		t.Fatalf("err = %v, want transport error", err)
	}
}

// Stop must leave the transport usable across Start/Stop cycles.
func TestProxyBackedStopLeavesTransportUsable(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.9"), 41414), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	for i := 0; i < 3; i++ {
		client.Start(context.Background())
		if _, _, err := client.Connect(context.Background(), "192.0.2.1:3478"); err != nil {
			t.Fatalf("Connect cycle %d: %v", i, err)
		}
		if err := client.Stop(); err != nil {
			t.Fatalf("Stop cycle %d: %v", i, err)
		}
	}
	if ft.calls != 3 {
		t.Fatalf("transport calls = %d, want 3", ft.calls)
	}
}

func TestProxyLookupFactory_ResolvesTransportPerDevice(t *testing.T) {
	byDevice := map[string]*fakeTransport{
		"wg0": {respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.1"), 1111), nil
		}},
		"wg1": {respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.2"), 2222), nil
		}},
	}
	logger := zerolog.Nop()
	factory := NewProxyLookupFactory(func(deviceName string) (StunTransport, error) {
		ft, ok := byDevice[deviceName]
		if !ok {
			return nil, errors.New("unknown device")
		}
		return ft, nil
	}, &logger)

	for device, wantPort := range map[string]int{"wg0": 1111, "wg1": 2222} {
		client, err := factory(context.Background(), device, 0, "ipv4", 0, nil, false)
		if err != nil {
			t.Fatalf("factory(%s): %v", device, err)
		}
		_, port, err := client.Connect(context.Background(), "192.0.2.1:3478")
		if err != nil {
			t.Fatalf("Connect(%s): %v", device, err)
		}
		if port != wantPort {
			t.Fatalf("Connect(%s) port = %d, want %d", device, port, wantPort)
		}
	}
}

func TestProxyLookupFactory_LookupErrorPropagates(t *testing.T) {
	logger := zerolog.Nop()
	wantErr := errors.New("proxy not initialized yet")
	factory := NewProxyLookupFactory(func(string) (StunTransport, error) {
		return nil, wantErr
	}, &logger)

	if _, err := factory(context.Background(), "wg0", 0, "ipv4", 0, nil, false); !errors.Is(err, wantErr) {
		t.Fatalf("expected lookup error, got %v", err)
	}
}

func TestProxyLookupFactory_WarnsOnceForIgnoredArgs(t *testing.T) {
	ft := &fakeTransport{respond: func(_ [12]byte, _ []byte) ([]byte, error) { return nil, errors.New("unused") }}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	factory := NewProxyLookupFactory(func(string) (StunTransport, error) { return ft, nil }, &logger)

	for i := 0; i < 3; i++ {
		if _, err := factory(context.Background(), "wg0", 51820, "ipv4", 0, nil, false); err != nil {
			t.Fatalf("factory cycle %d: %v", i, err)
		}
	}
	if got := strings.Count(buf.String(), "ignored"); got != 1 {
		t.Fatalf("ignored-args warning logged %d times, want exactly 1; log: %s", got, buf.String())
	}
}

func TestProxyLookupFactory_NoWarnWithoutIgnoredArgs(t *testing.T) {
	ft := &fakeTransport{respond: func(_ [12]byte, _ []byte) ([]byte, error) { return nil, errors.New("unused") }}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	factory := NewProxyLookupFactory(func(string) (StunTransport, error) { return ft, nil }, &logger)

	if _, err := factory(context.Background(), "wg0", 0, "ipv6", 0, nil, false); err != nil {
		t.Fatalf("factory: %v", err)
	}
	if strings.Contains(buf.String(), "ignored") {
		t.Fatalf("unexpected ignored-args warning: %s", buf.String())
	}
}

// The proxy carries the exchange out through its escaped outer socket, but the
// server name's lookup is a socket of its own. This pins that it goes through
// the dialer's escaped resolver: the name exists in no real zone and only the
// nameserver in the Escape answers it, so a regression to net.ResolveUDPAddr
// fails here instead of silently resolving over the default route.
func TestProxyBackedConnect_ResolvesThroughEscapedPath(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.9"), 41414), nil
		},
	}
	logger := zerolog.Nop()
	client := NewProxyBacked(ft, "ipv4", &logger)

	ctx := dialer.WithEscape(context.Background(), dialer.Escape{
		DNSServers: []string{fakeDNSServer(t)},
	})
	if _, _, err := client.Connect(ctx, "stun.escape-test.invalid:3478"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := netip.MustParseAddrPort("127.0.0.99:3478")
	if ft.lastAddr != want {
		t.Fatalf("server addr = %v, want the fake nameserver's answer %v", ft.lastAddr, want)
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
				Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true},
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
