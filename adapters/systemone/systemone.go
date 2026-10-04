// Package systemone implements the System One protocol semantic evaluator.
//
// System One servers answer typed questions about a state with probability
// distributions. The adapter asks one "noul" (yes/no) question per policy
// rule, whose instructions are the rule's condition, and normalizes each
// probability into provider-neutral rule evidence with a confidence score.
// Rule outcomes are never sent.
//
// Version 0.3.0 supports three providers: Antaeus System One servers, such as
// a self-hosted antaeusio/nli-server, the self-hosted Contrastive Language
// Model (CLM) reference server, and the hosted Drex API. It also retries HTTP
// 529 and reports provider Retry-After waits to the runner. Versions 0.2.0
// (Antaeus and CLM) and 0.1.0 (CLM only) stay installed with their original
// behavior so existing profiles keep working.
// Version 0.4.0 is an opt-in sibling that reports Drex token accounting;
// Identity and Registration remain on 0.3.0 for existing Go callers.
// Other System One providers are rejected until their wire contracts are
// verified.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/internal/remote"
)

const (
	AdapterID      = "io.antaeus.systemone"
	AdapterVersion = "0.3.0"
	// UsageAdapterVersion opts into provider token accounting. Existing
	// identities and Registration retain their original behavior.
	UsageAdapterVersion = "0.4.0"
	// PreviousAdapterVersion supports the antaeus and CLM providers.
	PreviousAdapterVersion = "0.2.0"
	// LegacyAdapterVersion is the first version, which supports CLM only.
	LegacyAdapterVersion = "0.1.0"
	// ProviderAntaeus is an Antaeus System One server, such as a self-hosted
	// antaeusio/nli-server.
	ProviderAntaeus = "antaeus"
	// ProviderCLM is the self-hosted Contrastive Language Model server.
	ProviderCLM = "contrastive-lm"
	// ProviderDrex is the hosted Drex API by Nace.AI, available only at
	// DrexEndpoint. It always requires a credential.
	ProviderDrex = "drex"
	DrexEndpoint = "https://drex.nace.ai"
	// Credential slots are optional for self-hosted servers: a server without
	// an API key needs none.
	AntaeusCredentialSlot     = "antaeus-api-key"
	AntaeusCredentialVariable = "ANTAEUS_API_KEY"
	CredentialSlot            = "clm-api-key"
	DefaultCredentialVariable = "CLM_API_KEY"
	DrexCredentialSlot        = "drex-api-key"
	DrexCredentialVariable    = "DREX_API_KEY"

	MaxRequestBytes  = 4 << 20
	MaxResponseBytes = 1 << 20
	MaxEndpointBytes = 2048
)

// Protocol is the provider-neutral protocol this adapter implements.
var Protocol = profile.ComponentIdentity{ID: "io.antaeus.rule-match", Version: "v0alpha1"}

// Identity is the current installed adapter identity.
var Identity = profile.ComponentIdentity{ID: AdapterID, Version: AdapterVersion}

// UsageIdentity is the installed usage-reporting 0.4.0 identity.
var UsageIdentity = profile.ComponentIdentity{ID: AdapterID, Version: UsageAdapterVersion}

// PreviousIdentity is the installed 0.2.0 identity.
var PreviousIdentity = profile.ComponentIdentity{ID: AdapterID, Version: PreviousAdapterVersion}

// LegacyIdentity is the installed CLM-only 0.1.0 identity.
var LegacyIdentity = profile.ComponentIdentity{ID: AdapterID, Version: LegacyAdapterVersion}

// providerSlots maps each adapter version's providers to their credential slot.
var providerSlots = map[string]map[string]string{
	LegacyAdapterVersion:   {ProviderCLM: CredentialSlot},
	PreviousAdapterVersion: {ProviderAntaeus: AntaeusCredentialSlot, ProviderCLM: CredentialSlot},
	AdapterVersion:         {ProviderAntaeus: AntaeusCredentialSlot, ProviderCLM: CredentialSlot, ProviderDrex: DrexCredentialSlot},
	UsageAdapterVersion:    {ProviderAntaeus: AntaeusCredentialSlot, ProviderCLM: CredentialSlot, ProviderDrex: DrexCredentialSlot},
}

