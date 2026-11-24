//go:build mobile

package mobile

import "runtime/debug"

// version is stamped with -ldflags "-X ...mobile.version=<tag>" by the mobile
// make target. gomobile builds a library rather than a module main, so the
// build info carries no usable Main.Version to fall back on the way the
// desktop binary does; the VCS revision is the next best thing.
var version string

// Version reports the STUNMESH core build the app is linked against, for an
// about screen or a bug report.
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}

const wireGuardModule = "golang.zx2c4.com/wireguard"

// WireGuardVersion reports the wireguard-go module version the app is linked
// against, as Go writes it (for example "v0.0.0-20250521234502-f333402bd9cb").
// It returns "unknown" when the build carries no module information.
func WireGuardVersion() string {
	info, _ := debug.ReadBuildInfo()
	return wireGuardVersion(info)
}

func wireGuardVersion(info *debug.BuildInfo) string {
	if info == nil {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path != wireGuardModule {
			continue
		}
		// A local-path replace has no version; the required one is still the
		// closest description of the code.
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Version != "" {
			return dep.Version
		}
		break
	}
	return "unknown"
}
