package wg

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

type fakeClient struct {
	device      *DeviceInfo
	deviceErr   error
	deviceCalls int
	updates     []PeerEndpointUpdate
	updateErr   error
	closeCalls  int
	closeErr    error
}

func (f *fakeClient) Device(name string) (*DeviceInfo, error) {
	f.deviceCalls++
	if f.deviceErr != nil {
		return nil, f.deviceErr
	}
	return f.device, nil
}

func (f *fakeClient) UpdatePeerEndpoint(u PeerEndpointUpdate) error {
	f.updates = append(f.updates, u)
	return f.updateErr
}

func (f *fakeClient) Close() error {
	f.closeCalls++
	return f.closeErr
}

type fakeProxyConfig struct {
	protocol     string
	listen       uint16
	tunnelIfaces []string
}

func (f *fakeProxyConfig) GetInterfaceProtocol(deviceName string) string { return f.protocol }
func (f *fakeProxyConfig) GetProxyListenPort(deviceName string) uint16   { return f.listen }
func (f *fakeProxyConfig) GetProxyFib(deviceName string) int             { return 0 }
func (f *fakeProxyConfig) TunnelInterfaceNames() []string                { return f.tunnelIfaces }

func newTestProxyClient(t *testing.T, inner Client, cfg ProxyConfig) (Client, *wgproxy.Manager) {
	t.Helper()
	logger := zerolog.Nop()
	manager := wgproxy.NewManager(&logger)
	t.Cleanup(func() { _ = manager.Close() })
	return NewProxyClient(inner, manager, cfg, &logger), manager
}

func testKey(b byte) Key {
	var k Key
	for i := range k {
		k[i] = b
	}
	return k
}

func TestProxyClient_Device_ReturnsInfoUnchangedAndRegistersPeers(t *testing.T) {
	// A loopback socket stands in for the WireGuard device.
	wgSock, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("bind fake WG socket: %v", err)
	}
	defer func() { _ = wgSock.Close() }()
	wgPort := wgSock.LocalAddr().(*net.UDPAddr).Port

	peer := testKey(0x01)
	info := &DeviceInfo{
		Name:       "wg0",
		ListenPort: wgPort,
		PeerKeys:   []Key{peer, testKey(0x02)},
	}
	inner := &fakeClient{device: info}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	got, err := pc.Device("wg0")
	if err != nil {
		t.Fatalf("Device: unexpected error: %v", err)
	}
	if got != info {
		t.Errorf("Device returned a different DeviceInfo, want the inner client's unchanged")
	}
	if got.ListenPort != wgPort {
		t.Errorf("ListenPort = %d, want faithful %d", got.ListenPort, wgPort)
	}

	// A remote socket plays the peer's real endpoint.
	remote, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("bind remote socket: %v", err)
	}
	defer func() { _ = remote.Close() }()
	remotePort := remote.LocalAddr().(*net.UDPAddr).Port

	if err := pc.UpdatePeerEndpoint(PeerEndpointUpdate{
		DeviceName: "wg0",
		PublicKey:  peer,
		Host:       "127.0.0.1",
		Port:       remotePort,
	}); err != nil {
		t.Fatalf("UpdatePeerEndpoint: unexpected error: %v", err)
	}

	// A packet from the programmed remote must relay to the WG socket fed via
	// Device(), proving SetWGTarget and AddPeer both ran there.
	proxy, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For: %v", err)
	}
	outerPort := proxy.OuterPort(wgproxy.FamilyIPv4)
	if outerPort == 0 {
		t.Fatal("ipv4 outer socket not bound")
	}
	payload := []byte{0x04, 0x00, 0x00, 0x00, 0xde, 0xad}
	if _, err := remote.WriteToUDP(payload, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(outerPort)}); err != nil {
		t.Fatalf("write to outer socket: %v", err)
	}
	if err := wgSock.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 1500)
	n, _, err := wgSock.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("relay packet never reached the WG socket: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Errorf("relayed payload = %x, want %x", buf[:n], payload)
	}
}

func TestProxyClient_UpdatePeerEndpoint_SubstitutesLoopbackEndpoint(t *testing.T) {
	peer := testKey(0x03)
	inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820, PeerKeys: []Key{peer}}}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	if _, err := pc.Device("wg0"); err != nil {
		t.Fatalf("Device: %v", err)
	}
	if err := pc.UpdatePeerEndpoint(PeerEndpointUpdate{
		DeviceName: "wg0",
		PublicKey:  peer,
		Host:       "203.0.113.9",
		Port:       4242,
	}); err != nil {
		t.Fatalf("UpdatePeerEndpoint: %v", err)
	}

	if len(inner.updates) != 1 {
		t.Fatalf("inner updates = %d, want 1", len(inner.updates))
	}
	u := inner.updates[0]
	if u.DeviceName != "wg0" || u.PublicKey != peer {
		t.Errorf("delegated update device/key mismatch: %+v", u)
	}
	if u.Host != "127.0.0.1" {
		t.Errorf("delegated Host = %q, want 127.0.0.1", u.Host)
	}

	proxy, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For: %v", err)
	}
	innerAddr, err := proxy.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if u.Port != int(innerAddr.Port()) {
		t.Errorf("delegated Port = %d, want peer inner port %d", u.Port, innerAddr.Port())
	}
}

