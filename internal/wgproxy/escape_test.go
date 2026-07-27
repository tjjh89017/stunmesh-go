package wgproxy

import (
	"errors"
	"testing"
)

func TestShouldEscape(t *testing.T) {
	probeErr := errors.New("route probe failed")

	tests := []struct {
		name         string
		covering     bool
		probeErr     error
		firewallMark int
		want         bool
	}{
		{name: "covering default with fwmark", covering: true, firewallMark: 0xca6c, want: true},
		{name: "no covering default", covering: false, firewallMark: 0xca6c, want: false},
		{name: "covering default but no fwmark", covering: true, firewallMark: 0, want: false},
		{name: "probe error is treated as no covering default", covering: true, probeErr: probeErr, firewallMark: 0xca6c, want: false},
		{name: "probe error and no fwmark", covering: false, probeErr: probeErr, firewallMark: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEscape(tt.covering, tt.probeErr, tt.firewallMark)
			if got != tt.want {
				t.Errorf("shouldEscape(%v, %v, %d) = %v, want %v", tt.covering, tt.probeErr, tt.firewallMark, got, tt.want)
			}
		})
	}
}

func TestRouteprobeFamily(t *testing.T) {
	if routeprobeFamily(FamilyIPv4) != 0 {
		t.Errorf("FamilyIPv4 should map to routeprobe.IPv4 (0)")
	}
	if routeprobeFamily(FamilyIPv6) != 1 {
		t.Errorf("FamilyIPv6 should map to routeprobe.IPv6 (1)")
	}
}
