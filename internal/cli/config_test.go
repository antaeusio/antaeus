package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/evaluator/localbinding"
)

func configFixture(t *testing.T) configRuntime {
	t.Helper()
	r := configRuntime{projectDir: t.TempDir(), userDir: t.TempDir()}
	copyConfigFixture(t, "evaluator-profile/semantic-routing.json", filepath.Join(r.projectDir, "profile.json"))
	copyConfigFixture(t, "local-secret-bindings/development.json", filepath.Join(r.projectDir, "bindings.json"))
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json","bindingsFile":"../bindings.json"}`)
	r.environment = localbinding.EnvironmentFunc(func(string) (string, bool) { t.Fatal("unexpected environment lookup"); return "", false })
	return r
}

func copyConfigFixture(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "contracts", "examples", "v0alpha1", source))
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, target, string(data))
}

func writeConfigTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func configCommand(t *testing.T, r configRuntime, want int, args ...string) string {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := runConfigWith(args, &out, &diagnostic, r)
	if code != want {
		t.Fatalf("%v: exit %d, want %d; output %s; error %s", args, code, want, out.String(), diagnostic.String())
	}
	if code != 0 {
		if out.Len() != 0 {
			t.Fatalf("error wrote stdout: %s", out.String())
		}
		return diagnostic.String()
	}
	if diagnostic.Len() != 0 {
		t.Fatalf("success wrote stderr: %s", diagnostic.String())
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("invalid JSON: %s", out.String())
	}
	return out.String()
}

func projectTestDigest(t *testing.T, r configRuntime) string {
	t.Helper()
	l, err := loadConfigManifest(filepath.Join(r.projectDir, ".antaeus", "config.json"), r.projectDir)
	if err != nil {
		t.Fatal(err)
	}
	return l.Digest()
}

func TestConfigTrustLifecycle(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	inspect := configCommand(t, r, 0, "inspect")
	if !strings.Contains(inspect, `"projectTrusted":false`) || !strings.Contains(inspect, "requires-project-trust") || !strings.Contains(inspect, digest) {
		t.Fatal(inspect)
	}
	if strings.Contains(inspect, "EXAMPLE_API_KEY") || strings.Contains(inspect, r.projectDir) {
		t.Fatal("inspection exposed reference name or path")
	}
	diagnostic := configCommand(t, r, 1, "check")
	if !strings.Contains(diagnostic, "antaeus config trust --digest "+digest) {
		t.Fatal(diagnostic)
	}
	configCommand(t, r, 1, "trust", "--digest", "sha256:"+strings.Repeat("0", 64))
	configCommand(t, r, 0, "trust", "--digest", digest)
	configCommand(t, r, 0, "trust", "--digest", digest) // idempotent
	inspect = configCommand(t, r, 0, "inspect")
	if !strings.Contains(inspect, `"projectTrusted":true`) {
		t.Fatal(inspect)
	}
	reads := 0
	r.environment = localbinding.EnvironmentFunc(func(name string) (string, bool) {
		reads++
		if name != "EXAMPLE_API_KEY" {
			t.Fatalf("unexpected lookup %q", name)
		}
		return "DO_NOT_PRINT_THIS_SECRET", true
	})
	checked := configCommand(t, r, 0, "check")
	if reads != 1 || !strings.Contains(checked, `"credentialsChecked":true`) || strings.Contains(checked, "DO_NOT_PRINT") {
		t.Fatalf("reads=%d output=%s", reads, checked)
	}
	configCommand(t, r, 0, "revoke", "--digest", digest)
	configCommand(t, r, 0, "revoke", "--digest", digest)
	configCommand(t, r, 1, "check")
	if reads != 1 {
		t.Fatal("revoked project read credentials")
	}
}

func TestConfigEditsAndProjectMovesInvalidateTrust(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	configCommand(t, r, 0, "trust", "--digest", digest)
	data, err := os.ReadFile(filepath.Join(r.projectDir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, filepath.Join(r.projectDir, "profile.json"), strings.Replace(string(data), "semantic-routing", "edited-profile", 1))
	if projectTestDigest(t, r) == digest {
		t.Fatal("profile edit preserved digest")
	}
	configCommand(t, r, 1, "check")
	configCommand(t, r, 1, "trust", "--digest", digest)
	// Revocation uses the supplied old digest, without loading changed artifacts.
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), "broken")
	configCommand(t, r, 0, "revoke", "--digest", digest)
	other := configFixture(t)
	other.userDir = r.userDir
	configCommand(t, other, 0, "trust", "--digest", projectTestDigest(t, other))
	moved := configFixture(t)
	moved.userDir = r.userDir
	configCommand(t, moved, 1, "check")
}

func TestConfigExplicitSelectionDoesNotTrustProject(t *testing.T) {
	r := configFixture(t)
	output := configCommand(t, r, 0, "inspect", "--profile", filepath.Join(r.projectDir, "profile.json"), "--bindings", filepath.Join(r.projectDir, "bindings.json"))
	if strings.Contains(output, "requires-project-trust") || !strings.Contains(output, `"projectTrusted":false`) {
		t.Fatal(output)
	}
	if trusted, err := projectTrusted(r, projectTestDigest(t, r)); err != nil || trusted {
		t.Fatal("inspection persisted trust")
	}
	configCommand(t, r, 1, "check", "--profile", filepath.Join(r.projectDir, "profile.json")) // project bindings still win
}

