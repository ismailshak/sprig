// Package build reports the running binary's version, VCS revision, and Go
// toolchain.
package build

import (
	"runtime/debug"
	"strings"
)

// Info is the running binary's version, revision and toolchain. go build
// embeds these automatically when building inside a git working tree.
type Info struct {
	Version   string
	Revision  string
	GoVersion string
}

// Read returns Info from the binary's embedded build metadata. Revision is
// empty when the binary was built without VCS stamping, as under go run or go
// test.
func Read() Info {
	info := Info{Version: "(unknown)"}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	// The image's build context is an allowlist, so git in the build stage
	// sees the tracked files left out as deletions and marks every build
	// dirty. The suffix would be on every release and means nothing.
	info.Version = strings.TrimSuffix(buildInfo.Main.Version, "+dirty")
	info.GoVersion = buildInfo.GoVersion

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			info.Revision = setting.Value
		}
	}

	return info
}
