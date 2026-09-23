package localconfig_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/localconfig"
	"github.com/antaeusio/antaeus/evaluator/profile"
)

func semantic(t *testing.T) profile.Artifact {
	t.Helper()
	p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/semantic-routing.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func bindings(name string) localbinding.Artifact {
	return localbinding.Artifact{APIVersion: localbinding.APIVersion, Kind: localbinding.Kind, SecretBindings: map[string]map[string]localbinding.Reference{
		"io.example.semantic": {"provider-api-key": {Source: "environment", Name: name}},
	}}
}

func layer(t *testing.T, p *profile.Artifact, b *localbinding.Artifact) localconfig.Layer {
	t.Helper()
	l, err := localconfig.NewLayer(p, b)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestProfilePrecedence(t *testing.T) {
	p := semantic(t)
	b := bindings("TOKEN")
	cliProfile, projectProfile, userProfile := p, semantic(t), semantic(t)
	cliProfile.Metadata.Name, projectProfile.Metadata.Name, userProfile.Metadata.Name = "cli", "project", "user"
	cli := layer(t, &cliProfile, &b)
	project := layer(t, &projectProfile, &b)
	user := layer(t, &userProfile, &b)
	for _, test := range []struct {
		name               string
		cli, project, user localconfig.Layer
		want               localconfig.Source
	}{
		{"cli", cli, project, user, localconfig.SourceCLI},
		{"project", localconfig.Layer{}, project, user, localconfig.SourceProject},
		{"user", localconfig.Layer{}, localconfig.Layer{}, user, localconfig.SourceUser},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, err := localconfig.Resolve(localconfig.Options{CLI: test.cli, Project: test.project, User: test.user, TrustedProjectDigest: project.Digest()})
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Profile()
			if err != nil || got.Metadata.Name != test.name || r.Summary().ProfileSource != test.want {
				t.Fatalf("selection = %+v, %v", r.Summary(), err)
			}
		})
	}
	if _, err := localconfig.Resolve(localconfig.Options{}); err == nil {
		t.Fatal("missing profile accepted")
	}
	if _, err := localconfig.Resolve(localconfig.Options{Builtin: &p}); err == nil {
		t.Fatal("semantic built-in accepted")
	}
	fixture, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := localconfig.Resolve(localconfig.Options{Builtin: &fixture})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary().ProfileSource != localconfig.SourceBuiltin {
		t.Fatal(r.Summary())
	}
	credentials, err := r.Preflight(localbinding.EnvironmentFunc(func(string) (string, bool) { t.Fatal("fixture queried environment"); return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	credentials.Clear()
}

func TestBindingPrecedenceReadsOnlyWinner(t *testing.T) {
	p := semantic(t)
	cliBindings, projectBindings, userBindings := bindings("CLI_TOKEN"), bindings("PROJECT_TOKEN"), bindings("USER_TOKEN")
	cliBindings.SecretBindings["io.example.unused"] = map[string]localbinding.Reference{"key": {Source: "environment", Name: "UNRELATED_TOKEN"}}
	cli, project, user := layer(t, nil, &cliBindings), layer(t, nil, &projectBindings), layer(t, nil, &userBindings)
	profileLayer := layer(t, &p, nil)
	for _, test := range []struct {
		name               string
		cli, project, user localconfig.Layer
		want               localconfig.Source
	}{
		{"CLI_TOKEN", cli, project, user, localconfig.SourceCLI},
		{"PROJECT_TOKEN", localconfig.Layer{}, project, user, localconfig.SourceProject},
		{"USER_TOKEN", localconfig.Layer{}, localconfig.Layer{}, user, localconfig.SourceUser},
		{"DEFAULT_TOKEN", localconfig.Layer{}, localconfig.Layer{}, localconfig.Layer{}, localconfig.SourceAdapter},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Select the profile at user level without changing reference precedence.
			u := test.user
			if test.want == localconfig.SourceAdapter {
				u = profileLayer
			} else {
				u = layer(t, &p, &userBindings)
			}
			r, err := localconfig.Resolve(localconfig.Options{CLI: test.cli, Project: test.project, User: u, TrustedProjectDigest: project.Digest(), Defaults: map[profile.ComponentIdentity]map[string]localbinding.Reference{
				p.Spec.Evaluators[0].Adapter: {"provider-api-key": {Source: "environment", Name: "DEFAULT_TOKEN"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			var reads []string
			credentials, err := r.Preflight(localbinding.EnvironmentFunc(func(name string) (string, bool) { reads = append(reads, name); return " exact secret ", true }))
			if err != nil {
				t.Fatal(err)
			}
			defer credentials.Clear()
			if !reflect.DeepEqual(reads, []string{test.name}) {
				t.Fatalf("reads = %v", reads)
			}
			summary := r.Summary()
			if len(summary.Bindings) != 1 || summary.Bindings[0].Source != test.want {
				t.Fatal(summary)
			}
			output, _ := json.Marshal(summary)
			if strings.Contains(string(output), "TOKEN") || strings.Contains(string(output), "exact secret") {
				t.Fatalf("summary leaks: %s", output)
			}
			value, ok := credentials.Credential("io.example.semantic", "provider-api-key")
			if !ok || string(value) != " exact secret " {
				t.Fatal("credential was altered")
			}
		})
	}
}

func TestTrustRequiredForProjectProfileEvenWithExplicitCredentials(t *testing.T) {
	p := semantic(t)
	b := bindings("CLI_TOKEN")
	project := layer(t, &p, nil)
	for _, digest := range []string{"", "sha256:" + strings.Repeat("0", 64)} {
		_, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, nil, &b), Project: project, TrustedProjectDigest: digest})
		var trust *localconfig.TrustRequiredError
		if !errors.As(err, &trust) || trust.Digest != project.Digest() {
			t.Fatalf("error = %v", err)
		}
	}
	// Every fallback credential must also pass the project trust gate.
	p.Spec.Evaluators[2].Adapter.ID = "io.example.fallback"
	projectBindings := bindings("UNUSED_TOKEN")
	projectBindings.SecretBindings["io.example.fallback"] = map[string]localbinding.Reference{"provider-api-key": {Source: "environment", Name: "FALLBACK_TOKEN"}}
	_, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &b), Project: layer(t, nil, &projectBindings)})
	var trust *localconfig.TrustRequiredError
	if !errors.As(err, &trust) {
		t.Fatalf("fallback bypassed trust: %v", err)
	}
}