func TestConfigUserSelectionAndNoParentDiscovery(t *testing.T) {
	r := configFixture(t)
	child := filepath.Join(r.projectDir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	r.projectDir = child
	configCommand(t, r, 1, "inspect") // does not find the parent manifest
	copyConfigFixture(t, "evaluator-profile/quickstart-fixture.json", filepath.Join(r.userDir, "fixture.json"))
	writeConfigTestFile(t, filepath.Join(r.userDir, "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"fixture.json"}`)
	output := configCommand(t, r, 0, "check")
	if !strings.Contains(output, `"profileSource":"user"`) || strings.Contains(output, "projectDigest") {
		t.Fatal(output)
	}
	// An unrelated .env is not consulted by any configuration command.
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".env"), "this is not configuration JSON")
	configCommand(t, r, 0, "inspect")
}

func TestConfigMissingCredentialRemediationAndRedaction(t *testing.T) {
	r := configFixture(t)
	configCommand(t, r, 0, "trust", "--digest", projectTestDigest(t, r))
	for _, present := range []bool{false, true} {
		r.environment = localbinding.EnvironmentFunc(func(string) (string, bool) { return "", present })
		diagnostic := configCommand(t, r, 1, "check")
		if !strings.Contains(diagnostic, "provider-api-key") || !strings.Contains(diagnostic, "--bindings") || strings.Contains(diagnostic, "EXAMPLE_API_KEY") {
			t.Fatal(diagnostic)
		}
	}
	for _, args := range [][]string{
		{"inspect", "--profile", "first.json", "--profile", "second.json"},
		{"inspect", "--api-key=DO_NOT_PRINT"},
		{"inspect", "DO_NOT_PRINT"},
		{"trust", "--digest", "DO_NOT_PRINT"},
		{"check", "--bindings", ""},
	} {
		if strings.Contains(configCommand(t, r, 64, args...), "DO_NOT_PRINT") {
			t.Fatal("usage error leaked argument")
		}
	}
}

func TestConfigRejectsMalformedManifestAndEscapes(t *testing.T) {
	for _, manifest := range []string{
		`null`, `[]`, `{}`, `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration"}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":null}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":""}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"x.json","ProfileFile":"y.json"}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"x.json","profileFile":"y.json"}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../.env"}`,
		`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../../escape.json"}`,
		strings.Repeat(" ", 16385),
	} {
		r := configFixture(t)
		writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), manifest)
		configCommand(t, r, 1, "inspect")
	}
	r := configFixture(t)
	outside := filepath.Join(t.TempDir(), "profile.json")
	copyConfigFixture(t, "evaluator-profile/semantic-routing.json", outside)
	if err := os.Remove(filepath.Join(r.projectDir, "profile.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.projectDir, "profile.json")); err != nil {
		t.Skip("symlinks unavailable")
	}
	configCommand(t, r, 1, "inspect")
}

func TestConfigTrustStorageIsIndependentAndRejectsCorruption(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	r.userDir = filepath.Join(r.projectDir, "attacker-user-config")
	configCommand(t, r, 1, "trust", "--digest", digest)
	r.userDir = t.TempDir()
	configCommand(t, r, 0, "trust", "--digest", digest)
	marker, _, err := trustMarker(r, digest)
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, marker, "corrupt")
	configCommand(t, r, 1, "check")
	configCommand(t, r, 1, "trust", "--digest", digest)
	if data, err := os.ReadFile(marker); err != nil || string(data) != "corrupt" {
		t.Fatalf("failed grant changed corrupt marker: %q, %v", data, err)
	}
	configCommand(t, r, 0, "revoke", "--digest", digest)
	configCommand(t, r, 0, "trust", "--digest", digest)
}

func TestConfigRejectsSymlinkedTrustAndDanglingManifest(t *testing.T) {
	r := configFixture(t)
	digest := projectTestDigest(t, r)
	configCommand(t, r, 0, "trust", "--digest", digest)
	marker, content, err := trustMarker(r, digest)
	if err != nil {
		t.Fatal(err)
	}
	configCommand(t, r, 0, "revoke", "--digest", digest)
	other := filepath.Join(r.projectDir, "forged-approval")
	writeConfigTestFile(t, other, content)
	if err := os.Symlink(other, marker); err != nil {
		t.Skip("symlinks unavailable")
	}
	configCommand(t, r, 1, "inspect")
	configCommand(t, r, 1, "trust", "--digest", digest)
	if target, err := os.Readlink(marker); err != nil || target != other {
		t.Fatalf("failed grant changed symlink: %q, %v", target, err)
	}
	if data, err := os.ReadFile(other); err != nil || string(data) != content {
		t.Fatalf("failed grant changed symlink target: %q, %v", data, err)
	}
	configCommand(t, r, 0, "revoke", "--digest", digest)
	if _, err := os.Stat(other); err != nil {
		t.Fatal("revoke removed symlink target")
	}
	manifest := filepath.Join(r.projectDir, ".antaeus", "config.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.json", manifest); err != nil {
		t.Fatal(err)
	}
	configCommand(t, r, 1, "inspect")
}

func TestConfigCommandDispatchHelp(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"config", "help"}, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "revoke --digest") || diagnostic.Len() != 0 {
		t.Fatalf("dispatch: code=%d stdout=%s stderr=%s", code, out.String(), diagnostic.String())
	}
}
