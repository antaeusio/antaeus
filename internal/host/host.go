// Package host implements the engine's JSON interface for embedding hosts,
// such as a JavaScript runtime running the WebAssembly build. Requests and
// responses are JSON documents, so the interface is independent of the host
// language. Configuration problems are returned as typed errors before any
// evaluation starts; accepted evaluations always return a Decision.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/antaeusio/antaeus/adapters/openai"
	"github.com/antaeusio/antaeus/adapters/systemone"
	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
	"github.com/antaeusio/antaeus/internal/strictsource"
	"github.com/antaeusio/antaeus/policy"
)

// InterfaceVersion identifies this request and response format. It changes
// only for incompatible changes.
const InterfaceVersion = 1

// MaxRequestBytes bounds one request document. Policy, profile, and input keep
// their own engine limits inside it.
const MaxRequestBytes = 4 << 20

// Error codes returned before evaluation starts.
const (
	CodeRequestInvalid       = "host.request_invalid"
	CodePolicyInvalid        = "host.policy_invalid"
	CodeProfileInvalid       = "host.profile_invalid"
	CodeInputInvalid         = "host.input_invalid"
	CodeAdapterNotInstalled  = "host.adapter_not_installed"
	CodeCredentialMissing    = "host.credential_missing"
	CodeDeadlineExceeded     = "host.deadline_exceeded"
	CodeEvaluationNotStarted = "host.evaluation_not_started"
	// CodeInternalError reports a failure inside the engine; the call had no
	// effect and later calls are unaffected.
	CodeInternalError = "host.internal_error"
)

// Error is a configuration problem that prevented evaluation. Messages never
// contain credential values.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PolicyIdentity names a validated policy by its canonical digest.
type PolicyIdentity struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Rules  int    `json:"rules"`
}

// Response is the result of every call: exactly one of Policy, Decision, or
// Error is set, and OK is false only with Error.
type Response struct {
	OK       bool               `json:"ok"`
	Policy   *PolicyIdentity    `json:"policy,omitempty"`
	Decision *decision.Decision `json:"decision,omitempty"`
	Error    *Error             `json:"error,omitempty"`
}

// ValidateRequest asks for a policy's identity.
type ValidateRequest struct {
	Policy       string `json:"policy"`
	PolicyFormat string `json:"policyFormat"`
}

// Credential supplies the value for one adapter credential slot. Values are
// used only for the evaluation they are passed with.
type Credential struct {
	AdapterID string `json:"adapterId"`
	Slot      string `json:"slot"`
	Value     string `json:"value"`
}

// EvaluateRequest runs one policy against one input with one profile.
type EvaluateRequest struct {
	Policy        string          `json:"policy"`
	PolicyFormat  string          `json:"policyFormat"`
	Profile       string          `json:"profile"`
	ProfileFormat string          `json:"profileFormat"`
	Input         json.RawMessage `json:"input"`
	CorrelationID string          `json:"correlationId"`
	// DeadlineUnixMS, when set, caps the evaluation at that absolute time in
	// addition to the profile's total timeout. A host that waited before
	// calling passes its original deadline so the wait counts against it.
	DeadlineUnixMS *int64       `json:"deadlineUnixMs,omitempty"`
	Credentials    []Credential `json:"credentials"`
}

// Registry returns the installed semantic adapters: OpenAI and every
// installed System One version. The deterministic fixture is not installed.
func Registry() runner.Registry {
	return runner.Registry{
		openai.Identity:            openai.Registration(),
		systemone.Identity:         systemone.Registration(),
		systemone.PreviousIdentity: systemone.PreviousRegistration(),
		systemone.LegacyIdentity:   systemone.LegacyRegistration(),
	}
}

// Validate parses and validates a policy and returns its identity.
func Validate(request []byte) []byte {
	var r ValidateRequest
	if err := decode(request, &r); err != nil {
		return fail(CodeRequestInvalid, err.Error())
	}
	if !validFormat(r.PolicyFormat) {
		return fail(CodeRequestInvalid, `policyFormat must be "yaml" or "json"`)
	}
	artifact, err := parsePolicy(r.Policy, r.PolicyFormat)
	if err != nil {
		return fail(CodePolicyInvalid, err.Error())
	}
	identity, err := identify(artifact)
	if err != nil {
		return fail(CodePolicyInvalid, err.Error())
	}
	return encode(Response{OK: true, Policy: &identity})
}

// Evaluate runs an evaluation with the given registry, normally Registry().
func Evaluate(ctx context.Context, request []byte, registry runner.Registry) []byte {
	var r EvaluateRequest
	if err := decode(request, &r); err != nil {
		return fail(CodeRequestInvalid, err.Error())
	}
	if !validFormat(r.PolicyFormat) {
		return fail(CodeRequestInvalid, `policyFormat must be "yaml" or "json"`)
	}
	artifact, err := parsePolicy(r.Policy, r.PolicyFormat)
	if err != nil {
		return fail(CodePolicyInvalid, err.Error())
	}
	var p profile.Artifact
	switch r.ProfileFormat {
	case "json":
		p, err = profile.Parse([]byte(r.Profile), profile.FormatJSON)
	case "yaml":
		p, err = profile.Parse([]byte(r.Profile), profile.FormatYAML)
	default:
		return fail(CodeRequestInvalid, `profileFormat must be "json" or "yaml"`)
	}
	if err != nil {
		return fail(CodeProfileInvalid, err.Error())
	}
	if err := checkInstalled(p, registry); err != nil {
		return fail(CodeAdapterNotInstalled, err.Error())
	}
	input, err := jsonvalue.CanonicalObject(r.Input)
	if err != nil {
		return fail(CodeInputInvalid, err.Error())
	}
	credentials, err := preflight(p, r.Credentials)
	if err != nil {
		var missing *localbinding.MissingCredentialError
		if errors.As(err, &missing) {
			return fail(CodeCredentialMissing, err.Error())
		}
		return fail(CodeRequestInvalid, "credentials are invalid for the profile's route")
	}
	defer credentials.Clear()
	if r.DeadlineUnixMS != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(*r.DeadlineUnixMS))
		defer cancel()
	}
	d, err := runner.Run(ctx, runner.Input{
		Policy: artifact, Profile: p, CanonicalInput: input,
		CorrelationID: r.CorrelationID, Credentials: credentials,
	}, registry)
	var missing *localbinding.MissingCredentialError
	switch {
	case err == nil:
	case errors.As(err, &missing):
		return fail(CodeCredentialMissing, err.Error())
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fail(CodeDeadlineExceeded, "the deadline passed before evaluation started")
	default:
		return fail(CodeEvaluationNotStarted, err.Error())
	}
	return encode(Response{OK: true, Decision: &d})
}

