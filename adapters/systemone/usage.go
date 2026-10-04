package systemone

import (
	"encoding/json"

	"github.com/antaeusio/antaeus/evaluator"
)

// parseUsage accepts only provider-reported counters, independent of answers.
// Unknown fields (for example billing_units) are not copied into the Decision.
func parseUsage(payload []byte) *evaluator.Usage {
	envelope, err := objectFields(payload)
	if err != nil {
		return &evaluator.Usage{Status: evaluator.UsageInvalid}
	}
	raw, exists := envelope["usage"]
	if !exists {
		return &evaluator.Usage{Status: evaluator.UsageUnavailable}
	}
	fields, err := objectFields(raw)
	if err != nil {
		return &evaluator.Usage{Status: evaluator.UsageInvalid}
	}
	input, ok := tokenCount(fields["input_tokens"])
	if !ok {
		return &evaluator.Usage{Status: evaluator.UsageInvalid}
	}
	u := &evaluator.Usage{Status: evaluator.UsageReported, InputTokens: &input}
	if raw, exists := fields["output_tokens"]; exists {
		output, ok := tokenCount(raw)
		if !ok {
			return &evaluator.Usage{Status: evaluator.UsageInvalid}
		}
		u.OutputTokens = &output
	}
	return u
}

func tokenCount(raw json.RawMessage) (int64, bool) {
	var count *int64
	if len(raw) == 0 || json.Unmarshal(raw, &count) != nil || count == nil || *count < 0 || *count > evaluator.MaxUsageTokens {
		return 0, false
	}
	return *count, true
}
