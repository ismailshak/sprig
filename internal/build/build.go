// Package build reports the running binary's version, VCS revision, and Go
// toolchain.
package build

import "runtime/debug"

// Info describes the running binary. go build embeds this automatically
// inside a git working tree.
type Info struct {
	Version   string
	Revision  string
	Dirty     bool
	GoVersion string
}

// Read extracts Info from the binary's embedded build metadata. Revision
// and Dirty are empty when the binary wasn't built with VCS stamping, such
// as under `go run` or `go test`.
func Read() Info {
	info := Info{Version: "(unknown)"}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	info.Version = buildInfo.Main.Version
	info.GoVersion = buildInfo.GoVersion

	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Revision = setting.Value
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}

	return info
}
