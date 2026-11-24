//go:build mobile

package mobile

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestWireGuardVersionHelper(t *testing.T) {
	other := &debug.Module{Path: "golang.org/x/sys", Version: "v0.30.0"}
	wgctrl := &debug.Module{Path: wireGuardModule + "/wgctrl", Version: "v0.0.0-20241231184526-a9ab2273dd10"}
	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"nil build info", nil, "unknown"},
		{"missing", &debug.BuildInfo{Deps: []*debug.Module{other, wgctrl}}, "unknown"},
		{"found", &debug.BuildInfo{Deps: []*debug.Module{
			other,
			wgctrl,
			{Path: wireGuardModule, Version: "v0.0.0-20250521234502-f333402bd9cb"},
		}}, "v0.0.0-20250521234502-f333402bd9cb"},
		{"versioned replace", &debug.BuildInfo{Deps: []*debug.Module{{
			Path:    wireGuardModule,
			Version: "v0.0.0-20250521234502-f333402bd9cb",
			Replace: &debug.Module{Path: "example.com/wireguard-fork", Version: "v0.0.1"},
		}}}, "v0.0.1"},
		{"local path replace", &debug.BuildInfo{Deps: []*debug.Module{{
			Path:    wireGuardModule,
			Version: "v0.0.0-20250521234502-f333402bd9cb",
			Replace: &debug.Module{Path: "../wireguard-go"},
		}}}, "v0.0.0-20250521234502-f333402bd9cb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wireGuardVersion(tc.info); got != tc.want {
				t.Fatalf("wireGuardVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The test binary links wireguard-go through this package, so its build info
// lists the module. The version itself is not pinned here: dependency bumps
// change it.
func TestWireGuardVersionFromBuild(t *testing.T) {
	got := WireGuardVersion()
	if got == "unknown" || !strings.HasPrefix(got, "v") {
		t.Fatalf("WireGuardVersion() = %q, want a module version", got)
	}
}
