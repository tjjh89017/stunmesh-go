package ctrl_test

import (
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func TestSelectEndpoint(t *testing.T) {
	both := ctrl.EndpointData{IPv4: "1.2.3.4:51820", IPv6: "[2001:db8::1]:51820"}
	ipv4Only := ctrl.EndpointData{IPv4: "1.2.3.4:51820"}
	ipv6Only := ctrl.EndpointData{IPv6: "[2001:db8::1]:51820"}
	empty := ctrl.EndpointData{}

	localBoth := &entity.DeviceStatus{IPv4: "9.9.9.9:1", IPv6: "[fe80::1]:1"}
	localIPv4Only := &entity.DeviceStatus{IPv4: "9.9.9.9:1"}
	localIPv6Only := &entity.DeviceStatus{IPv6: "[fe80::1]:1"}
	localNeither := &entity.DeviceStatus{}

	tests := []struct {
		name     string
		data     ctrl.EndpointData
		protocol string
		local    *entity.DeviceStatus
		want     string
		wantErr  bool
	}{
		{"ipv4 selects ipv4 when both present", both, "ipv4", nil, "1.2.3.4:51820", false},
		{"ipv4 errors when ipv4 absent", ipv6Only, "ipv4", nil, "", true},
		{"empty protocol defaults to ipv4", both, "", nil, "1.2.3.4:51820", false},
		{"ipv6 selects ipv6 when both present", both, "ipv6", nil, "[2001:db8::1]:51820", false},
		{"ipv6 errors when ipv6 absent", ipv4Only, "ipv6", nil, "", true},

		{"prefer_ipv4 selects ipv4 when both present, local nil", both, "prefer_ipv4", nil, "1.2.3.4:51820", false},
		{"prefer_ipv4 falls back to ipv6, local nil", ipv6Only, "prefer_ipv4", nil, "[2001:db8::1]:51820", false},
		{"prefer_ipv4 errors when both absent, local nil", empty, "prefer_ipv4", nil, "", true},

		{"prefer_ipv6 selects ipv6 when both present, local nil", both, "prefer_ipv6", nil, "[2001:db8::1]:51820", false},
		{"prefer_ipv6 falls back to ipv4, local nil", ipv4Only, "prefer_ipv6", nil, "1.2.3.4:51820", false},
		{"prefer_ipv6 errors when both absent, local nil", empty, "prefer_ipv6", nil, "", true},

		{"unknown protocol errors", both, "carrier-pigeon", nil, "", true},

		// local-aware prefer_ipv6
		{"prefer_ipv6, local has both, selects ipv6", both, "prefer_ipv6", localBoth, "[2001:db8::1]:51820", false},
		{"prefer_ipv6, local lacks ipv6, falls back to ipv4", both, "prefer_ipv6", localIPv4Only, "1.2.3.4:51820", false},
		{"prefer_ipv6, local lacks both, errors", both, "prefer_ipv6", localNeither, "", true},
		{"prefer_ipv6, local has only ipv6, selects ipv6", both, "prefer_ipv6", localIPv6Only, "[2001:db8::1]:51820", false},

		// local-aware prefer_ipv4 (mirror)
		{"prefer_ipv4, local has both, selects ipv4", both, "prefer_ipv4", localBoth, "1.2.3.4:51820", false},
		{"prefer_ipv4, local lacks ipv4, falls back to ipv6", both, "prefer_ipv4", localIPv6Only, "[2001:db8::1]:51820", false},
		{"prefer_ipv4, local lacks both, errors", both, "prefer_ipv4", localNeither, "", true},
		{"prefer_ipv4, local has only ipv4, selects ipv4", both, "prefer_ipv4", localIPv4Only, "1.2.3.4:51820", false},

		// hard modes ignore local entirely
		{"ipv4 ignores local lacking ipv4", both, "ipv4", localIPv6Only, "1.2.3.4:51820", false},
		{"ipv6 ignores local lacking ipv6", both, "ipv6", localIPv4Only, "[2001:db8::1]:51820", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ctrl.SelectEndpoint(tt.data, tt.protocol, tt.local)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SelectEndpoint(%+v, %q, %+v) = %q, nil; want error", tt.data, tt.protocol, tt.local, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectEndpoint(%+v, %q, %+v) returned unexpected error: %v", tt.data, tt.protocol, tt.local, err)
			}
			if got != tt.want {
				t.Errorf("SelectEndpoint(%+v, %q, %+v) = %q, want %q", tt.data, tt.protocol, tt.local, got, tt.want)
			}
		})
	}
}