// Capabilities includes confidence-scores: every answer carries a probability.
var Capabilities = []string{"json-input", "structured-rule-results", "confidence-scores"}

// DefaultReferences returns the documented local credential defaults of the
// current version. A default is used only when a profile declares its slot.
func DefaultReferences() map[string]localbinding.Reference {
	return map[string]localbinding.Reference{
		AntaeusCredentialSlot: {Source: "environment", Name: AntaeusCredentialVariable},
		CredentialSlot:        {Source: "environment", Name: DefaultCredentialVariable},
		DrexCredentialSlot:    {Source: "environment", Name: DrexCredentialVariable},
	}
}

// PreviousDefaultReferences returns the credential defaults of version 0.2.0.
func PreviousDefaultReferences() map[string]localbinding.Reference {
	return map[string]localbinding.Reference{
		AntaeusCredentialSlot: {Source: "environment", Name: AntaeusCredentialVariable},
		CredentialSlot:        {Source: "environment", Name: DefaultCredentialVariable},
	}
}

// LegacyDefaultReferences returns the credential default of version 0.1.0.
func LegacyDefaultReferences() map[string]localbinding.Reference {
	return map[string]localbinding.Reference{CredentialSlot: {Source: "environment", Name: DefaultCredentialVariable}}
}

// Registration returns the current adapter for runner.Registry.
func Registration() runner.Adapter {
	return registration(remote.NewClient(), AdapterVersion)
}

// UsageRegistration returns the opt-in usage-reporting 0.4.0 adapter.
func UsageRegistration() runner.Adapter {
	return registration(remote.NewClient(), UsageAdapterVersion)
}

// PreviousRegistration returns the 0.2.0 adapter for runner.Registry.
func PreviousRegistration() runner.Adapter {
	return registration(remote.NewClient(), PreviousAdapterVersion)
}

// LegacyRegistration returns the CLM-only 0.1.0 adapter for runner.Registry.
func LegacyRegistration() runner.Adapter {
	return registration(remote.NewClient(), LegacyAdapterVersion)
}

func registration(client *http.Client, version string) runner.Adapter {
	a := &adapter{client: client, version: version, now: time.Now}
	return runner.Adapter{
		Mode:         profile.ModeSemantic,
		Protocol:     Protocol,
		Capabilities: slices.Clone(Capabilities),
		Parameters:   parameterValidator{},
		Evaluate:     a.evaluate,
	}
}

// Parameters is the closed adapter parameter object carried by the profile.
type Parameters struct {
	// Endpoint is the server's base URL, for example https://evaluator.internal.example
	// or http://127.0.0.1:8080. The adapter appends /v1/systemone.
	Endpoint string `json:"endpoint"`
}

type parameterValidator struct{}

func (parameterValidator) ValidateParameters(raw json.RawMessage) error {
	_, err := parseParameters(raw)
	return err
}

func parseParameters(raw json.RawMessage) (Parameters, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Parameters{}, errors.New("parameters must be a JSON object")
	}
	for name := range fields {
		if name != "endpoint" {
			return Parameters{}, fmt.Errorf("parameter %q is not supported", name)
		}
	}
	var p Parameters
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return Parameters{}, errors.New("parameters have invalid types")
	}
	if _, err := endpointURL(p.Endpoint); err != nil {
		return Parameters{}, err
	}
	return p, nil
}

// endpointURL validates the configured base URL and returns the request URL.
// HTTPS is required except for loopback hosts. User information, queries,
// and fragments are rejected so no secret or behavior can hide in the URL.
func endpointURL(endpoint string) (string, error) {
	if endpoint == "" || len(endpoint) > MaxEndpointBytes {
		return "", fmt.Errorf("endpoint must be a URL of at most %d bytes", MaxEndpointBytes)
	}
	u, err := url.Parse(endpoint)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return "", errors.New("endpoint must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(endpoint, "#") {
		return "", errors.New("endpoint must not contain user information, a query, or a fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", errors.New("endpoint must use https unless the host is loopback")
		}
	default:
		return "", errors.New("endpoint must be an absolute http or https URL")
	}
	return strings.TrimSuffix(u.String(), "/") + "/v1/systemone", nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)

