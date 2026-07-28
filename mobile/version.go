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
