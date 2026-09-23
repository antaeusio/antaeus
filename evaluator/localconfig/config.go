// Package localconfig resolves local evaluator configuration without discovering
// files or reading credentials. It must not be used for production deployments.
package localconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/gowebpki/jcs"
)

type Source string

const (
	SourceCLI     Source = "cli"
	SourceProject Source = "project"
	SourceUser    Source = "user"
	SourceBuiltin Source = "builtin-fixture"
	SourceAdapter Source = "adapter-default"
)

// Layer is an immutable snapshot of one configuration level. Construct it with
// NewLayer; the zero value is an absent level. Neither artifact contains secrets.
type Layer struct {
	profileJSON []byte
	bindings    localbinding.Artifact
	digest      string
}

// NewLayer validates and copies both inputs. A project layer must include all
// its profile and binding configuration, including shadowed and unused entries,
// so changing any of them invalidates prior trust. It accepts no paths or URLs.
func NewLayer(p *profile.Artifact, bindings *localbinding.Artifact) (Layer, error) {
	var l Layer
	var err error
	if p != nil {
		l.profileJSON, err = p.CanonicalJSON()
		if err != nil {
			return Layer{}, fmt.Errorf("invalid layer profile: %w", err)
		}
	}
	if bindings != nil {
		if err := bindings.Validate(); err != nil {
			return Layer{}, errors.New("invalid layer bindings; inspect the bindings artifact")
		}
		l.bindings = localbinding.Artifact{APIVersion: bindings.APIVersion, Kind: bindings.Kind, SecretBindings: make(map[string]map[string]localbinding.Reference)}
		for adapter, slots := range bindings.SecretBindings {
			l.bindings.SecretBindings[adapter] = make(map[string]localbinding.Reference, len(slots))
			for slot, ref := range slots {
				l.bindings.SecretBindings[adapter][slot] = ref
			}
		}
	}
	if p == nil && bindings == nil {
		return l, nil
	}
	var copiedBindings *localbinding.Artifact
	if bindings != nil {
		copiedBindings = &l.bindings
	}
	encoded, err := json.Marshal(struct {
		Version  string                 `json:"version"`
		Profile  json.RawMessage        `json:"profile"`
		Bindings *localbinding.Artifact `json:"bindings"`
	}{"antaeus.local-config/v0alpha1", l.profileJSON, copiedBindings})
	if err != nil {
		return Layer{}, err
	}
	canonical, err := jcs.Transform(encoded)
	if err != nil {
		return Layer{}, err
	}
	sum := sha256.Sum256(canonical)
	l.digest = "sha256:" + hex.EncodeToString(sum[:])
	return l, nil
}

// Digest identifies the complete snapshot, not source formatting or filenames.
// Empty layers have an empty digest. Digest contains no credential values.
func (l Layer) Digest() string { return l.digest }

// Options must be assembled by the local caller from explicitly loaded sources.
// TrustedProjectDigest comes from an independent user trust store or explicit
// user approval, never from project configuration itself. Recheck that trust on
// each invocation. This package does not persist trust or implement CLI discovery.
type Options struct {
	CLI                  Layer
	Project              Layer
	User                 Layer
	TrustedProjectDigest string
	// Builtin is optional and allowed only for a documented synthetic command.
	Builtin *profile.Artifact
	// Defaults are registered by installed adapter contracts, keyed by exact
	// adapter identity and version. They are not user or project endpoint settings.
	Defaults map[profile.ComponentIdentity]map[string]localbinding.Reference
}

type BindingSummary struct {
	AdapterID       string `json:"adapterId"`
	Slot            string `json:"slot"`
	Source          Source `json:"source"`
	ReferenceSource string `json:"referenceSource"`
}

// Summary is safe for ordinary configuration output. It omits reference names,
// values, paths, and profile parameters. Bindings follow first route occurrence.
type Summary struct {
	ConfigurationDigest string           `json:"configurationDigest"`
	ProfileDigest       string           `json:"profileDigest"`
	ProfileSource       Source           `json:"profileSource"`
	ProjectDigest       string           `json:"projectDigest,omitempty"`
	Bindings            []BindingSummary `json:"bindings"`
}

// Resolved holds immutable selected artifacts. Construct it only with Resolve.
// Resolve does not validate installed adapter parameters or execute a route.
type Resolved struct {
	profileJSON []byte
	bindings    localbinding.Artifact
	summary     Summary
}

type TrustRequiredError struct{ Digest string }

func (e *TrustRequiredError) Error() string {
	return "project configuration requires trust before credential access; inspect the complete configuration and explicitly trust digest " + e.Digest
}

