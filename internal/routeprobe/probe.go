package routeprobe

// TunnelInterfaces is a set of interface names the caller treats as
// WireGuard tunnels. Detection is limited to these names: stunmesh only
// knows about the devices it manages (the config's `interfaces` keys), so a
// WireGuard interface stunmesh does not manage is never reported as
// covering, even if it happens to install a covering default route.
type TunnelInterfaces map[string]struct{}

// Contains reports whether name is a known tunnel interface.
func (t TunnelInterfaces) Contains(name string) bool {
	_, ok := t[name]
	return ok
}

// NewTunnelInterfaces builds a TunnelInterfaces set from interface names.
func NewTunnelInterfaces(names ...string) TunnelInterfaces {
	t := make(TunnelInterfaces, len(names))
	for _, n := range names {
		t[n] = struct{}{}
	}
	return t
}

// currentRoutesFn indirects to the per-OS currentRoutes implementation.
// Probe and DefaultRouteInterface call through it rather than currentRoutes
// directly so tests can fake the route table instead of needing real OS
// route-table access; see probe_test.go.
var currentRoutesFn = currentRoutes

// Probe reports whether any interface in tunnelIfaces currently installs a
// covering default route (see package doc) for family. Detection failure
// (e.g. the route table could not be read) is returned as an error and is
// distinct from "no covering default found"; callers should treat an error
// the same as false (i.e. do not assume escape is needed) while logging it.
func Probe(family Family, tunnelIfaces TunnelInterfaces) (bool, error) {
	routes, err := currentRoutesFn(family)
	if err != nil {
		return false, err
	}

	return HasCoveringDefault(routes, family, tunnelIfaces.Contains), nil
}

// DefaultRouteInterface returns the interface carrying the current physical
// (non-tunnel) default route for family — see chooseDefaultInterface for the
// selection rule. ok is false when no such route exists; err is non-nil only
// when the route table itself could not be read (same distinction as Probe).
func DefaultRouteInterface(family Family, tunnelIfaces TunnelInterfaces) (Route, bool, error) {
	routes, err := currentRoutesFn(family)
	if err != nil {
		return Route{}, false, err
	}

	route, ok := chooseDefaultInterface(routes, family, tunnelIfaces.Contains)
	return route, ok, nil
}
