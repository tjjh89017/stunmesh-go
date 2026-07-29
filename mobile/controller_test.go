//go:build mobile && (linux || android)

package mobile

import (
	"context"
	"errors"
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
// a real mobilebind.Bind or network access.
type fakeDiscoverer struct {
	results map[string]netip.AddrPort
	errs    map[string]error
}

func (f *fakeDiscoverer) Discover(_ context.Context, network, _ string) (netip.AddrPort, error) {
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
func newDiscoverTestController(protocol string, disc *fakeDiscoverer, lst *fakeListener) *controller {
	return &controller{
		node: &Node{listener: lst},
		cfg: &tunnelConfig{
			Interface: ifaceConfig{Protocol: protocol},
			Stun:      stunConfig{Addresses: []string{"stun.example.com:19302"}},
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