func TestTrustDigestAndSnapshotIsolation(t *testing.T) {
	p := semantic(t)
	b := bindings("PROJECT_TOKEN")
	project := layer(t, &p, &b)
	trusted := project.Digest()
	r, err := localconfig.Resolve(localconfig.Options{Project: project, TrustedProjectDigest: trusted})
	if err != nil {
		t.Fatal(err)
	}
	// Input mutations and output mutations cannot change the approved snapshot.
	b.SecretBindings["io.example.semantic"]["provider-api-key"] = localbinding.Reference{Source: "environment", Name: "CHANGED_TOKEN"}
	p.Spec.Evaluators[0].Model = new("changed-model")
	changed := layer(t, &p, &b)
	if changed.Digest() == trusted {
		t.Fatal("configuration edit preserved trust")
	}
	_, err = localconfig.Resolve(localconfig.Options{Project: changed, TrustedProjectDigest: trusted})
	var trust *localconfig.TrustRequiredError
	if !errors.As(err, &trust) {
		t.Fatalf("changed configuration accepted old trust: %v", err)
	}
	// Revocation is represented by omitting the digest on the next invocation.
	_, err = localconfig.Resolve(localconfig.Options{Project: project})
	if !errors.As(err, &trust) {
		t.Fatal("revoked configuration accepted")
	}
	copyProfile, _ := r.Profile()
	copyProfile.Spec.Evaluators[0].Model = new("other-model")
	summary := r.Summary()
	summary.Bindings[0].Source = localconfig.SourceAdapter
	if r.Summary().Bindings[0].Source != localconfig.SourceProject {
		t.Fatal("summary alias")
	}
	again, _ := r.Profile()
	if *again.Spec.Evaluators[0].Model == "other-model" {
		t.Fatal("profile alias")
	}
	credentials, err := r.Preflight(localbinding.EnvironmentFunc(func(name string) (string, bool) {
		if name != "PROJECT_TOKEN" {
			t.Fatalf("mutated snapshot read %q", name)
		}
		return "secret", true
	}))
	if err != nil {
		t.Fatal(err)
	}
	credentials.Clear()
	// Unused project references are included in the digest too.
	original := bindings("PROJECT_TOKEN")
	original.SecretBindings["io.example.unused"] = map[string]localbinding.Reference{"key": {Source: "environment", Name: "UNUSED"}}
	if layer(t, &again, &original).Digest() == trusted {
		t.Fatal("unused reference omitted from trust digest")
	}
}

