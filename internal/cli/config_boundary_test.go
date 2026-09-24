package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConfigTrustPublication(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	marker, expected, err := trustMarker(r, digest)
	if err != nil {
		t.Fatal(err)
	}
	// An interrupted preparation is not a grant and does not block a retry.
	pending := filepath.Join(filepath.Dir(marker), ".pending-approval-interrupted")
	writeConfigTestFile(t, pending, "partial")
	if trusted, err := projectTrusted(r, digest); err != nil || trusted {
		t.Fatalf("partial preparation: trusted=%v error=%v", trusted, err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			<-start
			if err := changeProjectTrust(r, digest, true); err != nil {
				t.Errorf("concurrent grant: %v", err)
			}
		})
		wg.Go(func() {
			<-start
			for j := 0; j < 100; j++ {
				// Either no grant or a complete grant is valid, never partial.
				if _, err := projectTrusted(r, digest); err != nil {
					t.Errorf("concurrent read: %v", err)
					return
				}
			}
		})
	}
	close(start)
	wg.Wait()
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != expected {
		t.Fatalf("published marker = %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(marker))
	if err != nil || len(entries) != 2 {
		t.Fatalf("want only grant and preexisting interrupted preparation: %v, %v", entries, err)
	}
}

func TestConfigExistingEscapeTargets(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(map[bool]string{false: "relative", true: "absolute"}[absolute], func(t *testing.T) {
			r := configFixture(t)
			outside := filepath.Join(t.TempDir(), "profile.json")
			copyConfigFixture(t, "evaluator-profile/quickstart-fixture.json", outside)
			// Positive control: target is a valid, readable profile outside root.
			if _, err := loadConfigLayer(outside, ""); err != nil {
				t.Fatal(err)
			}
			path := outside
			if !absolute {
				var err error
				path, err = filepath.Rel(filepath.Join(r.projectDir, ".antaeus"), outside)
				if err != nil {
					t.Fatal(err)
				}
			}
			encoded, _ := json.Marshal(map[string]string{"apiVersion": "config.antaeus.io/v0alpha1", "kind": "LocalConfiguration", "profileFile": path})
			writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), string(encoded))
			diagnostic := configCommand(t, r, 1, "inspect")
			if !strings.Contains(diagnostic, "cannot read selected profile artifact inside its configuration boundary") || strings.Contains(diagnostic, outside) {
				t.Fatal(diagnostic)
			}
			if _, err := readConfigFileWithin(outside, 1<<20, r.projectDir); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("want confinement error, not missing target: %v", err)
			}
		})
	}
}

func TestConfigIntermediateSymlinkCannotFallThrough(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside", true: "dangling"}[dangling], func(t *testing.T) {
			r := configFixture(t)
			copyConfigFixture(t, "evaluator-profile/quickstart-fixture.json", filepath.Join(r.userDir, "profile.json"))
			writeConfigTestFile(t, filepath.Join(r.userDir, "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"profile.json"}`)
			// Valid lower priority configuration must not mask the bad project.
			if err := os.Rename(filepath.Join(r.projectDir, ".antaeus"), filepath.Join(r.projectDir, "saved-config")); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			if dangling {
				// An in-root dangling target reproduces the silent ENOENT path.
				target = "missing-directory"
			} else {
				copyConfigFixture(t, "evaluator-profile/quickstart-fixture.json", filepath.Join(target, "profile.json"))
				writeConfigTestFile(t, filepath.Join(target, "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"profile.json"}`)
			}
			if err := os.Symlink(target, filepath.Join(r.projectDir, ".antaeus")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if diagnostic := configCommand(t, r, 1, "inspect"); !strings.Contains(diagnostic, "cannot read configuration manifest") {
				t.Fatal(diagnostic)
			}
		})
	}
}

func TestConfigDirectoryIdentityBoundaries(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	alias := filepath.Join(t.TempDir(), "project-alias")
	if err := os.Symlink(r.projectDir, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	aliasRuntime := r
	aliasRuntime.projectDir = alias
	configCommand(t, aliasRuntime, 0, "trust", "--digest", digest)
	if trusted, err := projectTrusted(r, digest); err != nil || !trusted {
		t.Fatalf("real path did not recognize symlink grant: %v %v", trusted, err)
	}
	r.userDir = filepath.Join(alias, "not-created", "user")
	if diagnostic := configCommand(t, r, 1, "inspect"); !strings.Contains(diagnostic, "user configuration directory must be outside the project") {
		t.Fatal(diagnostic)
	}
	// Trust can itself be redirected into the project independently of userDir.
	r.userDir = t.TempDir()
	if err := os.Symlink(r.projectDir, filepath.Join(r.userDir, "trust")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trustMarker(r, digest); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("nested trust storage accepted: %v", err)
	}
}

func TestConfigCaseAliasedContainment(t *testing.T) {
	r := configFixture(t)
	project := filepath.Join(r.projectDir, "MixedCaseProject")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(r.projectDir, "mixedcaseproject")
	base, err := os.Stat(project)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(alias)
	if err != nil || !os.SameFile(base, other) {
		t.Skip("filesystem does not resolve this case alias")
	}
	r.projectDir = project
	r.userDir = filepath.Join(alias, "not-created", "user")
	if err := independentUserConfig(r); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("case alias containment accepted: %v", err)
	}
	r.userDir = t.TempDir()
	if err := os.Symlink(alias, filepath.Join(r.userDir, "trust")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := trustMarker(r, "sha256:"+strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("case alias trust containment accepted: %v", err)
	}
}

func TestConfigDanglingUnconfinedAncestor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "directory", "config.json")
	// No directory is genuinely an absent manifest.
	if layer, err := loadConfigManifest(path, ""); err != nil || layer.Digest() != "" {
		t.Fatalf("absent manifest: %v", err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "directory")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readConfigFile(path, 16<<10); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dangling ancestor treated as absent: %v", err)
	}
	if _, err := loadConfigManifest(path, ""); err == nil || !strings.Contains(err.Error(), "cannot read configuration manifest") {
		t.Fatalf("malformed user manifest fell through: %v", err)
	}
}
