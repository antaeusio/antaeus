# OpenAI semantic adapter

`io.antaeus.openai@0.1.0` is the first installed semantic evaluator. It asks an
OpenAI model, through the Responses API with strict structured output, whether
each policy condition applies to an input. Antaeus then attaches the policy's
own outcomes and reduces them deterministically, as for every evaluator.

> **Status: experimental.** The adapter has not been evaluated on a frozen
> corpus. Its answers are not a claim of accuracy, calibration, or suitability
> for enforcement. Use it for local experimentation and shadow comparison.
> Every evaluation sends the policy conditions and the input to OpenAI and may
> be billed to your account. Send only data you are permitted to share with
> OpenAI under your account's retention and data controls.

## Quick run

With an OpenAI API key exported in your shell (Antaeus never loads `.env` files):

```sh
export OPENAI_API_KEY=...   # your own key; never commit it
scripts/build
.tmp/bin/antaeus evaluate-profile \
  --profile examples/openai/profile.json \
  --policy contracts/examples/v0alpha1/policy/vendor-onboarding.yaml \
  --input examples/openai/prohibited-service.json
```

The command prints one Decision. `evaluator.mode` is `semantic`,
`evaluator.synthetic` is `false`, and `evaluator.model` is the model OpenAI
reports. The execution trace records each attempt's latency and outcome code.
Every Decision outcome exits 0, so inspect `outcome`.

An input that lacks the information a condition needs produces an
`indeterminate` rule. Under v0alpha1 profiles, unresolved rules end in a
`failure` Decision unless a matched deny takes precedence. The quickstart's
`aggregate-analytics.json` input, for example, does not state whether analytics
is a prohibited category, so a model may reasonably answer `indeterminate`.

## Profile requirements

See [`examples/openai/profile.json`](../examples/openai/profile.json). Every
evaluator using this adapter must declare:

| Field | Requirement |
| --- | --- |
| `mode` | `semantic` |
| `adapter` | `{"id": "io.antaeus.openai", "version": "0.1.0"}` |
| `protocol` | `{"id": "io.antaeus.rule-match", "version": "v0alpha1"}` |
| `requiredCapabilities` | a subset of `json-input`, `structured-rule-results` |
| `provider` | `openai` |
| `model` | an explicit model; prefer a dated snapshot such as `gpt-5.4-mini-2026-03-17` |
| `modelRevision` | omitted; the dated snapshot in `model` is the pinned identity |
| `instructionTemplate.digest` | exactly the adapter's template digest (below) |
| `credentialSlot` | `openai-api-key` |
| `parameters.maxOutputTokens` | required integer from 16 through 128000 |
| `parameters.reasoningEffort` | optional: `none`, `minimal`, `low`, `medium`, `high`, or `xhigh` |

No other parameters are accepted. Mutable aliases such as `gpt-5.4-mini` are
accepted for exploration, but the provider can change what they resolve to, so
Decisions made with them are not exactly reproducible. The adapter does not
declare `confidence-scores`: it never asks the model to invent a numeric
confidence, so confidence routing cannot use it.

The instruction template digest is
`sha256:2074738685823fbc700b33fbf0cdd132a5ecff0df5b6efa9db0da74c1618ddfc`, the
SHA-256 of `openai.Instructions`. Pinning it makes the exact prompt part of the
profile identity. Any change to the template requires a new adapter version.
The CLI rejects a profile with a different digest before reading credentials.

## Credentials

The adapter declares one slot, `openai-api-key`. Its installed default
reference is the `OPENAI_API_KEY` process environment variable. An explicit
bindings artifact can bind the slot to another variable name, and explicit
bindings take precedence over the default. The variable is read once,
immediately before evaluation, passed only to the invoked attempt, and cleared
afterwards. It never appears in output, traces, or diagnostics. Missing
credentials fail before any provider call and never fall back to another
evaluator.

A project's `.antaeus/config.json` that selects a credential-reading
configuration requires saved project trust
(`antaeus config inspect`, then `antaeus config trust --digest ...`), as
described in [CLI configuration](./cli-configuration.md). Explicit `--profile`
selections that rely only on the adapter default need no project trust.
`antaeus config check --profile examples/openai/profile.json` verifies that the
credential is present without calling OpenAI.

## Request boundary

Each attempt makes one `POST https://api.openai.com/v1/responses` request with:

- `store: false`, no tools, no background mode, no previous-response or
  conversation state, and no streaming;
- the fixed instruction template, which tells the model that all conditions and
  input values are untrusted data, never instructions;
- one user message containing a JSON document with the policy name, the ordered
  rule IDs and conditions, and the canonical input. Rule outcomes (`allow`,
  `review`, `deny`) are never sent;
- a strict JSON schema whose `ruleId` enum is exactly the requested rule IDs and
  whose `status` is `matched`, `not_matched`, or `indeterminate`.

The origin is fixed. Redirects are never followed, TLS certificates are
verified (TLS 1.2 or newer), and standard HTTPS proxy environment variables are
honored. Requests are limited to 4 MiB and responses to 1 MiB.

## Result validation

The adapter accepts only a `completed` response with exactly one output text.
The text must decode as the schema, with no unknown fields or trailing values,
and must cover every requested rule exactly once. Provider order is not
trusted: results are re-emitted in request order. Each rule's reason code is
`openai.matched`, `openai.not_matched`, or `openai.indeterminate`. The
provider's request ID and reported model are recorded when they are safe
identifiers. Raw provider bodies and messages are never recorded.

## Failures and retries

| Condition | Adapter code | Retryable |
| --- | --- | --- |
| HTTP 401 or 403 | `openai.credential_rejected` | no |
| HTTP 400, 404, or 422 | `openai.request_rejected` | no |
| HTTP 408, or the attempt deadline elapsed | `evaluator.timeout` | yes |
| HTTP 429 | `evaluator.throttled` | yes |
| HTTP 429 with `insufficient_quota` | `openai.quota_exhausted` | no |
| HTTP 500, 502, 503, or 504, or a connection failure | `evaluator.unavailable` | yes |
| Any other status, including redirects | `openai.unexpected_status` | no |
| TLS verification failure | `openai.tls_failed` | no |
| Refusal | `openai.refused` | no |
| Incomplete response (for example, too few output tokens) | `openai.response_incomplete` | no |
| Malformed response or schema violation | `openai.response_malformed` / `openai.output_invalid` | no |
| Response larger than 1 MiB | `openai.response_too_large` | no |

Retries happen only as the profile's retry policy allows (see
[profile execution](./profile-execution.md#retry-and-routing-rules)); the
adapter itself never retries. OpenAI does not offer idempotency keys for this
endpoint, so a retried timeout can be processed or billed twice. The trace
records every attempt, and the profile's attempt limit bounds the total.

## Not yet included

- A frozen-corpus accuracy, calibration, cost, or adversarial evaluation.
- Recording provider rate-limit or retry-after metadata.
- Other providers. The adapter boundary is provider-neutral, so additional
  adapters plug into the same registry and profile contract.
