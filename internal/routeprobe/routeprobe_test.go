package routeprobe

import (
	"net/netip"
	"testing"
)

func isWG(names ...string) func(string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) bool {
		return set[name]
	}
}

func TestHasCoveringDefault(t *testing.T) {
	tests := []struct {
		name   string
		routes []Route
		family Family
		isWG   func(string) bool
		want   bool
	}{
		{
			name: "v4 default route on wg interface",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   true,
		},
		{
			name: "v4 default route on non-wg interface is not flagged",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "eth0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "v4 half pair both present on wg interface",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Interface: "wg0"},
				{Prefix: netip.MustParsePrefix("128.0.0.0/1"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   true,
		},
		{
			name: "v4 half pair split across two wg interfaces still covers",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Interface: "wg0"},
				{Prefix: netip.MustParsePrefix("128.0.0.0/1"), Interface: "wg1"},
			},
			family: IPv4,
			isWG:   isWG("wg0", "wg1"),
			want:   true,
		},
		{
			name: "v4 only one half present does not cover",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "v4 half pair present but one half on non-wg interface",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Interface: "wg0"},
				{Prefix: netip.MustParsePrefix("128.0.0.0/1"), Interface: "eth0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "v4 unrelated narrow route on wg interface does not cover",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "v6 default route on wg interface",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("::/0"), Interface: "wg0"},
			},
			family: IPv6,
			isWG:   isWG("wg0"),
			want:   true,
		},
		{
			name: "v6 half pair both present on wg interface",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("::/1"), Interface: "wg0"},
				{Prefix: netip.MustParsePrefix("8000::/1"), Interface: "wg0"},
			},
			family: IPv6,
			isWG:   isWG("wg0"),
			want:   true,
		},
		{
			name: "v6 only one half present does not cover",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("8000::/1"), Interface: "wg0"},
			},
			family: IPv6,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "family split: v4 default present does not satisfy v6 query",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0"},
			},
			family: IPv6,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "family split: v6 default present does not satisfy v4 query",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("::/0"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name:   "no routes",
			routes: nil,
			family: IPv4,
			isWG:   isWG("wg0"),
			want:   false,
		},
		{
			name: "nil isTunnel predicate never matches",
			routes: []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0"},
			},
			family: IPv4,
			isWG:   nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCoveringDefault(tt.routes, tt.family, tt.isWG)
			if got != tt.want {
				t.Errorf("HasCoveringDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}
