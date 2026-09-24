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

func TestCurrentPrefersInjectedMetadata(t *testing.T) {
	tests := []struct {
		name, version, commit, module string
		want                          Info
	}{
		{"injected release", "v0.1.0", "abc123", "v0.1.0", Info{"v0.1.0", "abc123"}},
		{"injected wins over module", "v0.2.0-rc.1", "abc123", "v0.1.0", Info{"v0.2.0-rc.1", "abc123"}},
		{"go install of a tag", "dev", "unknown", "v0.1.0", Info{"v0.1.0", "unknown"}},
		{"local checkout", "dev", "unknown", "(devel)", Info{"dev", "unknown"}},
		{"no build info", "dev", "unknown", "", Info{"dev", "unknown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := current(test.version, test.commit, test.module); got != test.want {
				t.Fatalf("current() = %+v, want %+v", got, test.want)
			}
		})
	}
}
