package ctrl

import (
	"fmt"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

// SelectEndpoint applies the peer protocol preference to a decrypted
// EndpointData record: "ipv4"/"ipv6" require that family and error when it
// is absent, "prefer_ipv4"/"prefer_ipv6" fall back to the other family, and
// an empty protocol defaults to "ipv4" (config loading already applies this
// default before an entity.Peer is constructed, so desktop callers never
// actually pass "" here; mobile's config leaves the field optional, so its
// controller can).
//
// local is the local host's own last STUN discovery result (see
// entity.DeviceStatus), used only by "prefer_ipv4"/"prefer_ipv6" to skip a
// family the local host has no address for. local == nil means the local
// capability is unknown, in which case the result matches pure protocol
// preference exactly as before this parameter existed.
//
// Shared by EstablishController.Execute and the mobile controller so the
// two callers can never drift on this rule again.
func SelectEndpoint(data EndpointData, protocol string, local *entity.DeviceStatus) (string, error) {
	usable := func(candidate string, localHas func(entity.DeviceStatus) bool) bool {
		if candidate == "" {
			return false
		}
		return local == nil || localHas(*local)
	}
	hasIPv4 := func(s entity.DeviceStatus) bool { return s.IPv4 != "" }
	hasIPv6 := func(s entity.DeviceStatus) bool { return s.IPv6 != "" }

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
		if data.IPv4 == "" && data.IPv6 == "" {
			return "", fmt.Errorf("record has no endpoints")
		}
		if usable(data.IPv4, hasIPv4) {
			return data.IPv4, nil
		}
		if usable(data.IPv6, hasIPv6) {
			return data.IPv6, nil
		}
		return "", fmt.Errorf("no endpoint reachable from the local address families")
	case "prefer_ipv6":
		if data.IPv4 == "" && data.IPv6 == "" {
			return "", fmt.Errorf("record has no endpoints")
		}
		if usable(data.IPv6, hasIPv6) {
			return data.IPv6, nil
		}
		if usable(data.IPv4, hasIPv4) {
			return data.IPv4, nil
		}
		return "", fmt.Errorf("no endpoint reachable from the local address families")
	default:
		return "", fmt.Errorf("unknown peer protocol %q", protocol)
	}
}
