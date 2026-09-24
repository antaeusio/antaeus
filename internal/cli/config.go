package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/localconfig"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/internal/strictsource"
)

const configUsage = `Usage: antaeus config <command>

  inspect [--profile <file>] [--bindings <file>]
          Print redacted selections and project trust state without reading credentials
  check [--profile <file>] [--bindings <file>]
          Check selected environment references after project trust validation
  trust --digest <sha256:digest>
          Trust the current project's exact inspected configuration snapshot
  revoke --digest <sha256:digest>
          Revoke that snapshot for the current project, even if its files changed

Project manifest: .antaeus/config.json in the current directory (no parent search).
User manifest: antaeus/config.json under the OS user-config directory.
These commands do not execute evaluators or change fixture evaluation behavior.
`

var configDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var configPathPattern = regexp.MustCompile(`^[^\x00]+\.(json|yaml|yml)$`)

// configRuntime makes credential reads and filesystem locations explicit in tests.
type configRuntime struct {
	projectDir  string
	userDir     string
	environment localbinding.Environment
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = io.WriteString(stdout, configUsage)
		return 0
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return commandError(stderr, "config", errors.New("cannot locate current project directory"))
	}
	userDir, err := os.UserConfigDir()
	if err != nil {
		return commandError(stderr, "config", errors.New("cannot locate OS user-config directory"))
	}
	return runConfigWith(args, stdout, stderr, configRuntime{projectDir, filepath.Join(userDir, "antaeus"), localbinding.EnvironmentFunc(os.LookupEnv)})
}

func runConfigWith(args []string, stdout, stderr io.Writer, runtime configRuntime) int {
	if len(args) == 0 {
		return usageError(stderr, "config requires a command")
	}
	action := args[0]
	if action != "inspect" && action != "check" && action != "trust" && action != "revoke" {
		return usageError(stderr, "unknown config command")
	}
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var profilePath, bindingsPath, digest string
	if action == "trust" || action == "revoke" {
		uniqueConfigFlag(flags, "digest", &digest)
	} else {
		uniqueConfigFlag(flags, "profile", &profilePath)
		uniqueConfigFlag(flags, "bindings", &bindingsPath)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, configUsage)
			return 0
		}
		return usageError(stderr, "invalid or repeated config flag; run antaeus config help")
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "config does not accept positional arguments")
	}
	if action == "trust" || action == "revoke" {
		if !configDigestPattern.MatchString(digest) {
			return usageError(stderr, "config trust/revoke requires --digest sha256:<64 lowercase hex digits>")
		}
	}
	if err := independentUserConfig(runtime); err != nil {
		return commandError(stderr, "config", err)
	}
	if action == "revoke" {
		if err := changeProjectTrust(runtime, digest, false); err != nil {
			return commandError(stderr, "config revoke", err)
		}
		return writeJSON(stdout, stderr, "config revoke", map[string]any{"projectDigest": digest, "trusted": false})
	}
	project, err := loadConfigManifest(filepath.Join(runtime.projectDir, ".antaeus", "config.json"), runtime.projectDir)
	if err != nil {
		return commandError(stderr, "config project", err)
	}
	if action == "trust" {
		if project.Digest() == "" || project.Digest() != digest {
			return commandError(stderr, "config trust", errors.New("project snapshot does not match the supplied digest; inspect the configuration again"))
		}
		if err := changeProjectTrust(runtime, digest, true); err != nil {
			return commandError(stderr, "config trust", err)
		}
		return writeJSON(stdout, stderr, "config trust", map[string]any{"projectDigest": digest, "trusted": true})
	}
	user, err := loadConfigManifest(filepath.Join(runtime.userDir, "config.json"), "")
	if err != nil {
		return commandError(stderr, "config user", err)
	}
	cli, err := loadConfigLayer(profilePath, bindingsPath)
	if err != nil {
		return commandError(stderr, "config explicit selection", err)
	}
	trusted, err := projectTrusted(runtime, project.Digest())
	if err != nil {
		return commandError(stderr, "config trust store", err)
	}
	o := localconfig.Options{CLI: cli, Project: project, User: user}
	if trusted {
		o.TrustedProjectDigest = project.Digest()
	}
	resolved, err := localconfig.Resolve(o)
	access := "allowed"
	var trust *localconfig.TrustRequiredError
	if errors.As(err, &trust) {
		if action == "check" {
			return commandError(stderr, "config check", fmt.Errorf("project requires trust; inspect its files and run antaeus config trust --digest %s", trust.Digest))
		}
		// Inspection cannot call Preflight. Use a temporary approval solely to
		// derive source summaries; report the actual persistent trust state below.
		o.TrustedProjectDigest = project.Digest()
		resolved, err = localconfig.Resolve(o)
		access = "requires-project-trust"
	}
	if err != nil {
		return configResolutionError(stderr, err)
	}
	if action == "check" {
		credentials, err := resolved.Preflight(runtime.environment)
		if err != nil {
			return configResolutionError(stderr, err)
		}
		credentials.Clear()
	}
	return writeJSON(stdout, stderr, "config", struct {
		Configuration      localconfig.Summary `json:"configuration"`
		ProjectDigest      string              `json:"projectDigest,omitempty"`
		ProjectTrusted     bool                `json:"projectTrusted"`
		CredentialAccess   string              `json:"credentialAccess"`
		CredentialsChecked bool                `json:"credentialsChecked"`
	}{resolved.Summary(), project.Digest(), trusted, access, action == "check"})
}

