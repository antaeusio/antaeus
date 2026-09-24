// Package systemone implements the System One protocol semantic evaluator.
//
// System One servers answer typed questions about a state with probability
// distributions. The adapter asks one "noul" (yes/no) question per policy
// rule, whose instructions are the rule's condition, and normalizes each
// probability into provider-neutral rule evidence with a confidence score.
// Rule outcomes are never sent.
//
// Version 0.1.0 supports the self-hosted Contrastive Language Model (CLM)
// reference server. Other System One providers are rejected until their wire
// contracts are verified.
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
	"strings"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/internal/remote"
)

const (
	AdapterID      = "io.antaeus.systemone"
	AdapterVersion = "0.1.0"
	// ProviderCLM is the self-hosted Contrastive Language Model server.
	ProviderCLM = "contrastive-lm"
	// CredentialSlot is optional: a CLM server without an API key needs none.
	CredentialSlot            = "clm-api-key"
	DefaultCredentialVariable = "CLM_API_KEY"

	MaxRequestBytes  = 4 << 20
	MaxResponseBytes = 1 << 20
	MaxEndpointBytes = 2048
)

// Protocol is the provider-neutral protocol this adapter implements.
var Protocol = profile.ComponentIdentity{ID: "io.antaeus.rule-match", Version: "v0alpha1"}

// Identity is the exact installed adapter identity.
var Identity = profile.ComponentIdentity{ID: AdapterID, Version: AdapterVersion}

// Capabilities includes confidence-scores: every answer carries a probability.
var Capabilities = []string{"json-input", "structured-rule-results", "confidence-scores"}

// DefaultReferences returns the documented local credential default. It is
// used only when a profile declares the optional credential slot.
func DefaultReferences() map[string]localbinding.Reference {
	return map[string]localbinding.Reference{CredentialSlot: {Source: "environment", Name: DefaultCredentialVariable}}
}

// Registration returns the installed adapter for runner.Registry.
func Registration() runner.Adapter {
	return registration(remote.NewClient())
}

func registration(client *http.Client) runner.Adapter {
	a := &adapter{client: client}
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
	// Endpoint is the server's base URL, for example https://clm.internal.example
	// or http://127.0.0.1:8700. The adapter appends /v1/systemone.
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

// ValidateEvaluator checks the profile fields this adapter requires. Run it
// before accepting an evaluation so a misconfigured profile is rejected
// rather than turned into a failure Decision.
func ValidateEvaluator(e profile.Evaluator) error {
	if e.Adapter != Identity || e.Mode != profile.ModeSemantic || e.Protocol != Protocol {
		return errors.New("evaluator is not configured for " + AdapterID + "@" + AdapterVersion)
	}
	if e.Provider == nil || *e.Provider != ProviderCLM {
		return errors.New(`system one evaluators currently support only provider "` + ProviderCLM + `"`)
	}
	if e.Model == nil {
		return errors.New("system one evaluators require an explicit model, for example clm-latest")
	}
	if e.ModelRevision != nil {
		return errors.New("system one evaluators do not support modelRevision")
	}
	if e.InstructionTemplate != nil {
		return errors.New("system one evaluators send no instruction template; remove instructionTemplate")
	}
	if e.CredentialSlot != nil && *e.CredentialSlot != CredentialSlot {
		return errors.New(`system one evaluators accept only credentialSlot "` + CredentialSlot + `"`)
	}
	raw, err := json.Marshal(e.Parameters)
	if err != nil {
		return errors.New("parameters are invalid")
	}
	_, err = parseParameters(raw)
	return err
}

type adapter struct {
	client *http.Client
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

func (a *adapter) evaluate(ctx context.Context, request evaluator.Request, config runner.Configuration) (evaluator.Result, error) {
	if err := ValidateEvaluator(config.Evaluator); err != nil {
		return evaluator.Result{}, failure("systemone.configuration_invalid", false, err.Error())
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
	if response.StatusCode != http.StatusOK {
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return evaluator.Result{}, failure("systemone.response_malformed", false, "provider returned an unexpected success status")
		}
		return evaluator.Result{}, statusFailure(response.StatusCode)
	}
	payload, tooLarge, err := remote.ReadBounded(response.Body, MaxResponseBytes)
	if err != nil {
		return evaluator.Result{}, remote.TransportFailure(ctx, err, "systemone")
	}
	if tooLarge {
		return evaluator.Result{}, failure("systemone.response_too_large", false, "provider response exceeded the size limit")
	}
	results, model, err := parseResponse(payload, request.Rules)
	if err != nil {
		return evaluator.Result{}, err
	}
	return evaluator.Result{
		RuleResults: results,
		Metadata: evaluator.Metadata{
			AdapterID:      AdapterID,
			AdapterVersion: AdapterVersion,
			Mode:           evaluator.ModeSemantic,
			Provider:       ProviderCLM,
			Model:          model,
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

func statusFailure(status int) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure("systemone.credential_rejected", false, fmt.Sprintf("provider rejected the credential (HTTP %d)", status))
	case status == http.StatusRequestTimeout:
		return failure("evaluator.timeout", true, "provider request timed out (HTTP 408)")
	case status == http.StatusTooManyRequests:
		return failure("evaluator.throttled", true, "provider rate limit reached (HTTP 429)")
	case status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity:
		return failure("systemone.request_rejected", false, fmt.Sprintf("provider rejected the request (HTTP %d)", status))
	case status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		return failure("evaluator.unavailable", true, fmt.Sprintf("provider is unavailable (HTTP %d)", status))
	}
	return failure("systemone.unexpected_status", false, fmt.Sprintf("provider returned unexpected HTTP %d", status))
}
