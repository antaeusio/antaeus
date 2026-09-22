// Package buildinfo exposes version metadata to the Antaeus command.
package buildinfo

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
)

// Info describes the source version used to build the command.
type Info struct {
	Version string
	Commit  string
}

// Current returns the metadata embedded in the running command.
func Current() Info {
	return Info{
		Version: version,
		Commit:  commit,
	}
}

// String returns a stable, human-readable version summary.
func (i Info) String() string {
	if i.Commit == "" || i.Commit == "unknown" {
		return i.Version
	}
	return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
}