func uniqueConfigFlag(flags *flag.FlagSet, name string, target *string) {
	seen := false
	flags.Func(name, "explicit non-secret configuration selection", func(value string) error {
		if seen || value == "" {
			return errors.New("repeated or empty flag")
		}
		seen = true
		*target = value
		return nil
	})
}

func configResolutionError(stderr io.Writer, err error) int {
	var missing *localbinding.MissingCredentialError
	if errors.As(err, &missing) {
		return commandError(stderr, "config", fmt.Errorf("%w; configure a reference with --bindings or a project/user bindingsFile, then set its referenced variable in the process environment", missing))
	}
	return commandError(stderr, "config", err)
}

// Manifests are bounded strict JSON. Paths are explicit, relative to the
// manifest directory unless absolute. Project paths must remain in the project.
func loadConfigManifest(path, projectBoundary string) (localconfig.Layer, error) {
	data, err := readConfigFileWithin(path, 16<<10, projectBoundary)
	if errors.Is(err, os.ErrNotExist) {
		return localconfig.Layer{}, nil
	}
	if err != nil {
		return localconfig.Layer{}, errors.New("cannot read configuration manifest")
	}
	data, err = strictsource.Decode(data, strictsource.FormatJSON, 16<<10, "local configuration")
	if err != nil {
		return localconfig.Layer{}, errors.New("invalid configuration manifest; expected strict LocalConfiguration JSON")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return localconfig.Layer{}, errors.New("configuration manifest must be an object")
	}
	values := map[string]string{}
	for key, raw := range fields {
		if key != "apiVersion" && key != "kind" && key != "profileFile" && key != "bindingsFile" {
			return localconfig.Layer{}, errors.New("unknown configuration manifest field")
		}
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return localconfig.Layer{}, errors.New("configuration manifest fields must be strings")
		}
		values[key] = value
	}
	if values["apiVersion"] != "config.antaeus.io/v0alpha1" || values["kind"] != "LocalConfiguration" {
		return localconfig.Layer{}, errors.New("unsupported local configuration version or kind")
	}
	if _, p := fields["profileFile"]; !p {
		if _, b := fields["bindingsFile"]; !b {
			return localconfig.Layer{}, errors.New("configuration must select a profileFile or bindingsFile")
		}
	}
	for _, key := range []string{"profileFile", "bindingsFile"} {
		if _, present := fields[key]; !present {
			continue
		}
		value := values[key]
		if utf8.RuneCountInString(value) > 4096 || !configPathPattern.MatchString(value) {
			return localconfig.Layer{}, errors.New("configuration paths must name a JSON or YAML artifact")
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(filepath.Dir(path), value)
		}
		values[key] = value
	}
	return loadConfigLayerWithin(values["profileFile"], values["bindingsFile"], projectBoundary)
}

