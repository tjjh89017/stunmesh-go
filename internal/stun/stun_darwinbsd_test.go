//go:build darwin || freebsd

package stun

import (
	"errors"
	"net"
	"reflect"
	"sort"
	"testing"

	"github.com/rs/zerolog"
)

// ifaceList builds a fake net.Interfaces() returning the named interfaces, all
// up and non-loopback unless the name is "lo0".
func ifaceList(names ...string) func() ([]net.Interface, error) {
	return func() ([]net.Interface, error) {
		out := make([]net.Interface, 0, len(names))
		for i, n := range names {
			flags := net.FlagUp
			if n == "lo0" {
				flags = net.FlagUp | net.FlagLoopback
			}
			out = append(out, net.Interface{Index: i + 1, Name: n, Flags: flags})
		}
		return out, nil
	}
}

func constRoute(name string, err error) func(string) (string, error) {
	return func(string) (string, error) { return name, err }
}

func TestResolveListenInterfaces(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name         string
		protocol     string
		exclude      string
		listen       []string
		defaultRoute bool
		ifaces       func() ([]net.Interface, error)
		route        func(string) (string, error)
		wantNames    []string // order-insensitive
		wantRequired []string
		wantErr      bool
	}{
		{
			name:      "no selector opens all eligible, minus loopback and wg",
			exclude:   "wg0",
			ifaces:    ifaceList("em0", "em1", "lo0", "wg0"),
			route:     constRoute("", nil),
			wantNames: []string{"em0", "em1"},
		},
		{
			name:    "no selector, nothing eligible is an error",
			exclude: "wg0",
			ifaces:  ifaceList("lo0", "wg0"),
			route:   constRoute("", nil),
			wantErr: true,
		},
		{
			name:         "explicit list selects only those, marked required",
			exclude:      "wg0",
			listen:       []string{"em0"},
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "unknown interface name is skipped, not fatal",
			exclude:      "wg0",
			listen:       []string{"em0", "typo0"},
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:    "explicit list of only unknowns resolves to none is an error",
			exclude: "wg0",
			listen:  []string{"typo0"},
			ifaces:  ifaceList("em0", "wg0"),
			route:   constRoute("", nil),
			wantErr: true,
		},
		{
			name:      "naming the wg interface itself is skipped",
			exclude:   "wg0",
			listen:    []string{"wg0", "em0"},
			ifaces:    ifaceList("em0", "wg0"),
			route:     constRoute("", nil),
			wantNames: []string{"em0"},
		},
		{
			name:         "default route interface is added (best effort, not required)",
			exclude:      "wg0",
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em1", nil),
			wantNames:    []string{"em1"},
			wantRequired: nil,
		},
		{
			name:         "union of explicit list and default route, deduped",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em0", nil), // same as explicit -> dedup
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "union keeps both when default route differs",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "em1", "wg0"),
			route:        constRoute("em1", nil),
			wantNames:    []string{"em0", "em1"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "default route missing for protocol resolves to none is an error",
			exclude:      "wg0",
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", nil), // e.g. no IPv6 default route
			wantErr:      true,
		},
		{
			name:         "default route lookup error is skipped, not fatal, when list carries",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("", errors.New("route table read failed")),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
		{
			name:         "default route pointing at wg interface is skipped",
			exclude:      "wg0",
			listen:       []string{"em0"},
			defaultRoute: true,
			ifaces:       ifaceList("em0", "wg0"),
			route:        constRoute("wg0", nil),
			wantNames:    []string{"em0"},
			wantRequired: []string{"em0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, required, err := resolveListenInterfaces(&logger, tt.protocol, tt.exclude, tt.listen, tt.defaultRoute, tt.ifaces, tt.route)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got names=%v", names)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotNames := append([]string(nil), names...)
			wantNames := append([]string(nil), tt.wantNames...)
			sort.Strings(gotNames)
			sort.Strings(wantNames)
			if !reflect.DeepEqual(gotNames, wantNames) {
				t.Errorf("names = %v, want %v", names, tt.wantNames)
			}

			var gotRequired []string
			for k := range required {
				gotRequired = append(gotRequired, k)
			}
			wantRequired := append([]string(nil), tt.wantRequired...)
			sort.Strings(gotRequired)
			sort.Strings(wantRequired)
			if !reflect.DeepEqual(gotRequired, wantRequired) {
				t.Errorf("required = %v, want %v", gotRequired, tt.wantRequired)
			}
		})
	}
}

// TestResolveListenInterfaces_InterfaceListError surfaces an enumeration
// failure rather than treating it as "no interfaces".
func TestResolveListenInterfaces_InterfaceListError(t *testing.T) {
	logger := zerolog.Nop()
	boom := errors.New("cannot list interfaces")
	failing := func() ([]net.Interface, error) { return nil, boom }

	_, _, err := resolveListenInterfaces(&logger, "ipv4", "wg0", []string{"em0"}, false, failing, constRoute("", nil))
	if !errors.Is(err, boom) {
		t.Fatalf("expected the enumeration error, got %v", err)
	}
}

// TestDefaultRouteInterface_Smoke exercises the real x/net/route parsing path on
// the platform it targets (it only runs where the build tag lets it compile).
// It cannot assert a specific interface -- the CI VM's routing table is not
// fixed -- but it pins that the RIB dump parses without error and that any name
// returned is a real interface.
func TestDefaultRouteInterface_Smoke(t *testing.T) {
	for _, protocol := range []string{"ipv4", "ipv6"} {
		name, err := defaultRouteInterface(protocol)
		if err != nil {
			t.Fatalf("defaultRouteInterface(%q): %v", protocol, err)
		}
		if name == "" {
			continue // no default route for this family is legitimate
		}
		if _, err := net.InterfaceByName(name); err != nil {
			t.Errorf("defaultRouteInterface(%q) returned %q which is not a real interface: %v", protocol, name, err)
		}
	}
}
