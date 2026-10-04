package runner

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/internal/schematest"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func usageSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/schemas/v0alpha1/usage-report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.UseRegexpEngine(schematest.CompilePattern)
	const url = "https://antaeus.io/contracts/v0alpha1/usage-report.schema.json"
	if err := c.AddResource(url, document); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func usageOf(t *testing.T, d decision.Decision) UsageReport {
	t.Helper()
	traceOf(t, d) // The original execution trace still conforms unchanged.
	var report UsageReport
	raw := d.Extensions[UsageExtension]
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if err := usageSchema(t).Validate(document); err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != d.Evaluator.Attempts {
		t.Fatal("usage/trace count mismatch")
	}
	for i, attempt := range report.Attempts {
		if attempt.TraceIndex != i {
			t.Fatalf("trace index %d at %d", attempt.TraceIndex, i)
		}
	}
	return report
}

func TestUsageAcrossRetryAndFallback(t *testing.T) {
	in := inputFixture(t)
	clock := &fakeTime{current: time.Now()}
	calls := 0
	count := int64(41)
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		switch calls {
		case 1:
			return evaluator.Result{Usage: &evaluator.Usage{Status: "reported", InputTokens: &count}}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true, Message: "PRIVATE_ERROR"}
		case 2:
			count = 99 // A reused adapter pointer must not change the first record.
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
		default:
			result := evidence(r, c, 1)
			result.Usage = &evaluator.Usage{Status: "reported", InputTokens: new(int64(0)), OutputTokens: new(int64(0))}
			return result, nil
		}
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	u := usageOf(t, d)
	if calls != 3 || !d.Evaluator.Fallback || *u.Attempts[0].InputTokens != 41 || u.Attempts[0].AdapterFailureCode != "evaluator.timeout" || u.Attempts[1].Status != "unavailable" || u.Attempts[1].InputTokens != nil || *u.Attempts[2].InputTokens != 0 {
		t.Fatalf("calls %d, decision %+v, usage %+v", calls, d, u)
	}
	if strings.Contains(string(d.Extensions[UsageExtension]), "PRIVATE_ERROR") {
		t.Fatal("error payload leaked")
	}
}

func TestUsageOnFallbackAfterUnreportedPrimary(t *testing.T) {
	in := inputFixture(t)
	d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		if c.Evaluator.ID == in.Profile.Spec.Routing.Primary {
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
		}
		result := evidence(r, c, 1)
		result.Usage = &evaluator.Usage{Status: evaluator.UsageReported, InputTokens: new(int64(41))}
		return result, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	u := usageOf(t, d)
	if len(u.Attempts) != 2 || u.Attempts[0].Status != evaluator.UsageUnavailable || *u.Attempts[1].InputTokens != 41 {
		t.Fatalf("usage %+v", u)
	}
}

func TestUninvokedUsageRouteDoesNotEnableReporting(t *testing.T) {
	in := inputFixture(t)
	calls := 0
	d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		result := evidence(r, c, 1)
		if c.Evaluator.ID != in.Profile.Spec.Routing.Primary {
			result.Usage = &evaluator.Usage{Status: evaluator.UsageReported, InputTokens: new(int64(41))}
		}
		return result, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || d.Extensions[UsageExtension] != nil {
		t.Fatal("unused route enabled accounting")
	}
}

func TestUsageIndependentOfSemanticAndResultFailures(t *testing.T) {
	for _, kind := range []string{"low confidence", "invalid result", "late response", "adapter error"} {
		t.Run(kind, func(t *testing.T) {
			in := inputFixture(t)
			clock := &fakeTime{current: time.Now()}
			d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				result := evidence(r, c, 1)
				result.Usage = &evaluator.Usage{Status: "reported", InputTokens: new(int64(575))}
				switch kind {
				case "low confidence":
					result = evidence(r, c, 0.5)
					result.Usage = &evaluator.Usage{Status: "reported", InputTokens: new(int64(575))}
				case "invalid result":
					result.RuleResults = nil
				case "late response":
					clock.current = clock.current.Add(3 * time.Second)
				case "adapter error":
					return result, &evaluator.Error{Code: "systemone.output_invalid", Message: "PRIVATE_ERROR"}
				}
				return result, nil
			}), clock.timing())
			if err != nil {
				t.Fatal(err)
			}
			u := usageOf(t, d)
			if d.Outcome != decision.OutcomeFailure {
				t.Fatalf("outcome %s", d.Outcome)
			}
			for _, attempt := range u.Attempts {
				if attempt.Status != "reported" || *attempt.InputTokens != 575 {
					t.Fatalf("usage %+v", u)
				}
			}
		})
	}
}

func TestBadUsageDoesNotInvalidateEvidence(t *testing.T) {
	for _, u := range []*evaluator.Usage{
		nil, {Status: "unavailable"}, {Status: "invalid"},
		{Status: "reported"}, {Status: "reported", InputTokens: new(int64(-1))},
		{Status: "reported", InputTokens: new(evaluator.MaxUsageTokens + 1)},
		{Status: "unavailable", InputTokens: new(int64(0))}, {Status: "PRIVATE_ERROR"},
	} {
		in := inputFixture(t)
		d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
			result := evidence(r, c, 1)
			result.Usage = u
			return result, nil
		}))
		if err != nil || d.Outcome == decision.OutcomeFailure {
			t.Fatalf("decision %+v, err %v", d, err)
		}
		if u == nil {
			if _, exists := d.Extensions[UsageExtension]; exists {
				t.Fatal("legacy output acquired a usage extension")
			}
			continue
		}
		report := usageOf(t, d)
		if report.Attempts[0].InputTokens != nil || strings.Contains(string(d.Extensions[UsageExtension]), "PRIVATE_ERROR") {
			t.Fatal("invalid usage escaped normalization")
		}
	}
}

func TestUsageSchemaRejectsInvalidRecords(t *testing.T) {
	schema := usageSchema(t)
	for _, record := range []string{
		`{"traceIndex":0,"status":"reported","inputTokens":0}`,
		`{"traceIndex":63,"status":"reported","inputTokens":9007199254740991,"outputTokens":0}`,
		`{"traceIndex":1,"status":"unavailable","adapterFailureCode":"systemone.output_invalid"}`,
		`{"traceIndex":0,"status":"invalid"}`,
	} {
		var document any
		_ = json.Unmarshal([]byte(`{"version":"v0alpha1","attempts":[`+record+`]}`), &document)
		if err := schema.Validate(document); err != nil {
			t.Fatal(err)
		}
	}
	for _, record := range []string{
		`{"traceIndex":0,"status":"reported"}`,
		`{"traceIndex":0,"status":"reported","inputTokens":null}`,
		`{"traceIndex":0,"status":"reported","inputTokens":-1}`,
		`{"traceIndex":0,"status":"reported","inputTokens":1.5}`,
		`{"traceIndex":0,"status":"reported","inputTokens":9007199254740992}`,
		`{"traceIndex":0,"status":"unavailable","inputTokens":0}`,
		`{"traceIndex":0,"status":"invalid","outputTokens":0}`,
		`{"traceIndex":64,"status":"unavailable"}`,
		`{"traceIndex":0,"status":"unknown"}`,
		`{"traceIndex":0,"status":"unavailable","adapterFailureCode":"PRIVATE ERROR"}`,
		`{"traceIndex":0,"status":"unavailable","raw":"secret"}`,
	} {
		var document any
		_ = json.Unmarshal([]byte(`{"version":"v0alpha1","attempts":[`+record+`]}`), &document)
		if schema.Validate(document) == nil {
			t.Fatalf("accepted %s", record)
		}
	}
}
