# Provider usage reporting

System One adapter `0.4.0` reports Drex input-token counts and optional
output-token counts through `Decision.extensions["io.antaeus.usage"]`.
Other System One providers report `unavailable` until their token-accounting
contracts are verified.
Select it explicitly in the evaluator profile; changing the adapter version
changes the profile digest. Versions `0.1.0` through `0.3.0` keep their
original behavior and do not report usage. Go callers register
`systemone.UsageIdentity: systemone.UsageRegistration()`; the CLI and JSON
WebAssembly host install it alongside the earlier versions.

```json
{
  "version": "v0alpha1",
  "attempts": [
    {"traceIndex": 0, "status": "unavailable", "adapterFailureCode": "evaluator.throttled"},
    {"traceIndex": 1, "status": "reported", "inputTokens": 575, "outputTokens": 48}
  ]
}
```

The authoritative shape is
[`usage-report.schema.json`](../contracts/schemas/v0alpha1/usage-report.schema.json).
`traceIndex` is the zero-based index into the unchanged
`io.antaeus.execution.attempts` array. When at least one invoked adapter
reports usage support, this extension contains exactly one record per trace
attempt, in the same order. Uninvoked routes have no record. Schema validation
checks each record; consumers must additionally verify the count, order, and
index relationship against the execution trace.

Each record has one accounting status:

- `reported`: valid provider `usage.input_tokens`, with optional
  `usage.output_tokens`. A reported zero is a known zero. Output may be absent
  without making input accounting unknown.
- `unavailable`: no usage reporting from that adapter, no `usage` field, or
  no readable HTTP 200 response (including timeouts, transport failures, and
  non-200 responses). There are no counters.
- `invalid`: the response envelope, a present usage object, or a counter failed validation. There are
  no counters. The adapter accepts nonnegative decimal integer JSON numbers
  through 9,007,199,254,740,991; strings, nulls, fractional values, unsafe
  integers, exponent or decimal-point spellings, and repeated object keys are
  rejected.

Unknown accounting is never estimated from input bytes, rule count, output
length, or another attempt. There is no aggregate total that can conceal an
unknown attempt. Sum reported input counts across attempts only as known
provider consumption; a full input total is known only when every attempt is
reported. Output totals additionally require every reported output counter.

Usage survives an invalid answer, low-confidence routing, retry, escalation,
fallback, or an already-read response rejected for missing its deadline.
Unread responses and engine panics cannot provide counters. The runner copies
each counter before a later attempt can reuse adapter-owned storage. A safe,
bounded adapter error code may accompany a record; error messages, credentials,
raw input, and raw provider responses are never copied into this extension.
The execution trace records normalized routing failures, such as
`evaluation.adapter_failed`; `adapterFailureCode` preserves the underlying
classification, such as `systemone.output_invalid`, needed for diagnostics.
Go adapter authors must use fixed public identifiers for `Error.Code`; once
usage reporting is enabled, shape-valid codes from every invoked adapter may
be published verbatim, including adapters without token reporting.

Usage validation does not change rule evidence, routing, or the terminal
Decision. This is provider accounting, not a price, balance, or charge:
applications decide which outcomes and attempts are billable. Incomplete
usage must remain visibly incomplete in any accounting workflow.

The Decision's existing reverse-DNS extension mechanism permits this additive
contract without changing the closed execution trace or the version-1 JSON
host interface. Consumers that require usage must select a reporting adapter
and validate this extension explicitly; older profiles do not acquire it.