// decode accepts exactly one JSON object under the same strict rules as
// policy and profile sources: valid UTF-8, no duplicate object keys at any
// depth, bounded nesting, and nothing after the document. Unknown fields are
// rejected.
func decode(request []byte, target any) error {
	if len(request) > MaxRequestBytes {
		return fmt.Errorf("request exceeds %d bytes", MaxRequestBytes)
	}
	normalized, err := strictsource.Decode(request, strictsource.FormatJSON, MaxRequestBytes, "request")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("request is not a valid JSON object for this call: %w", err)
	}
	return nil
}

func validFormat(format string) bool {
	return format == "yaml" || format == "json"
}

func parsePolicy(source, format string) (policy.Artifact, error) {
	if format == "json" {
		return policy.Parse([]byte(source), policy.FormatJSON)
	}
	return policy.Parse([]byte(source), policy.FormatYAML)
}

func identify(artifact policy.Artifact) (PolicyIdentity, error) {
	digest, err := artifact.Digest()
	if err != nil {
		return PolicyIdentity{}, err
	}
	return PolicyIdentity{Name: artifact.Metadata.Name, Digest: digest, Rules: len(artifact.Spec.Rules)}, nil
}

// checkInstalled rejects a profile before evaluation when it names an adapter
// the registry lacks or an evaluator its adapter would reject, so a
// misconfiguration is an error rather than a failure Decision.
func checkInstalled(p profile.Artifact, registry runner.Registry) error {
	for _, e := range p.Spec.Evaluators {
		if _, ok := registry[e.Adapter]; !ok || e.Mode != profile.ModeSemantic {
			return fmt.Errorf("evaluator %q uses %s@%s, which is not installed in this host", e.ID, e.Adapter.ID, e.Adapter.Version)
		}
		var err error
		switch e.Adapter.ID {
		case systemone.AdapterID:
			err = systemone.ValidateEvaluator(e)
		case openai.AdapterID:
			err = openai.ValidateEvaluator(e)
		}
		if err != nil {
			return fmt.Errorf("evaluator %q: %w", e.ID, err)
		}
	}
	return nil
}

// preflight binds the supplied values through the same checked path as local
// environment bindings. Values for slots the profile's configured route does
// not use are ignored without being validated, so a host may pass a fixed
// credential set. A routed slot without a value is reported by the runner as
// a MissingCredentialError before evaluation starts.
func preflight(p profile.Artifact, supplied []Credential) (*localbinding.Credentials, error) {
	evaluators := map[string]profile.Evaluator{}
	for _, e := range p.Spec.Evaluators {
		evaluators[e.ID] = e
	}
	route := []string{p.Spec.Routing.Primary}
	if p.Spec.Routing.Escalation != nil {
		route = append(route, *p.Spec.Routing.Escalation)
	}
	routed := map[localbinding.Key]bool{}
	for _, id := range append(route, p.Spec.Routing.Fallbacks...) {
		if e := evaluators[id]; e.CredentialSlot != nil {
			routed[localbinding.Key{AdapterID: e.Adapter.ID, Slot: *e.CredentialSlot}] = true
		}
	}
	bindings := localbinding.Artifact{
		APIVersion:     localbinding.APIVersion,
		Kind:           localbinding.Kind,
		SecretBindings: map[string]map[string]localbinding.Reference{},
	}
	values := map[string]string{}
	for i, c := range supplied {
		if !routed[localbinding.Key{AdapterID: c.AdapterID, Slot: c.Slot}] {
			continue
		}
		slots := bindings.SecretBindings[c.AdapterID]
		if slots == nil {
			slots = map[string]localbinding.Reference{}
			bindings.SecretBindings[c.AdapterID] = slots
		}
		if _, duplicate := slots[c.Slot]; duplicate {
			return nil, fmt.Errorf("credentials[%d] repeats adapter %q slot %q", i, c.AdapterID, c.Slot)
		}
		name := "ANTAEUS_HOST_CREDENTIAL_" + strconv.Itoa(i)
		slots[c.Slot] = localbinding.Reference{Source: "environment", Name: name}
		values[name] = c.Value
	}
	if len(values) == 0 {
		return &localbinding.Credentials{}, nil
	}
	return localbinding.Preflight(p, bindings, localbinding.EnvironmentFunc(func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}))
}

// Failure encodes an error response.
func Failure(code, message string) []byte {
	return encode(Response{Error: &Error{Code: code, Message: message}})
}

func fail(code, message string) []byte { return Failure(code, message) }

func encode(r Response) []byte {
	out, err := json.Marshal(r)
	if err != nil {
		out, _ = json.Marshal(Response{Error: &Error{Code: CodeEvaluationNotStarted, Message: "cannot encode the response"}})
	}
	return out
}
