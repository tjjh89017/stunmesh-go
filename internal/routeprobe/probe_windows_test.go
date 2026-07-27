//go:build windows

package routeprobe

import (
	"net/netip"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// prefix4 builds an IpAddressPrefix carrying an AF_INET RawSockaddrInet4,
// overlaid onto the union field the same way forwardPrefix reads it back.
func prefix4(addr [4]byte, bits int) windows.IpAddressPrefix {
	v4 := windows.RawSockaddrInet4{Family: windows.AF_INET, Addr: addr}
	var p windows.IpAddressPrefix
	*(*windows.RawSockaddrInet4)(unsafe.Pointer(&p.Prefix)) = v4
	p.PrefixLength = uint8(bits)
	return p
}

// prefix6 is prefix4's IPv6 counterpart.
func prefix6(addr [16]byte, bits int) windows.IpAddressPrefix {
	v6 := windows.RawSockaddrInet6{Family: windows.AF_INET6, Addr: addr}
	var p windows.IpAddressPrefix
	*(*windows.RawSockaddrInet6)(unsafe.Pointer(&p.Prefix)) = v6
	p.PrefixLength = uint8(bits)
	return p
}

func TestForwardPrefix(t *testing.T) {
	v4 := netip.MustParseAddr("192.168.1.0")
	v6 := netip.MustParseAddr("2001:db8::")

	tests := []struct {
		name       string
		p          windows.IpAddressPrefix
		family     Family
		wantPrefix netip.Prefix
		wantOK     bool
	}{
		{
			name:       "v4 row matching v4 family",
			p:          prefix4(v4.As4(), 24),
			family:     IPv4,
			wantPrefix: netip.PrefixFrom(v4, 24),
			wantOK:     true,
		},
		{
			name:       "v4 default route",
			p:          prefix4([4]byte{0, 0, 0, 0}, 0),
			family:     IPv4,
			wantPrefix: netip.MustParsePrefix("0.0.0.0/0"),
			wantOK:     true,
		},
		{
			name:       "v4 host route",
			p:          prefix4(v4.As4(), 32),
			family:     IPv4,
			wantPrefix: netip.PrefixFrom(v4, 32),
			wantOK:     true,
		},
		{
			name:   "v4 row requested against v6 family is rejected",
			p:      prefix4(v4.As4(), 24),
			family: IPv6,
			wantOK: false,
		},
		{
			name:       "v6 row matching v6 family",
			p:          prefix6(v6.As16(), 64),
			family:     IPv6,
			wantPrefix: netip.PrefixFrom(v6, 64),
			wantOK:     true,
		},
		{
			name:       "v6 default route",
			p:          prefix6([16]byte{}, 0),
			family:     IPv6,
			wantPrefix: netip.MustParsePrefix("::/0"),
			wantOK:     true,
		},
		{
			name:       "v6 host route",
			p:          prefix6(v6.As16(), 128),
			family:     IPv6,
			wantPrefix: netip.PrefixFrom(v6, 128),
			wantOK:     true,
		},
		{
			name:   "v6 row requested against v4 family is rejected",
			p:      prefix6(v6.As16(), 64),
			family: IPv4,
			wantOK: false,
		},
		{
			name: "unknown family tag is rejected",
			p: windows.IpAddressPrefix{
				Prefix:       windows.RawSockaddrInet{Family: windows.AF_UNSPEC},
				PrefixLength: 0,
			},
			family: IPv4,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, ok := forwardPrefix(tt.p, tt.family)
			if ok != tt.wantOK {
				t.Fatalf("forwardPrefix() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && prefix != tt.wantPrefix {
				t.Errorf("forwardPrefix() = %v, want %v", prefix, tt.wantPrefix)
			}
		})
	}
}
