package wg

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

// ProxyConfig provides the per-device settings the decorator needs; satisfied
// by *config.DeviceConfig. Platform gating is wiring's responsibility.
type ProxyConfig interface {
	GetInterfaceProtocol(deviceName string) string
	GetProxyListenPort(deviceName string) uint16
	// GetProxyFib returns the freebsd-only escape FIB number for deviceName
	// (0 means not configured); ignored on every other platform.
	GetProxyFib(deviceName string) int
	// TunnelInterfaceNames returns every configured WireGuard interface name,
	// used to scope the tunnel-escape route probe to devices stunmesh manages.
	TunnelInterfaceNames() []string
}

// proxyClient decorates a Client so every device is fronted by a wgproxy
// relay; controllers see the plain Client interface and stay unmodified.
type proxyClient struct {
	inner   Client
	manager *wgproxy.Manager
	config  ProxyConfig
	logger  zerolog.Logger
}

// NewProxyClient wraps inner with the proxy decorator.
func NewProxyClient(inner Client, manager *wgproxy.Manager, config ProxyConfig, logger *zerolog.Logger) Client {
	return &proxyClient{
		inner:   inner,
		manager: manager,
		config:  config,
		logger:  logger.With().Str("component", "wg.proxy_client").Logger(),
	}
}

// Device delegates, then feeds the proxy the WG-side target and registers
// every peer. DeviceInfo is returned unchanged — ListenPort stays real.
func (c *proxyClient) Device(name string) (*DeviceInfo, error) {
	info, err := c.inner.Device(name)
	if err != nil {
		return nil, err
	}
	tunnelIfaces := routeprobe.NewTunnelInterfaces(c.config.TunnelInterfaceNames()...)
	proxy, err := c.ensureProxy(name, wgproxy.WithEscape(info.FirewallMark, c.config.GetProxyFib(name), tunnelIfaces))
	if err != nil {
		return nil, err
	}
	proxy.SetWGTarget(uint16(info.ListenPort))
	for _, key := range info.PeerKeys {
		if _, err := proxy.AddPeer(key); err != nil {
			return nil, fmt.Errorf("wg: register peer with proxy: %w", err)
		}
	}
	return info, nil
}

// UpdatePeerEndpoint programs the proxy with the real remote, then delegates
// with the endpoint replaced by the peer's loopback inner socket.
func (c *proxyClient) UpdatePeerEndpoint(u PeerEndpointUpdate) error {
	addr, err := netip.ParseAddr(u.Host)
	if err != nil {
		return fmt.Errorf("wg: parse peer endpoint host %q: %w", u.Host, err)
	}
	proxy, err := c.ensureProxy(u.DeviceName)
	if err != nil {
		return err
	}
	innerAddr, err := proxy.AddPeer(u.PublicKey)
	if err != nil {
		return fmt.Errorf("wg: register peer with proxy: %w", err)
	}
	remote := netip.AddrPortFrom(addr, uint16(u.Port))
	proxy.SetPeerEndpoint(u.PublicKey, remote)
	c.logger.Debug().
		Str("device", u.DeviceName).
		Str("remote", remote.String()).
		Str("inner", innerAddr.String()).
		Msg("peer endpoint substituted with proxy inner socket")
	return c.inner.UpdatePeerEndpoint(PeerEndpointUpdate{
		DeviceName: u.DeviceName,
		PublicKey:  u.PublicKey,
		Host:       innerAddr.Addr().String(),
		Port:       int(innerAddr.Port()),
	})
}

func (c *proxyClient) Close() error {
	return errors.Join(c.inner.Close(), c.manager.Close())
}

func (c *proxyClient) ensureProxy(deviceName string, opts ...wgproxy.Option) (*wgproxy.Proxy, error) {
	return c.manager.For(deviceName, c.families(deviceName), opts...)
}

func (c *proxyClient) families(deviceName string) map[wgproxy.Family]uint16 {
	port := c.config.GetProxyListenPort(deviceName)
	switch c.config.GetInterfaceProtocol(deviceName) {
	case "ipv6":
		return map[wgproxy.Family]uint16{wgproxy.FamilyIPv6: port}
	case "dualstack":
		return map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: port, wgproxy.FamilyIPv6: port}
	default:
		return map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: port}
	}
}
