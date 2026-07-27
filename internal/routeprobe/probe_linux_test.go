//go:build linux

package routeprobe

import (
	"net/netip"
	"testing"
)

const fixtureProcNetRoute = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlan0	00000000	0101A8C0	0003	0	0	600	00000000	0	0	0
eth0	0000FEA9	00000000	0001	0	0	1000	0000FFFF	0	0	0
wg0	00000000	00000000	0001	0	0	0	00000080	0	0	0
wg0	00000080	00000000	0001	0	0	0	00000080	0	0	0
wg0	0000000A	00000000	0001	0	0	0	000000FF	0	0	0
`

func TestParseProcNetRouteV4(t *testing.T) {
	routes, err := parseProcNetRouteV4([]byte(fixtureProcNetRoute))
	if err != nil {
		t.Fatalf("parseProcNetRouteV4() error = %v", err)
	}

	want := []Route{
		{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wlan0"},
		{Prefix: netip.MustParsePrefix("169.254.0.0/16"), Interface: "eth0"},
		{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Interface: "wg0"},
		{Prefix: netip.MustParsePrefix("128.0.0.0/1"), Interface: "wg0"},
		{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Interface: "wg0"},
	}

	if len(routes) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(routes), len(want), routes)
	}
	for i, r := range routes {
		if r != want[i] {
			t.Errorf("route[%d] = %+v, want %+v", i, r, want[i])
		}
	}
}

func TestParseProcNetRouteV4_malformed(t *testing.T) {
	_, err := parseProcNetRouteV4([]byte("Iface\tDestination\nwg0\tZZZZZZZZ\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n"))
	if err == nil {
		t.Fatal("expected error for malformed destination hex")
	}
}

const fixtureProcNetIPv6Route = `00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000001 00000000 00200200 wg0
20010db8000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000001 00000000 00000001 eth0
80000000000000000000000000000000 01 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000001 00000000 00000001 wg0
00000000000000000000000000000000 01 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000001 00000000 00000001 wg0
`

func TestParseProcNetRouteV6(t *testing.T) {
	routes, err := parseProcNetRouteV6([]byte(fixtureProcNetIPv6Route))
	if err != nil {
		t.Fatalf("parseProcNetRouteV6() error = %v", err)
	}

	want := []Route{
		{Prefix: netip.MustParsePrefix("::/0"), Interface: "wg0"},
		{Prefix: netip.MustParsePrefix("2001:db8::/64"), Interface: "eth0"},
		{Prefix: netip.MustParsePrefix("8000::/1"), Interface: "wg0"},
		{Prefix: netip.MustParsePrefix("::/1"), Interface: "wg0"},
	}

	if len(routes) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(routes), len(want), routes)
	}
	for i, r := range routes {
		if r != want[i] {
			t.Errorf("route[%d] = %+v, want %+v", i, r, want[i])
		}
	}
}

func TestParseProcNetRouteV6_malformed(t *testing.T) {
	_, err := parseProcNetRouteV6([]byte("zz 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000001 00000000 00000001 wg0\n"))
	if err == nil {
		t.Fatal("expected error for malformed destination hex")
	}
}

// Together with routeprobe_test.go's TestHasCoveringDefault, this confirms
// the fixture's wg0 /1+/1 pair is recognized as a covering default while the
// eth0/wlan0 routes on their own are not mistaken for one.
func TestLinuxFixturesEndToEnd(t *testing.T) {
	v4Routes, err := parseProcNetRouteV4([]byte(fixtureProcNetRoute))
	if err != nil {
		t.Fatalf("parseProcNetRouteV4() error = %v", err)
	}
	isWG := isWG("wg0")
	if !HasCoveringDefault(v4Routes, IPv4, isWG) {
		t.Error("expected v4 default route on wg0 to be detected as covering")
	}

	v6Routes, err := parseProcNetRouteV6([]byte(fixtureProcNetIPv6Route))
	if err != nil {
		t.Fatalf("parseProcNetRouteV6() error = %v", err)
	}
	if !HasCoveringDefault(v6Routes, IPv6, isWG) {
		t.Error("expected v6 default route on wg0 to be detected as covering")
	}
}