// ValidateEvaluator checks the profile fields this adapter requires, for
// any installed version. Run it before accepting an evaluation so a
// misconfigured profile is rejected rather than turned into a failure Decision.
func ValidateEvaluator(e profile.Evaluator) error {
	if (e.Adapter != UsageIdentity && e.Adapter != Identity && e.Adapter != PreviousIdentity && e.Adapter != LegacyIdentity) || e.Mode != profile.ModeSemantic || e.Protocol != Protocol {
		return errors.New("evaluator is not configured for " + AdapterID + "@" + UsageAdapterVersion + ", @" + AdapterVersion + ", @" + PreviousAdapterVersion + ", or @" + LegacyAdapterVersion)
	}
	providers := providerSlots[e.Adapter.Version]
	if e.Provider == nil {
		return errors.New("system one evaluators require a provider")
	}
	slot, ok := providers[*e.Provider]
	if !ok {
		switch e.Adapter.Version {
		case LegacyAdapterVersion:
			return errors.New(AdapterID + "@" + LegacyAdapterVersion + ` supports only provider "` + ProviderCLM + `"`)
		case PreviousAdapterVersion:
			return errors.New(AdapterID + "@" + PreviousAdapterVersion + ` supports only providers "` + ProviderAntaeus + `" and "` + ProviderCLM + `"`)
		}
		return errors.New(`system one evaluators support only providers "` + ProviderAntaeus + `", "` + ProviderCLM + `", and "` + ProviderDrex + `"`)
	}
	if e.Model == nil {
		return errors.New("system one evaluators require an explicit model, the name the server serves")
	}
	if e.ModelRevision != nil {
		return errors.New("system one evaluators do not support modelRevision")
	}
	if e.InstructionTemplate != nil {
		return errors.New("system one evaluators send no instruction template; remove instructionTemplate")
	}
	if e.CredentialSlot != nil && *e.CredentialSlot != slot {
		return errors.New(`provider "` + *e.Provider + `" accepts only credentialSlot "` + slot + `"`)
	}
	if *e.Provider == ProviderDrex && e.CredentialSlot == nil {
		return errors.New(`provider "` + ProviderDrex + `" requires credentialSlot "` + DrexCredentialSlot + `"`)
	}
	raw, err := json.Marshal(e.Parameters)
	if err != nil {
		return errors.New("parameters are invalid")
	}
	p, err := parseParameters(raw)
	if err != nil {
		return err
	}
	// The Drex key is a hosted-service credential; never send it elsewhere.
	if *e.Provider == ProviderDrex {
		if target, _ := endpointURL(p.Endpoint); target != DrexEndpoint+"/v1/systemone" {
			return errors.New(`provider "` + ProviderDrex + `" requires endpoint "` + DrexEndpoint + `"`)
		}
	}
	return nil
}

type adapter struct {
	client  *http.Client
	version string
	now     func() time.Time
}

func failure(code string, retryable bool, message string) error {
	return &evaluator.Error{Code: code, Retryable: retryable, Message: message}
}

