// Package cli wires the Antaeus command without exposing CLI concerns to the
// reusable engine packages.
package cli

import (
	"fmt"
	"io"

	"github.com/antaeusio/antaeus/internal/buildinfo"
)

const usage = `Usage: antaeus <command>

Commands:
  version  Print version information
  help     Print this help
`

// Run executes the command and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return usageError(stderr, fmt.Sprintf("%s does not accept arguments", args[0]))
		}
		_, _ = io.WriteString(stdout, usage)
		return 0
	case "version", "--version":
		if len(args) != 1 {
			return usageError(stderr, fmt.Sprintf("%s does not accept arguments", args[0]))
		}
		_, _ = fmt.Fprintf(stdout, "antaeus %s\n", buildinfo.Current())
		return 0
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func usageError(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintf(stderr, "antaeus: %s\n\n%s", message, usage)
	return 64
}