func TestShadowedProjectDoesNotRequireTrust(t *testing.T) {
	p := semantic(t)
	b := bindings("CLI_TOKEN")
	projectBindings := bindings("PROJECT_TOKEN")
	r, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &b), Project: layer(t, &p, &projectBindings)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary().ProjectDigest != "" {
		t.Fatal("shadowed project claimed provenance")
	}
}

func TestMissingWinnerNeverFallsBack(t *testing.T) {
	p := semantic(t)
	cliBindings, userBindings := bindings("MISSING_TOKEN"), bindings("PRESENT_TOKEN")
	r, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &cliBindings), User: layer(t, nil, &userBindings)})
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []bool{false, true} {
		var reads []string
		_, err = r.Preflight(localbinding.EnvironmentFunc(func(name string) (string, bool) { reads = append(reads, name); return "", present }))
		var missing *localbinding.MissingCredentialError
		if !errors.As(err, &missing) || !reflect.DeepEqual(reads, []string{"MISSING_TOKEN"}) {
			t.Fatalf("reads = %v, error = %v", reads, err)
		}
		if strings.Contains(err.Error(), "TOKEN") {
			t.Fatal("error leaked reference name")
		}
	}
}

func TestAdapterDefaultsUseExactVersionAndRejectConflicts(t *testing.T) {
	p := semantic(t)
	defaults := map[profile.ComponentIdentity]map[string]localbinding.Reference{
		{ID: "io.example.semantic", Version: "2.0.0"}: {"provider-api-key": {Source: "environment", Name: "V2_TOKEN"}},
	}
	_, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, nil), Defaults: defaults})
	var missing *localbinding.MissingCredentialError
	if !errors.As(err, &missing) {
		t.Fatalf("wrong-version default accepted: %v", err)
	}
	p.Spec.Evaluators[2].Adapter.Version = "2.0.0"
	defaults[profile.ComponentIdentity{ID: "io.example.semantic", Version: "1.0.0"}] = map[string]localbinding.Reference{"provider-api-key": {Source: "environment", Name: "V1_TOKEN"}}
	_, err = localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, nil), Defaults: defaults})
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflicting defaults accepted: %v", err)
	}
	// An explicit binding disambiguates the shared tuple for both versions.
	b := bindings("EXPLICIT_TOKEN")
	if _, err = localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &b), Defaults: defaults}); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedSourcesAndZeroResultFailClosed(t *testing.T) {
	p := semantic(t)
	b := bindings("not an environment name")
	if _, err := localconfig.NewLayer(&p, &b); err == nil {
		t.Fatal("invalid layer accepted")
	}
	o := localconfig.Options{CLI: layer(t, &p, nil), Defaults: map[profile.ComponentIdentity]map[string]localbinding.Reference{
		p.Spec.Evaluators[0].Adapter: {"provider-api-key": {Source: "raw-secret", Name: "SECRET_VALUE"}},
	}}
	if _, err := localconfig.Resolve(o); err == nil || strings.Contains(err.Error(), "SECRET_VALUE") {
		t.Fatalf("error = %v", err)
	}
	for _, r := range []*localconfig.Resolved{nil, {}} {
		if _, err := r.Preflight(nil); err == nil {
			t.Fatal("unresolved preflight accepted")
		}
	}
}