func loadConfigLayer(profilePath, bindingsPath string) (localconfig.Layer, error) {
	return loadConfigLayerWithin(profilePath, bindingsPath, "")
}

func loadConfigLayerWithin(profilePath, bindingsPath, boundary string) (localconfig.Layer, error) {
	var p *profile.Artifact
	var b *localbinding.Artifact
	if profilePath != "" {
		if !configPathPattern.MatchString(profilePath) {
			return localconfig.Layer{}, errors.New("profile path must name a JSON or YAML artifact")
		}
		data, err := readConfigFileWithin(profilePath, profile.MaxSourceBytes, boundary)
		if err != nil {
			return localconfig.Layer{}, errors.New("cannot read selected profile artifact inside its configuration boundary")
		}
		format := profile.FormatJSON
		if filepath.Ext(profilePath) != ".json" {
			format = profile.FormatYAML
		}
		artifact, err := profile.Parse(data, format)
		if err != nil {
			return localconfig.Layer{}, errors.New("cannot load selected evaluator profile; inspect the JSON/YAML artifact")
		}
		p = &artifact
	}
	if bindingsPath != "" {
		if !configPathPattern.MatchString(bindingsPath) {
			return localconfig.Layer{}, errors.New("bindings path must name a JSON or YAML artifact")
		}
		data, err := readConfigFileWithin(bindingsPath, localbinding.MaxSourceBytes, boundary)
		if err != nil {
			return localconfig.Layer{}, errors.New("cannot read selected bindings artifact inside its configuration boundary")
		}
		format := localbinding.FormatJSON
		if filepath.Ext(bindingsPath) != ".json" {
			format = localbinding.FormatYAML
		}
		artifact, err := localbinding.Parse(data, format)
		if err != nil {
			return localconfig.Layer{}, errors.New("cannot load selected secret bindings; inspect the reference-only JSON/YAML artifact")
		}
		b = &artifact
	}
	return localconfig.NewLayer(p, b)
}

func readConfigFile(path string, maxBytes int64) ([]byte, error) {
	return readConfigFileWithin(path, maxBytes, "")
}

func readConfigFileWithin(path string, maxBytes int64, boundary string) ([]byte, error) {
	open, stat, lstat := os.Open, os.Stat, os.Lstat
	if boundary != "" {
		root, err := os.OpenRoot(boundary)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		path, err = filepath.Rel(boundary, path)
		if err != nil {
			return nil, err
		}
		open, stat, lstat = root.Open, root.Stat, root.Lstat
	}
	info, err := stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// ENOENT may come from an intermediate symlink, not the final
			// component. Do not turn malformed higher-priority configuration
			// into permission to use a lower-priority manifest.
			for candidate := path; ; candidate = filepath.Dir(candidate) {
				_, linkErr := lstat(candidate)
				if linkErr == nil {
					// Not all platforms label directory redirections as
					// symlinks. Require every existing ancestor to resolve.
					if _, targetErr := stat(candidate); targetErr != nil {
						return nil, errors.New("configuration contains an unresolved path")
					}
					break
				}
				if !errors.Is(linkErr, os.ErrNotExist) {
					return nil, linkErr
				}
				if filepath.Dir(candidate) == candidate {
					break
				}
			}
		}
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, errors.New("invalid configuration file size or type")
	}
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("configuration must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, errors.New("cannot read bounded configuration file")
	}
	return data, nil
}

