package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "no arguments shows help",
			wantCode:   0,
			wantStdout: "Usage: antaeus <command>",
		},
		{
			name:       "help command",
			args:       []string{"help"},
			wantCode:   0,
			wantStdout: "Commands:",
		},
		{
			name:       "version command",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: "antaeus dev",
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: "antaeus dev",
		},
		{
			name:       "unknown command",
			args:       []string{"unknown"},
			wantCode:   64,
			wantStderr: `unknown command "unknown"`,
		},
		{
			name:       "unexpected argument",
			args:       []string{"version", "extra"},
			wantCode:   64,
			wantStderr: "version does not accept arguments",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if got := Run(test.args, &stdout, &stderr); got != test.wantCode {
				t.Fatalf("Run() = %d, want %d", got, test.wantCode)
			}
			if !strings.Contains(stdout.String(), test.wantStdout) {
				t.Errorf("stdout = %q, want substring %q", stdout.String(), test.wantStdout)
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), test.wantStderr)
			}
		})
	}
}