func TestMixedSourcesAndCaseAmbiguity(t *testing.T) {
	p := semantic(t)
	p.Spec.Evaluators[2].Adapter.ID = "io.example.fallback"
	cliBindings := bindings("PRIMARY_TOKEN")
	userBindings := localbinding.Artifact{APIVersion: localbinding.APIVersion, Kind: localbinding.Kind,
		SecretBindings: map[string]map[string]localbinding.Reference{
			"io.example.fallback": {"provider-api-key": {Source: "environment", Name: "FALLBACK_TOKEN"}},
		}}
	r, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &cliBindings), User: layer(t, nil, &userBindings)})
	if err != nil {
		t.Fatal(err)
	}
	var reads []string
	credentials, err := r.Preflight(localbinding.EnvironmentFunc(func(name string) (string, bool) {
		reads = append(reads, name)
		return "secret", true
	}))
	if err != nil {
		t.Fatal(err)
	}
	credentials.Clear()
	if !reflect.DeepEqual(reads, []string{"PRIMARY_TOKEN", "FALLBACK_TOKEN"}) {
		t.Fatalf("reads = %v", reads)
	}
	s := r.Summary()
	if len(s.Bindings) != 2 || s.Bindings[0].Source != localconfig.SourceCLI || s.Bindings[1].Source != localconfig.SourceUser {
		t.Fatal(s)
	}
	userBindings.SecretBindings["io.example.fallback"]["provider-api-key"] = localbinding.Reference{Source: "environment", Name: "primary_token"}
	_, err = localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &cliBindings), User: layer(t, nil, &userBindings)})
	if err == nil || strings.Contains(err.Error(), "primary_token") {
		t.Fatalf("case ambiguity error = %v", err)
	}
}

func TestEffectiveIdentityTracksReferencesButNotValues(t *testing.T) {
	p := semantic(t)
	b := bindings("FIRST_TOKEN")
	first, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &b)})
	if err != nil {
		t.Fatal(err)
	}
	before := first.Summary()
	for _, value := range []string{"before-rotation", "after-rotation"} {
		credentials, err := first.Preflight(localbinding.EnvironmentFunc(func(string) (string, bool) { return value, true }))
		if err != nil {
			t.Fatal(err)
		}
		credentials.Clear()
		if first.Summary().ConfigurationDigest != before.ConfigurationDigest {
			t.Fatal("value rotation changed identity")
		}
	}
	b.SecretBindings["io.example.semantic"]["provider-api-key"] = localbinding.Reference{Source: "environment", Name: "SECOND_TOKEN"}
	second, err := localconfig.Resolve(localconfig.Options{CLI: layer(t, &p, &b)})
	if err != nil {
		t.Fatal(err)
	}
	after := second.Summary()
	if before.ProfileDigest != after.ProfileDigest || before.ConfigurationDigest == after.ConfigurationDigest {
		t.Fatal("incorrect reference identity boundary")
	}
	// Layer source class does not change effective artifact identity.
	third, err := localconfig.Resolve(localconfig.Options{User: layer(t, &p, &b)})
	if err != nil {
		t.Fatal(err)
	}
	if third.Summary().ConfigurationDigest != after.ConfigurationDigest || third.Summary().ProfileSource == after.ProfileSource {
		t.Fatal("identity/provenance mismatch")
	}
}
