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
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

// The production package deliberately does not import wgproxy; this guards the
// structural satisfaction (via the TxnID = [12]byte alias) at compile time.
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

// The §3 regression guard: Stop has nothing it could close, so the transport
// stays usable across Start/Stop cycles (resolver calls them on every Resolve).
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

func TestProxyBackedFactory_WarnsOnceForIgnoredArgs(t *testing.T) {
	ft := &fakeTransport{
		respond: func(txnID [12]byte, _ []byte) ([]byte, error) {
			return bindingSuccess(t, txnID, net.ParseIP("203.0.113.9"), 41414), nil
		},
	}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	factory := NewProxyBackedFactory(ft, &logger)

	for i := 0; i < 3; i++ {
		client, err := factory(context.Background(), "wg0", 51820, "ipv4", 0, []string{"eth0"}, true)
		if err != nil {
			t.Fatalf("factory cycle %d: %v", i, err)
		}
		if _, _, err := client.Connect(context.Background(), "192.0.2.1:3478"); err != nil {
			t.Fatalf("Connect cycle %d: %v", i, err)
		}
	}

	if got := strings.Count(buf.String(), "ignored"); got != 1 {
		t.Fatalf("ignored-args warning logged %d times, want exactly 1; log: %s", got, buf.String())
	}
}

func TestProxyBackedFactory_NoWarnWithoutIgnoredArgs(t *testing.T) {
	ft := &fakeTransport{respond: func(_ [12]byte, _ []byte) ([]byte, error) { return nil, errors.New("unused") }}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	factory := NewProxyBackedFactory(ft, &logger)

	if _, err := factory(context.Background(), "wg0", 0, "ipv6", 0, nil, false); err != nil {
		t.Fatalf("factory: %v", err)
	}
	if strings.Contains(buf.String(), "ignored") {
		t.Fatalf("unexpected ignored-args warning: %s", buf.String())
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
