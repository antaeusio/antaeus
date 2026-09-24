// Package buildinfo exposes version metadata to the Antaeus command.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

var (
	version = "dev"
	commit  = "unknown"
)

// Info describes the source version used to build the command.
type Info struct {
	Version string
	Commit  string
}

// Current returns the metadata embedded in the running command. Builds
// without injected metadata, such as go install of a tagged module version,
// fall back to the module version recorded by the Go toolchain.
func Current() Info {
	moduleVersion := ""
	if build, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = build.Main.Version
	}
	return current(version, commit, moduleVersion)
}

func current(version, commit, moduleVersion string) Info {
	if version == "dev" && moduleVersion != "" && moduleVersion != "(devel)" {
		version = moduleVersion
	}
	return Info{Version: version, Commit: commit}
}

// String returns a stable, human-readable version summary.
func (i Info) String() string {
	if i.Commit == "" || i.Commit == "unknown" {
		return i.Version
	}
	return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
}
