package ctrl

import (
	"context"
	"errors"
	"fmt"
)

// FamilyResolver resolves the reflexive endpoint for one address family,
// formatted as host:port (IPv6 bracketed per net.JoinHostPort).
type FamilyResolver func(ctx context.Context) (string, error)

// DiscoverEndpoints resolves the reflexive endpoint(s) for an interface/
// device protocol ("ipv4", "ipv6", or "dualstack"), shared by desktop's
// raw-socket STUN resolver (PublishController) and mobile's
// mobilebind.Bind-backed resolver, via resolveIPv4/resolveIPv6.
//
// Policy: a single-family protocol hard-fails on that family's error. The
// dualstack protocol soft-fails per family — warn (if non-nil) is called for
// each family that fails, but discovery continues — and only returns an
// error if both families end up empty. This is desktop's original policy;
// mobile now shares it too, replacing its previous behavior of silently
// leaving a failed family's endpoint empty with no shared error path.
func DiscoverEndpoints(ctx context.Context, protocol string, warn func(family string, err error), resolveIPv4, resolveIPv6 FamilyResolver) (ipv4Endpoint, ipv6Endpoint string, err error) {
	var wantIPv4, wantIPv6 bool
	switch protocol {
	case "ipv4":
		wantIPv4 = true
	case "ipv6":
		wantIPv6 = true
	case "dualstack":
		wantIPv4 = true
		wantIPv6 = true
	default:
		return "", "", fmt.Errorf("unknown protocol: %s", protocol)
	}

	var ipv4Err, ipv6Err error
	if wantIPv4 {
		ipv4Endpoint, ipv4Err = resolveIPv4(ctx)
	}
	if wantIPv6 {
		ipv6Endpoint, ipv6Err = resolveIPv6(ctx)
	}

	switch protocol {
	case "ipv4":
		if ipv4Err != nil {
			return "", "", ipv4Err
		}
	case "ipv6":
		if ipv6Err != nil {
			return "", "", ipv6Err
		}
	case "dualstack":
		if ipv4Err != nil && warn != nil {
			warn("ipv4", ipv4Err)
		}
		if ipv6Err != nil && warn != nil {
			warn("ipv6", ipv6Err)
		}
		if ipv4Endpoint == "" && ipv6Endpoint == "" {
			return "", "", errors.New("both IPv4 and IPv6 STUN discovery failed")
		}
	}

	return ipv4Endpoint, ipv6Endpoint, nil
}