// Resolve applies whole-profile precedence and per-adapter/slot reference
// precedence. No environment access occurs here, including on errors.
func Resolve(o Options) (*Resolved, error) {
	layers := []struct {
		source Source
		layer  Layer
	}{
		{SourceCLI, o.CLI}, {SourceProject, o.Project}, {SourceUser, o.User},
	}
	r := &Resolved{summary: Summary{Bindings: []BindingSummary{}}, bindings: localbinding.Artifact{
		APIVersion: localbinding.APIVersion, Kind: localbinding.Kind,
		SecretBindings: make(map[string]map[string]localbinding.Reference),
	}}
	for _, entry := range layers {
		if len(entry.layer.profileJSON) != 0 {
			r.profileJSON = append([]byte(nil), entry.layer.profileJSON...)
			r.summary.ProfileSource = entry.source
			break
		}
	}
	if len(r.profileJSON) == 0 && o.Builtin != nil {
		canonical, err := o.Builtin.CanonicalJSON()
		if err != nil {
			return nil, fmt.Errorf("invalid built-in profile: %w", err)
		}
		for _, e := range o.Builtin.Spec.Evaluators {
			if e.Mode != profile.ModeDeterministicFixture {
				return nil, errors.New("built-in profile must contain only deterministic fixtures")
			}
		}
		r.profileJSON = canonical
		r.summary.ProfileSource = SourceBuiltin
	}
	if len(r.profileJSON) == 0 {
		return nil, errors.New("no evaluator profile selected; supply an explicit profile or configure a project or user profile")
	}
	p, err := r.Profile()
	if err != nil {
		return nil, err
	}
	r.summary.ProfileDigest, err = p.Digest()
	if err != nil {
		return nil, err
	}
	evaluators := make(map[string]profile.Evaluator, len(p.Spec.Evaluators))
	for _, e := range p.Spec.Evaluators {
		evaluators[e.ID] = e
	}
	route := []string{p.Spec.Routing.Primary}
	if p.Spec.Routing.Escalation != nil {
		route = append(route, *p.Spec.Routing.Escalation)
	}
	route = append(route, p.Spec.Routing.Fallbacks...)
	projectUsed := r.summary.ProfileSource == SourceProject
	needsCredentials := false
	referenceNames := make(map[string]string)
	for _, id := range route {
		e := evaluators[id]
		if e.CredentialSlot == nil {
			continue
		}
		needsCredentials = true
		key := localbinding.Key{AdapterID: e.Adapter.ID, Slot: *e.CredentialSlot}
		var ref localbinding.Reference
		var source Source
		for _, entry := range layers {
			if candidate, exists := entry.layer.bindings.SecretBindings[key.AdapterID][key.Slot]; exists {
				ref, source = candidate, entry.source
				break
			}
		}
		if source == "" {
			if candidate, exists := o.Defaults[e.Adapter][key.Slot]; exists {
				ref, source = candidate, SourceAdapter
			}
		}
		if source == "" {
			return nil, &localbinding.MissingCredentialError{EvaluatorID: e.ID, AdapterID: key.AdapterID, Slot: key.Slot}
		}
		if source == SourceProject {
			projectUsed = true
		}
		folded := strings.ToUpper(ref.Name)
		if previous, exists := referenceNames[folded]; exists && previous != ref.Name {
			return nil, errors.New("selected environment references differ only by letter case; use portable unambiguous names")
		}
		referenceNames[folded] = ref.Name
		if previous, exists := r.bindings.SecretBindings[key.AdapterID][key.Slot]; exists {
			if previous != ref {
				return nil, errors.New("conflicting adapter defaults for a shared credential slot; supply an explicit reference")
			}
			continue
		}
		if r.bindings.SecretBindings[key.AdapterID] == nil {
			r.bindings.SecretBindings[key.AdapterID] = make(map[string]localbinding.Reference)
		}
		r.bindings.SecretBindings[key.AdapterID][key.Slot] = ref
		r.summary.Bindings = append(r.summary.Bindings, BindingSummary{key.AdapterID, key.Slot, source, "environment"})
	}
	if projectUsed {
		r.summary.ProjectDigest = o.Project.Digest()
		if needsCredentials && (o.TrustedProjectDigest == "" || o.TrustedProjectDigest != o.Project.Digest()) {
			return nil, &TrustRequiredError{Digest: o.Project.Digest()}
		}
	}
	if needsCredentials {
		if err := r.bindings.Validate(); err != nil {
			return nil, errors.New("invalid effective credential references; inspect the selected binding sources")
		}
	}
	var effectiveBindings *localbinding.Artifact
	if needsCredentials {
		effectiveBindings = &r.bindings
	}
	effective, err := NewLayer(&p, effectiveBindings)
	if err != nil {
		return nil, err
	}
	r.summary.ConfigurationDigest = effective.Digest()
	return r, nil
}

func (r *Resolved) Summary() Summary {
	if r == nil {
		return Summary{}
	}
	s := r.summary
	s.Bindings = append([]BindingSummary{}, s.Bindings...)
	return s
}

// Profile returns a fresh independent copy for installed-adapter validation.
func (r *Resolved) Profile() (profile.Artifact, error) {
	if r == nil || len(r.profileJSON) == 0 {
		return profile.Artifact{}, errors.New("configuration has not been resolved")
	}
	return profile.Parse(r.profileJSON, profile.FormatJSON)
}

// Preflight captures only selected references, immediately before evaluation.
// An unset winning reference is an error; it never falls back to a lower layer.
// Call Clear on the returned credentials when evaluation ends. A synthetic
// profile needs no lookup and returns an empty credential set.
func (r *Resolved) Preflight(environment localbinding.Environment) (*localbinding.Credentials, error) {
	p, err := r.Profile()
	if err != nil {
		return nil, err
	}
	if len(r.summary.Bindings) == 0 {
		return &localbinding.Credentials{}, nil
	}
	return localbinding.Preflight(p, r.bindings, environment)
}
