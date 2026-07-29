package ctrl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

func resolverReturning(endpoint string, err error) ctrl.FamilyResolver {
	return func(ctx context.Context) (string, error) {
		return endpoint, err
	}
}

// TestDiscoverEndpoints_SharedPolicy exercises the single DiscoverEndpoints
// policy used by both PublishController.discoverEndpoints (desktop,
// raw-socket STUN) and the mobile controller's discover
// (mobilebind.Bind.Discover) since they were unified: single-family
// protocols hard-fail on that family's error, dualstack soft-fails per
// family (only warning) and hard-fails only when both are empty. This is
// the one deliberate behavior change mobile picked up — it previously used
// a silent both-empty check with no shared error path.
func TestDiscoverEndpoints_SharedPolicy(t *testing.T) {
	ipv4Err := errors.New("ipv4 boom")
	ipv6Err := errors.New("ipv6 boom")

	tests := []struct {
		name         string
		protocol     string
		ipv4         ctrl.FamilyResolver
		ipv6         ctrl.FamilyResolver
		wantIPv4     string
		wantIPv6     string
		wantErr      error
		wantWarnFams []string
	}{
		{
			name:     "ipv4 succeeds",
			protocol: "ipv4",
			ipv4:     resolverReturning("1.2.3.4:51820", nil),
			ipv6:     resolverReturning("", errors.New("must not be called")),
			wantIPv4: "1.2.3.4:51820",
		},
		{
			name:     "ipv4 hard-fails on its own error",
			protocol: "ipv4",
			ipv4:     resolverReturning("", ipv4Err),
			ipv6:     resolverReturning("", errors.New("must not be called")),
			wantErr:  ipv4Err,
		},
		{
			name:     "ipv6 hard-fails on its own error",
			protocol: "ipv6",
			ipv4:     resolverReturning("", errors.New("must not be called")),
			ipv6:     resolverReturning("", ipv6Err),
			wantErr:  ipv6Err,
		},
		{
			name:     "dualstack succeeds when both resolve",
			protocol: "dualstack",
			ipv4:     resolverReturning("1.2.3.4:51820", nil),
			ipv6:     resolverReturning("[2001:db8::1]:51820", nil),
			wantIPv4: "1.2.3.4:51820",
			wantIPv6: "[2001:db8::1]:51820",
		},
		{
			name:         "dualstack soft-fails ipv4, keeps ipv6",
			protocol:     "dualstack",
			ipv4:         resolverReturning("", ipv4Err),
			ipv6:         resolverReturning("[2001:db8::1]:51820", nil),
			wantIPv6:     "[2001:db8::1]:51820",
			wantWarnFams: []string{"ipv4"},
		},
		{
			name:         "dualstack soft-fails ipv6, keeps ipv4",
			protocol:     "dualstack",
			ipv4:         resolverReturning("1.2.3.4:51820", nil),
			ipv6:         resolverReturning("", ipv6Err),
			wantIPv4:     "1.2.3.4:51820",
			wantWarnFams: []string{"ipv6"},
		},
		{
			name:         "dualstack hard-fails only when both are empty",
			protocol:     "dualstack",
			ipv4:         resolverReturning("", ipv4Err),
			ipv6:         resolverReturning("", ipv6Err),
			wantWarnFams: []string{"ipv4", "ipv6"},
			wantErr:      errors.New("both IPv4 and IPv6 STUN discovery failed"),
		},
		{
			name:     "unknown protocol errors before resolving",
			protocol: "carrier-pigeon",
			ipv4:     resolverReturning("", errors.New("must not be called")),
			ipv6:     resolverReturning("", errors.New("must not be called")),
			wantErr:  errors.New("unknown protocol: carrier-pigeon"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warned []string
			warn := func(family string, err error) {
				warned = append(warned, family)
			}

			ipv4, ipv6, err := ctrl.DiscoverEndpoints(context.Background(), tt.protocol, warn, tt.ipv4, tt.ipv6)

			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Fatalf("DiscoverEndpoints() err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("DiscoverEndpoints() unexpected err: %v", err)
			}

			if ipv4 != tt.wantIPv4 {
				t.Errorf("ipv4 = %q, want %q", ipv4, tt.wantIPv4)
			}
			if ipv6 != tt.wantIPv6 {
				t.Errorf("ipv6 = %q, want %q", ipv6, tt.wantIPv6)
			}
			if len(warned) != len(tt.wantWarnFams) {
				t.Fatalf("warned families = %v, want %v", warned, tt.wantWarnFams)
			}
			for i, fam := range tt.wantWarnFams {
				if warned[i] != fam {
					t.Errorf("warned[%d] = %q, want %q", i, warned[i], fam)
				}
			}
		})
	}
}
