// Proxy-backed STUN client (Windows data path). It never holds a socket —
// exchanges ride StunTransport — and timeouts come from ctx plus the
// transport's timer, never socket read deadlines.
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

// ErrTxnIDMismatch means the response echoed a different transaction ID.
var ErrTxnIDMismatch = errors.New("stun: response transaction ID does not match request")

// StunTransport carries STUN exchanges. Plain [12]byte (wgproxy.TxnID's
// underlying type) lets *wgproxy.Proxy satisfy it without an import cycle.
type StunTransport interface {
	Exchange(ctx context.Context, server netip.AddrPort, txnID [12]byte, packet []byte) ([]byte, error)
}

// ProxyBacked is a StunClient that rides a proxy's outer socket.
type ProxyBacked struct {
	transport StunTransport
	protocol  string
	logger    zerolog.Logger
}

// NewProxyBacked builds a proxy-backed client for "ipv4" or "ipv6".
func NewProxyBacked(transport StunTransport, protocol string, logger *zerolog.Logger) *ProxyBacked {
	return &ProxyBacked{
		transport: transport,
		protocol:  protocol,
		logger:    logger.With().Str("component", "stun").Logger(),
	}
}

// TransportLookup resolves a device name to its STUN transport. It must never
// create one — proxies are bound by the wg decorator before any Resolve.
type TransportLookup func(deviceName string) (StunTransport, error)

// NewProxyLookupFactory builds a ClientFactory resolving the transport per
// device; socket-owning arguments are ignored, warned once per process.
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

// Start is structurally empty: there is no socket to open.
func (c *ProxyBacked) Start(_ context.Context) {}

// Stop is structurally empty: the transport stays usable for the next Resolve.
func (c *ProxyBacked) Stop() error { return nil }

// Connect sends a binding request through the transport and returns the
// XOR-mapped endpoint.
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

// parseBindingResponse validates the response against the request's
// transaction ID and extracts the mapped endpoint or error code.
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
