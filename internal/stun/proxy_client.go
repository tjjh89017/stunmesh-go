// Proxy-backed STUN client (Windows data path). The client never holds a
// socket: it talks through StunTransport, implemented by the long-lived
// wgproxy outer sockets, so the resolver's per-Resolve Start/Stop cycle has
// nothing it could open or close (plan §2.5 threat 1). Timeouts come from ctx
// plus the transport's internal timer — never socket read deadlines. Portable
// so the parse/build logic unit-tests on every GOOS.
package stun

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	stun "github.com/pion/stun/v3"
	"github.com/rs/zerolog"
)

// ErrTxnIDMismatch means the response echoed a different transaction ID than
// the request carried.
var ErrTxnIDMismatch = errors.New("stun: response transaction ID does not match request")

// StunTransport is the narrow handle the proxy-backed client sends STUN
// exchanges through. It uses plain [12]byte (wgproxy.TxnID is an alias of it)
// so *wgproxy.Proxy satisfies it structurally without this package importing
// wgproxy. Exchange returns the raw response bytes; parsing is the client's
// job.
type StunTransport interface {
	Exchange(ctx context.Context, server netip.AddrPort, txnID [12]byte, packet []byte) ([]byte, error)
}

// ProxyBacked is a StunClient that rides a proxy's outer socket.
type ProxyBacked struct {
	transport StunTransport
	protocol  string
	logger    zerolog.Logger
}

// NewProxyBacked builds a proxy-backed client for one protocol ("ipv4" or
// "ipv6").
func NewProxyBacked(transport StunTransport, protocol string, logger *zerolog.Logger) *ProxyBacked {
	return &ProxyBacked{
		transport: transport,
		protocol:  protocol,
		logger:    logger.With().Str("component", "stun").Logger(),
	}
}

// NewProxyBackedFactory adapts a single fixed transport into the resolver's
// ClientFactory; see NewProxyLookupFactory for the ignored-argument policy.
func NewProxyBackedFactory(transport StunTransport, logger *zerolog.Logger) ClientFactory {
	return NewProxyLookupFactory(func(string) (StunTransport, error) {
		return transport, nil
	}, logger)
}

// TransportLookup resolves a device name to the transport carrying its STUN
// exchanges (the device's proxy outer sockets). It must never create the
// transport — proxies are bound by the wg decorator's Device() call, which
// bootstrap runs before any Resolve.
type TransportLookup func(deviceName string) (StunTransport, error)

// NewProxyLookupFactory builds a ClientFactory that resolves the transport
// per device on every Resolve. The factory arguments that only make sense for
// socket-owning clients (port, firewallMark, listenInterfaces,
// listenDefaultRoute) are ignored, with one warning for the process lifetime.
func NewProxyLookupFactory(lookup TransportLookup, logger *zerolog.Logger) ClientFactory {
	var warnIgnored sync.Once
	return func(_ context.Context, deviceName string, port uint16, protocol string, firewallMark int, listenInterfaces []string, listenDefaultRoute bool) (StunClient, error) {
		transport, err := lookup(deviceName)
		if err != nil {
			return nil, err
		}
		if port != 0 || firewallMark != 0 || len(listenInterfaces) > 0 || listenDefaultRoute {
			warnIgnored.Do(func() {
				logger.Warn().Msg("port/fwmark/listen_interfaces/listen_default_route are ignored by the proxy-backed STUN client (it borrows the proxy's outer socket)")
			})
		}
		return NewProxyBacked(transport, protocol, logger), nil
	}
}

// Start is structurally empty: there is no socket to open (§2.5 threat 1).
func (c *ProxyBacked) Start(_ context.Context) {}

// Stop is structurally empty: there is no socket to close, so the transport
// stays usable for the next Resolve cycle.
func (c *ProxyBacked) Stop() error { return nil }

// Connect builds a binding request with a fresh random transaction ID, sends
// it through the transport, and returns the XOR-mapped endpoint.
func (c *ProxyBacked) Connect(ctx context.Context, stunAddr string) (string, int, error) {
	c.logger.Info().Msgf("connecting to STUN server: %s", stunAddr)

	network := "udp4"
	if c.protocol == "ipv6" {
		network = "udp6"
	}
	addr, err := net.ResolveUDPAddr(network, stunAddr)
	if err != nil {
		return "", 0, err
	}
	server := addr.AddrPort()
	server = netip.AddrPortFrom(server.Addr().Unmap(), server.Port())

	req, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return "", 0, err
	}

	raw, err := c.transport.Exchange(ctx, server, req.TransactionID, req.Raw)
	if err != nil {
		return "", 0, err
	}

	return parseBindingResponse(ctx, raw, req.TransactionID)
}

// parseBindingResponse validates a raw STUN response (Decode checks the magic
// cookie) against the request's transaction ID and extracts the mapped
// endpoint from a binding success, or the error code from a binding error.
func parseBindingResponse(ctx context.Context, raw []byte, txnID [12]byte) (string, int, error) {
	msg := &stun.Message{Raw: raw}
	if err := msg.Decode(); err != nil {
		return "", 0, err
	}
	if msg.TransactionID != txnID {
		return "", 0, ErrTxnIDMismatch
	}

	switch msg.Type {
	case stun.BindingSuccess:
		xorAddr := Parse(ctx, msg)
		if xorAddr == nil {
			return "", 0, ErrNoMappedAddress
		}
		return xorAddr.IP.String(), xorAddr.Port, nil
	case stun.BindingError:
		var code stun.ErrorCodeAttribute
		if err := code.GetFrom(msg); err != nil {
			return "", 0, errors.New("stun: binding error response without ERROR-CODE")
		}
		return "", 0, fmt.Errorf("stun: binding error response: %d %s", code.Code, code.Reason)
	default:
		return "", 0, fmt.Errorf("stun: unexpected response type %s", msg.Type)
	}
}