// Each approval gets its own exclusive marker, avoiding read/modify/write races
// between unrelated grants. The marker content is not an authorization source:
// it must exactly match the independently computed project and snapshot key.
func trustMarker(runtime configRuntime, digest string) (string, string, error) {
	project, err := filepath.EvalSymlinks(runtime.projectDir)
	if err != nil {
		return "", "", errors.New("cannot resolve project directory for trust")
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return "", "", errors.New("cannot resolve project directory for trust")
	}
	storage, err := resolveFuturePath(filepath.Join(runtime.userDir, "trust"))
	if err != nil {
		return "", "", errors.New("cannot resolve independent user trust directory")
	}
	inside, err := pathWithinDirectory(project, storage)
	if err != nil {
		return "", "", errors.New("cannot verify independent user trust directory")
	}
	if inside {
		return "", "", errors.New("user trust directory must be outside the project")
	}
	key := sha256.Sum256([]byte(project + "\x00" + digest))
	name := hex.EncodeToString(key[:])
	return filepath.Join(storage, name+".approval"), "antaeus-project-trust-v1\n" + name + "\n", nil
}

func resolveFuturePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent, err := resolveFuturePath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func independentUserConfig(runtime configRuntime) error {
	project, err := filepath.EvalSymlinks(runtime.projectDir)
	if err != nil {
		return errors.New("cannot resolve project directory")
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return errors.New("cannot resolve project directory")
	}
	user, err := resolveFuturePath(runtime.userDir)
	if err != nil {
		return errors.New("cannot resolve user configuration directory")
	}
	inside, err := pathWithinDirectory(project, user)
	if err != nil {
		return errors.New("cannot verify independent user configuration directory")
	}
	if inside {
		return errors.New("user configuration directory must be outside the project")
	}
	return nil
}

// Paths have already had existing symlinks resolved. Compare filesystem
// identities of existing ancestors, not spellings: casing and other aliases
// can identify the same directory on supported filesystems. Missing suffixes
// are permitted because trust/config directories may not have been created yet.
func pathWithinDirectory(directory, path string) (bool, error) {
	base, err := os.Stat(directory)
	if err != nil {
		return false, err
	}
	for {
		info, err := os.Stat(path)
		if err == nil && os.SameFile(base, info) {
			return true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false, nil
		}
		path = parent
	}
}

func projectTrusted(runtime configRuntime, digest string) (bool, error) {
	if digest == "" {
		return false, nil
	}
	path, expected, err := trustMarker(runtime, digest)
	if err != nil {
		return false, err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("project trust markers must not be symlinks")
	}
	data, err := readConfigFile(path, 256)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || string(data) != expected {
		return false, errors.New("invalid project trust marker; revoke the digest before trusting it again")
	}
	return true, nil
}

func changeProjectTrust(runtime configRuntime, digest string, grant bool) error {
	path, marker, err := trustMarker(runtime, digest)
	if err != nil {
		return err
	}
	if !grant {
		err = os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot revoke project trust")
		}
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create project trust directory")
	}
	// Publish only a complete, closed marker. A same-directory hard link is
	// atomic and does not replace an existing grant or symlink. Unsupported
	// filesystems fail closed rather than reverting to partial publication.
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-approval-*")
	if err != nil {
		return errors.New("cannot create project trust marker")
	}
	defer os.Remove(f.Name())
	_, writeErr := io.WriteString(f, marker)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("cannot save project trust; grant was not recorded")
	}
	if err := os.Link(f.Name(), path); errors.Is(err, os.ErrExist) {
		trusted, err := projectTrusted(runtime, digest)
		if err != nil {
			return err
		}
		if !trusted {
			return errors.New("project trust changed during grant; inspect and retry")
		}
		return nil
	} else if err != nil {
		return errors.New("cannot publish project trust; trust storage must support atomic hard links")
	}
	return nil
}
