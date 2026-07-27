//go:build darwin || freebsd

package routeprobe

import (
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"

	"golang.org/x/net/route"
)

func TestRouteMessagePrefix(t *testing.T) {
	v4 := netip.MustParseAddr("192.168.1.1")
	v6 := netip.MustParseAddr("2001:db8::1")

	tests := []struct {
		name       string
		rm         *route.RouteMessage
		family     Family
		wantPrefix netip.Prefix
		wantOK     bool
	}{
		{
			name: "v4 default route with no netmask slot",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet4Addr{IP: [4]byte{0, 0, 0, 0}},
				},
			},
			family:     IPv4,
			wantPrefix: netip.MustParsePrefix("0.0.0.0/0"),
			wantOK:     true,
		},
		{
			name: "v4 host route with no netmask slot is full length",
			rm: &route.RouteMessage{
				Flags: syscall.RTF_HOST,
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet4Addr{IP: v4.As4()},
				},
			},
			family:     IPv4,
			wantPrefix: netip.PrefixFrom(v4, 32),
			wantOK:     true,
		},
		{
			name: "v4 route with explicit netmask",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST:     &route.Inet4Addr{IP: [4]byte{10, 0, 0, 0}},
					syscall.RTAX_NETMASK: &route.Inet4Addr{IP: [4]byte{255, 0, 0, 0}},
				},
			},
			family:     IPv4,
			wantPrefix: netip.MustParsePrefix("10.0.0.0/8"),
			wantOK:     true,
		},
		{
			name: "v6 default route with no netmask slot",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet6Addr{IP: [16]byte{}},
				},
			},
			family:     IPv6,
			wantPrefix: netip.MustParsePrefix("::/0"),
			wantOK:     true,
		},
		{
			name: "v6 host route with no netmask slot is full length",
			rm: &route.RouteMessage{
				Flags: syscall.RTF_HOST,
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet6Addr{IP: v6.As16()},
				},
			},
			family:     IPv6,
			wantPrefix: netip.PrefixFrom(v6, 128),
			wantOK:     true,
		},
		{
			name: "v6 route with explicit netmask",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST:     &route.Inet6Addr{IP: netip.MustParseAddr("2001:db8::").As16()},
					syscall.RTAX_NETMASK: &route.Inet6Addr{IP: netip.MustParseAddr("ffff:ffff:ffff:ffff::").As16()},
				},
			},
			family:     IPv6,
			wantPrefix: netip.MustParsePrefix("2001:db8::/64"),
			wantOK:     true,
		},
		{
			name: "v4 dst requested against v6 family is rejected",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet4Addr{IP: [4]byte{1, 2, 3, 4}},
				},
			},
			family: IPv6,
			wantOK: false,
		},
		{
			name: "v6 dst requested against v4 family is rejected",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.Inet6Addr{IP: v6.As16()},
				},
			},
			family: IPv4,
			wantOK: false,
		},
		{
			name: "unsupported dst address type is rejected",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: &route.LinkAddr{Name: "en0"},
				},
			},
			family: IPv4,
			wantOK: false,
		},
		{
			name: "dst slot missing entirely",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{},
			},
			family: IPv4,
			wantOK: false,
		},
		{
			name: "dst slot present but nil",
			rm: &route.RouteMessage{
				Addrs: []route.Addr{
					syscall.RTAX_DST: nil,
				},
			},
			family: IPv4,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, ok := routeMessagePrefix(tt.rm, tt.family)
			if ok != tt.wantOK {
				t.Fatalf("routeMessagePrefix() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && prefix != tt.wantPrefix {
				t.Errorf("routeMessagePrefix() = %v, want %v", prefix, tt.wantPrefix)
			}
		})
	}
}

func TestRoutesFromMessages(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) == 0 {
		t.Skip("no interfaces available to resolve against")
	}
	iface := ifaces[0]

	const missingIndex = 1 << 30 // well past any real interface count

	msgs := []route.Message{
		// Non-route messages are ignored.
		&route.InterfaceMessage{},
		// Routes with a delivery error are skipped.
		&route.RouteMessage{
			Err:   errors.New("boom"),
			Index: iface.Index,
			Addrs: []route.Addr{
				syscall.RTAX_DST: &route.Inet4Addr{IP: [4]byte{0, 0, 0, 0}},
			},
		},
		// Wrong family for the requested probe is skipped.
		&route.RouteMessage{
			Index: iface.Index,
			Addrs: []route.Addr{
				syscall.RTAX_DST: &route.Inet6Addr{IP: [16]byte{}},
			},
		},
		// Interface index that no longer resolves is skipped.
		&route.RouteMessage{
			Index: missingIndex,
			Addrs: []route.Addr{
				syscall.RTAX_DST: &route.Inet4Addr{IP: [4]byte{0, 0, 0, 0}},
			},
		},
		// A valid v4 route resolving to a real interface.
		&route.RouteMessage{
			Index: iface.Index,
			Addrs: []route.Addr{
				syscall.RTAX_DST:     &route.Inet4Addr{IP: [4]byte{10, 0, 0, 0}},
				syscall.RTAX_NETMASK: &route.Inet4Addr{IP: [4]byte{255, 0, 0, 0}},
			},
		},
	}

	got := routesFromMessages(msgs, IPv4)

	want := []Route{
		{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Interface: iface.Name, Index: iface.Index},
	}

	if len(got) != len(want) {
		t.Fatalf("routesFromMessages() = %+v, want %+v", got, want)
	}
	for i, r := range got {
		if r != want[i] {
			t.Errorf("routesFromMessages()[%d] = %+v, want %+v", i, r, want[i])
		}
	}
}