func TestProxyClient_UpdatePeerEndpoint_InvalidHost(t *testing.T) {
	inner := &fakeClient{}
	pc, _ := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	err := pc.UpdatePeerEndpoint(PeerEndpointUpdate{
		DeviceName: "wg0",
		PublicKey:  testKey(0x04),
		Host:       "not-an-ip",
		Port:       1234,
	})
	if err == nil {
		t.Fatal("expected error for unparsable host")
	}
	if len(inner.updates) != 0 {
		t.Errorf("inner client called despite invalid host: %+v", inner.updates)
	}
}

func TestProxyClient_Device_Idempotent(t *testing.T) {
	peer := testKey(0x05)
	inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820, PeerKeys: []Key{peer}}}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	if _, err := pc.Device("wg0"); err != nil {
		t.Fatalf("first Device: %v", err)
	}
	proxy, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For: %v", err)
	}
	outerPort := proxy.OuterPort(wgproxy.FamilyIPv4)
	innerAddr, err := proxy.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	if _, err := pc.Device("wg0"); err != nil {
		t.Fatalf("second Device: %v", err)
	}
	proxy2, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For after second Device: %v", err)
	}
	if proxy2 != proxy {
		t.Error("second Device created a new proxy")
	}
	if got := proxy2.OuterPort(wgproxy.FamilyIPv4); got != outerPort {
		t.Errorf("outer port changed: %d -> %d", outerPort, got)
	}
	innerAddr2, err := proxy2.AddPeer(peer)
	if err != nil {
		t.Fatalf("AddPeer after second Device: %v", err)
	}
	if innerAddr2 != innerAddr {
		t.Errorf("peer inner address changed: %s -> %s", innerAddr, innerAddr2)
	}
	if inner.deviceCalls != 2 {
		t.Errorf("inner Device calls = %d, want 2", inner.deviceCalls)
	}
}

// TunnelInterfaceNames feeds routeprobe.NewTunnelInterfaces via WithEscape;
// the escape itself is platform/privilege-gated and probed against the real
// route table, so this only exercises the plumbing stays non-nil end to end.
func TestProxyClient_Device_WithTunnelInterfaceNames(t *testing.T) {
	peer := testKey(0x06)
	inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820, PeerKeys: []Key{peer}}}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4", tunnelIfaces: []string{"wg0", "wg1"}})

	got, err := pc.Device("wg0")
	if err != nil {
		t.Fatalf("Device: unexpected error: %v", err)
	}
	if got != inner.device {
		t.Errorf("Device returned a different DeviceInfo, want the inner client's unchanged")
	}

	proxy, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For: %v", err)
	}
	if _, err := proxy.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
}

func TestProxyClient_Device_InnerError(t *testing.T) {
	wantErr := errors.New("device gone")
	inner := &fakeClient{deviceErr: wantErr}
	pc, _ := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	if _, err := pc.Device("wg0"); !errors.Is(err, wantErr) {
		t.Fatalf("Device error = %v, want %v", err, wantErr)
	}
}

func TestProxyClient_FamilyDerivation(t *testing.T) {
	cases := []struct {
		protocol string
		wantV4   bool
		wantV6   bool
	}{
		{"ipv4", true, false},
		{"ipv6", false, true},
		{"dualstack", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820}}
			pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: tc.protocol})

			if _, err := pc.Device("wg0"); err != nil {
				t.Fatalf("Device: %v", err)
			}
			proxy, err := manager.For("wg0", nil)
			if err != nil {
				t.Fatalf("manager.For: %v", err)
			}
			if got := proxy.OuterPort(wgproxy.FamilyIPv4) != 0; got != tc.wantV4 {
				t.Errorf("ipv4 outer bound = %v, want %v", got, tc.wantV4)
			}
			if got := proxy.OuterPort(wgproxy.FamilyIPv6) != 0; got != tc.wantV6 {
				t.Errorf("ipv6 outer bound = %v, want %v", got, tc.wantV6)
			}
		})
	}
}

func TestProxyClient_ListenOverride(t *testing.T) {
	// Reserve a free port, release it, and ask the proxy to pin it. A racing
	// re-bind by another process is possible but vanishingly rare in CI.
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("probe bind: %v", err)
	}
	port := uint16(probe.LocalAddr().(*net.UDPAddr).Port)
	_ = probe.Close()

	inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820}}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4", listen: port})

	if _, err := pc.Device("wg0"); err != nil {
		t.Fatalf("Device: %v", err)
	}
	proxy, err := manager.For("wg0", nil)
	if err != nil {
		t.Fatalf("manager.For: %v", err)
	}
	if got := proxy.OuterPort(wgproxy.FamilyIPv4); got != port {
		t.Errorf("outer port = %d, want configured %d", got, port)
	}
}

func TestProxyClient_Close_ClosesBoth(t *testing.T) {
	inner := &fakeClient{device: &DeviceInfo{Name: "wg0", ListenPort: 51820}}
	pc, manager := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	if _, err := pc.Device("wg0"); err != nil {
		t.Fatalf("Device: %v", err)
	}
	if err := pc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if inner.closeCalls != 1 {
		t.Errorf("inner Close calls = %d, want 1", inner.closeCalls)
	}
	if _, err := manager.For("wg0", nil); !errors.Is(err, wgproxy.ErrManagerClosed) {
		t.Errorf("manager still open after Close: err = %v", err)
	}
}

func TestProxyClient_Close_JoinsErrors(t *testing.T) {
	wantErr := errors.New("inner close failed")
	inner := &fakeClient{closeErr: wantErr}
	pc, _ := newTestProxyClient(t, inner, &fakeProxyConfig{protocol: "ipv4"})

	if err := pc.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close error = %v, want %v", err, wantErr)
	}
}