type question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type requestBody struct {
	State     json.RawMessage     `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

func (a *adapter) evaluate(ctx context.Context, request evaluator.Request, config runner.Configuration) (result evaluator.Result, evaluationErr error) {
	// Unknown accounting remains visible even on transport/status failures.
	// Earlier versions continue to ignore usage, including malformed counters.
	if a.version == UsageAdapterVersion {
		defer func() {
			if result.Usage == nil {
				result.Usage = &evaluator.Usage{Status: evaluator.UsageUnavailable}
			}
		}()
	}
	if err := ValidateEvaluator(config.Evaluator); err != nil {
		return evaluator.Result{}, failure("systemone.configuration_invalid", false, err.Error())
	}
	if config.Evaluator.Adapter.Version != a.version {
		return evaluator.Result{}, failure("systemone.configuration_invalid", false, "evaluator is not configured for "+AdapterID+"@"+a.version)
	}
	// Values are passed unmodified; a credential that cannot form a header is
	// a configuration error, not a provider outage.
	for _, b := range config.Credential {
		if b < 0x21 || b > 0x7e {
			return evaluator.Result{}, failure("systemone.credential_invalid", false, "credential contains characters that are not valid in a bearer token")
		}
	}
	if config.Evaluator.CredentialSlot != nil && len(config.Credential) == 0 {
		return evaluator.Result{}, failure("systemone.credential_missing", false, "credential is not available")
	}
	if err := request.Validate(); err != nil {
		return evaluator.Result{}, failure("systemone.request_invalid", false, err.Error())
	}
	raw, _ := json.Marshal(config.Evaluator.Parameters)
	parameters, _ := parseParameters(raw)
	target, _ := endpointURL(parameters.Endpoint)

	questions := make(map[string]question, len(request.Rules))
	for _, rule := range request.Rules {
		questions[rule.ID] = question{Type: "noul", Instructions: rule.When}
	}
	body, err := json.Marshal(requestBody{State: request.CanonicalInput, Model: *config.Evaluator.Model, Questions: questions})
	if err != nil {
		return evaluator.Result{}, failure("systemone.request_invalid", false, "cannot encode provider request")
	}
	if len(body) > MaxRequestBytes {
		return evaluator.Result{}, failure("systemone.request_too_large", false, "provider request exceeded the size limit")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return evaluator.Result{}, failure("systemone.request_invalid", false, "cannot construct provider request")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if len(config.Credential) > 0 {
		httpRequest.Header.Set("Authorization", "Bearer "+string(config.Credential))
	}

	response, err := a.client.Do(httpRequest)
	if err != nil {
		return evaluator.Result{}, remote.TransportFailure(ctx, err, "systemone")
	}
	defer func() { _ = response.Body.Close() }()
	signals := reportsProviderSignals(a.version)
	if response.StatusCode != http.StatusOK {
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return evaluator.Result{}, failure("systemone.response_malformed", false, "provider returned an unexpected success status")
		}
		err := statusFailure(response.StatusCode, signals)
		if failure, ok := err.(*evaluator.Error); ok && signals && failure.Retryable {
			failure.RetryAfter = retryAfter(response.Header, a.now())
		}
		return evaluator.Result{}, err
	}
	payload, tooLarge, err := remote.ReadBounded(response.Body, MaxResponseBytes)
	if err != nil {
		return evaluator.Result{}, remote.TransportFailure(ctx, err, "systemone")
	}
	if tooLarge {
		return evaluator.Result{}, failure("systemone.response_too_large", false, "provider response exceeded the size limit")
	}
	var usage *evaluator.Usage
	if a.version == UsageAdapterVersion && *config.Evaluator.Provider == ProviderDrex {
		usage = parseUsage(payload)
	}
	results, model, err := parseResponse(payload, request.Rules)
	if err != nil {
		return evaluator.Result{Usage: usage}, err
	}
	var requestID string
	if signals {
		requestID = safeRequestID(response.Header.Get("X-Request-Id"))
	}
	return evaluator.Result{
		RuleResults: results,
		Usage:       usage,
		Metadata: evaluator.Metadata{
			AdapterID:      AdapterID,
			AdapterVersion: a.version,
			Mode:           evaluator.ModeSemantic,
			Provider:       *config.Evaluator.Provider,
			Model:          model,
			RequestID:      requestID,
		},
	}, nil
}

// objectFields decodes a JSON object into its members with exact,
// case-sensitive keys. Unlike decoding into a map, a repeated key is an error
// instead of silently keeping the last value.
func objectFields(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid object key")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("repeated object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON object")
	}
	return fields, nil
}

func parseResponse(payload []byte, rules []evaluator.Rule) ([]evaluator.RuleResult, string, error) {
	envelope, err := objectFields(payload)
	if err != nil {
		return nil, "", failure("systemone.response_malformed", false, "provider response is not a System One answer object")
	}
	answers, err := objectFields(envelope["answers"])
	if err != nil {
		return nil, "", failure("systemone.response_malformed", false, "provider response is not a System One answer object")
	}
	if len(answers) != len(rules) {
		return nil, "", failure("systemone.output_invalid", false, "provider response does not answer every requested rule exactly once")
	}
	results := make([]evaluator.RuleResult, len(rules))
	for i, rule := range rules {
		raw, ok := answers[rule.ID]
		if !ok {
			return nil, "", failure("systemone.output_invalid", false, "provider response does not answer every requested rule exactly once")
		}
		answer, err := objectFields(raw)
		var kind string
		var p float64
		if err != nil || len(answer) != 2 || json.Unmarshal(answer["type"], &kind) != nil || kind != "noul" ||
			answer["noul"] == nil || string(answer["noul"]) == "null" || json.Unmarshal(answer["noul"], &p) != nil {
			return nil, "", failure("systemone.output_invalid", false, "provider answer is not a noul probability")
		}
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
			return nil, "", failure("systemone.output_invalid", false, "provider probability is not between 0 and 1")
		}
		status := decision.RuleIndeterminate
		switch {
		case p > 0.5:
			status = decision.RuleMatched
		case p < 0.5:
			status = decision.RuleNotMatched
		}
		confidence := math.Max(p, 1-p)
		results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: status, Confidence: &confidence, ReasonCodes: []string{"systemone." + string(status)}}
	}
	var model string
	if json.Unmarshal(envelope["model"], &model) != nil || !modelPattern.MatchString(model) {
		model = ""
	}
	return results, model, nil
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func safeRequestID(value string) string {
	if requestIDPattern.MatchString(value) {
		return value
	}
	return ""
}

// statusOverloaded is the nonstandard HTTP 529 some providers, including Drex,
// return when at capacity.
const statusOverloaded = 529

// maxRetryAfter bounds a provider-stated wait. Any longer wait already exceeds
// every evaluation deadline, so the runner skips the retry either way.
const maxRetryAfter = 24 * time.Hour

// retryAfter reads the provider-required wait before another attempt:
// retry-after-ms (milliseconds) first, then the standard Retry-After, which is
// either delay-seconds or an HTTP-date (RFC 9110 section 10.2.3). Malformed or
// negative values count as absent; a past date means no wait.
func retryAfter(header http.Header, now time.Time) time.Duration {
	if value := strings.TrimSpace(header.Get("Retry-After-Ms")); value != "" {
		if ms, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(ms) && !math.IsInf(ms, 0) && ms >= 0 {
			return time.Duration(math.Min(ms, float64(maxRetryAfter.Milliseconds())) * float64(time.Millisecond))
		}
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if strings.Trim(value, "0123456789") == "" {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return min(max(date.Sub(now), 0), maxRetryAfter)
	}
	return 0
}

// reportsProviderSignals reports whether a version maps HTTP 529, carries
// provider Retry-After waits, and records request IDs. Versions 0.1.0 and
// 0.2.0 predate these behaviors and keep their original handling; 0.3.0 and
// every later version keep them.
func reportsProviderSignals(version string) bool {
	return version != LegacyAdapterVersion && version != PreviousAdapterVersion
}

// statusFailure classifies a non-success HTTP status. signals enables the
// 0.3.0 mapping of HTTP 529 (overloaded) to a retryable unavailable failure.
func statusFailure(status int, signals bool) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure("systemone.credential_rejected", false, fmt.Sprintf("provider rejected the credential (HTTP %d)", status))
	case status == http.StatusRequestTimeout:
		return failure("evaluator.timeout", true, "provider request timed out (HTTP 408)")
	case status == http.StatusTooManyRequests:
		return failure("evaluator.throttled", true, "provider rate limit reached (HTTP 429)")
	case status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity:
		return failure("systemone.request_rejected", false, fmt.Sprintf("provider rejected the request (HTTP %d)", status))
	case status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || (signals && status == statusOverloaded):
		return failure("evaluator.unavailable", true, fmt.Sprintf("provider is unavailable (HTTP %d)", status))
	}
	return failure("systemone.unexpected_status", false, fmt.Sprintf("provider returned unexpected HTTP %d", status))
}
