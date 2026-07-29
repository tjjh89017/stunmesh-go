package routeprobe

import (
	"errors"
	"net/netip"
	"testing"
)

// withFakeRoutes overrides currentRoutesFn for the duration of a test and
// restores it afterward, so Probe/DefaultRouteInterface can be exercised
// without real OS route-table access.
func withFakeRoutes(t *testing.T, fn func(Family) ([]Route, error)) {
	t.Helper()
	orig := currentRoutesFn
	currentRoutesFn = fn
	t.Cleanup(func() { currentRoutesFn = orig })
}

func TestProbe(t *testing.T) {
	t.Run("covering default route on a tunnel interface is detected", func(t *testing.T) {
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0"},
			}, nil
		})

		got, err := Probe(IPv4, NewTunnelInterfaces("wg0"))
		if err != nil {
			t.Fatalf("Probe() error = %v", err)
		}
		if !got {
			t.Error("Probe() = false, want true")
		}
	})

	t.Run("no covering route is reported false", func(t *testing.T) {
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "eth0"},
			}, nil
		})

		got, err := Probe(IPv4, NewTunnelInterfaces("wg0"))
		if err != nil {
			t.Fatalf("Probe() error = %v", err)
		}
		if got {
			t.Error("Probe() = true, want false")
		}
	})

	t.Run("route table read failure is propagated", func(t *testing.T) {
		wantErr := errors.New("boom")
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return nil, wantErr
		})

		_, err := Probe(IPv4, NewTunnelInterfaces("wg0"))
		if !errors.Is(err, wantErr) {
			t.Fatalf("Probe() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("requested family is forwarded to currentRoutesFn", func(t *testing.T) {
		var gotFamily Family
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			gotFamily = family
			return nil, nil
		})

		if _, err := Probe(IPv6, NewTunnelInterfaces("wg0")); err != nil {
			t.Fatalf("Probe() error = %v", err)
		}
		if gotFamily != IPv6 {
			t.Errorf("currentRoutesFn called with family = %v, want %v", gotFamily, IPv6)
		}
	})
}

func TestDefaultRouteInterface(t *testing.T) {
	t.Run("physical default route is returned, tunnel default ignored", func(t *testing.T) {
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0", Index: 5},
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "en0", Index: 4},
			}, nil
		})

		route, ok, err := DefaultRouteInterface(IPv4, NewTunnelInterfaces("wg0"))
		if err != nil {
			t.Fatalf("DefaultRouteInterface() error = %v", err)
		}
		if !ok {
			t.Fatal("DefaultRouteInterface() ok = false, want true")
		}
		want := Route{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "en0", Index: 4}
		if route != want {
			t.Errorf("DefaultRouteInterface() = %+v, want %+v", route, want)
		}
	})

	t.Run("no physical default route present is reported not ok", func(t *testing.T) {
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return []Route{
				{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Interface: "wg0", Index: 5},
			}, nil
		})

		_, ok, err := DefaultRouteInterface(IPv4, NewTunnelInterfaces("wg0"))
		if err != nil {
			t.Fatalf("DefaultRouteInterface() error = %v", err)
		}
		if ok {
			t.Error("DefaultRouteInterface() ok = true, want false")
		}
	})

	t.Run("route table read failure is propagated", func(t *testing.T) {
		wantErr := errors.New("boom")
		withFakeRoutes(t, func(family Family) ([]Route, error) {
			return nil, wantErr
		})

		_, _, err := DefaultRouteInterface(IPv4, NewTunnelInterfaces("wg0"))
		if !errors.Is(err, wantErr) {
			t.Fatalf("DefaultRouteInterface() error = %v, want %v", err, wantErr)
		}
	})
}
