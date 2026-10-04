package runner

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/antaeusio/antaeus/evaluator"
)

const UsageExtension = "io.antaeus.usage"

// UsageReport joins provider accounting to execution-trace attempts by index.
// It is emitted only when at least one invoked adapter reports usage support.
// There is no total: unknown attempts must never be silently counted as zero.
type UsageReport struct {
	Version  string         `json:"version"`
	Attempts []UsageAttempt `json:"attempts"`
}

type UsageAttempt struct {
	TraceIndex         int                   `json:"traceIndex"`
	AdapterFailureCode string                `json:"adapterFailureCode,omitempty"`
	Status             evaluator.UsageStatus `json:"status"`
	InputTokens        *int64                `json:"inputTokens,omitempty"`
	OutputTokens       *int64                `json:"outputTokens,omitempty"`
}

var adapterFailureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)

// appendAttempt owns both arrays so their index relationship cannot drift.
func (x *execution) appendAttempt(attempt Attempt, usage *evaluator.Usage, err error) {
	u := evaluator.Usage{Status: evaluator.UsageUnavailable}
	if usage != nil {
		x.usageEnabled = true
		u = normalizedUsage(*usage)
	}
	record := UsageAttempt{TraceIndex: len(x.trace.Attempts), Status: u.Status, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
	var failure *evaluator.Error
	if errors.As(err, &failure) && failure != nil && adapterFailureCodePattern.MatchString(failure.Code) {
		record.AdapterFailureCode = failure.Code
	}
	x.usage = append(x.usage, record)
	x.trace.Attempts = append(x.trace.Attempts, attempt)
}

// Bad accounting cannot change a semantic result or enter the public record.
// Copy counters so adapter-owned pointers cannot alter earlier attempts.
func normalizedUsage(u evaluator.Usage) evaluator.Usage {
	valid := func(n *int64) bool { return n != nil && *n >= 0 && *n <= evaluator.MaxUsageTokens }
	if u.Status == evaluator.UsageReported && valid(u.InputTokens) && (u.OutputTokens == nil || valid(u.OutputTokens)) {
		input := *u.InputTokens
		u.InputTokens = &input
		if u.OutputTokens != nil {
			output := *u.OutputTokens
			u.OutputTokens = &output
		}
		return u
	}
	if (u.Status == evaluator.UsageUnavailable || u.Status == evaluator.UsageInvalid) && u.InputTokens == nil && u.OutputTokens == nil {
		return u
	}
	return evaluator.Usage{Status: evaluator.UsageInvalid}
}

func (x *execution) usageJSON() (json.RawMessage, error) {
	return json.Marshal(UsageReport{Version: "v0alpha1", Attempts: x.usage})
}
