package ctrl

import "fmt"

// SelectEndpoint applies the peer protocol preference to a decrypted
// EndpointData record: "ipv4"/"ipv6" require that family and error when it
// is absent, "prefer_ipv4"/"prefer_ipv6" fall back to the other family, and
// an empty protocol defaults to "ipv4" (config loading already applies this
// default before an entity.Peer is constructed, so desktop callers never
// actually pass "" here; mobile's config leaves the field optional, so its
// controller can).
//
// Shared by EstablishController.Execute and the mobile controller so the
// two callers can never drift on this rule again.
func SelectEndpoint(data EndpointData, protocol string) (string, error) {
	switch protocol {
	case "", "ipv4":
		if data.IPv4 == "" {
			return "", fmt.Errorf("no ipv4 endpoint in record")
		}
		return data.IPv4, nil
	case "ipv6":
		if data.IPv6 == "" {
			return "", fmt.Errorf("no ipv6 endpoint in record")
		}
		return data.IPv6, nil
	case "prefer_ipv4":
		if data.IPv4 != "" {
			return data.IPv4, nil
		}
		if data.IPv6 != "" {
			return data.IPv6, nil
		}
		return "", fmt.Errorf("record has no endpoints")
	case "prefer_ipv6":
		if data.IPv6 != "" {
			return data.IPv6, nil
		}
		if data.IPv4 != "" {
			return data.IPv4, nil
		}
		return "", fmt.Errorf("record has no endpoints")
	default:
		return "", fmt.Errorf("unknown peer protocol %q", protocol)
	}
}
