package buildinfo

import "testing"

func TestInfoString(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "version and commit",
			info: Info{Version: "v0.1.0", Commit: "abc123"},
			want: "v0.1.0 (abc123)",
		},
		{
			name: "unknown commit",
			info: Info{Version: "dev", Commit: "unknown"},
			want: "dev",
		},
		{
			name: "empty commit",
			info: Info{Version: "dev"},
			want: "dev",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.info.String(); got != test.want {
				t.Fatalf("Info.String() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCurrentUsesEmbeddedValues(t *testing.T) {
	got := Current()
	if got.Version != version {
		t.Fatalf("Current().Version = %q, want %q", got.Version, version)
	}
	if got.Commit != commit {
		t.Fatalf("Current().Commit = %q, want %q", got.Commit, commit)
	}
}
