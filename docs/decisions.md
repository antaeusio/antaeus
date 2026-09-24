# Reading a Decision

A `decision.antaeus.io/v0alpha1` Decision records the result of an accepted
evaluation. It does not authenticate a caller, authorize an operation, perform a
transaction or execute application side effects. Consumers must inspect the
outcome and retain the policy identity, rather than treating successful transport
as permission to proceed.

The [Decision schema](../contracts/schemas/v0alpha1/decision.schema.json) and
[portable contract](../contracts/README.md) are authoritative. This guide explains
the existing contract and Go implementation, not a new API version or deployed
HTTP service. No released semantic evaluator is claimed.

## Four outcomes

| Outcome | Meaning | Complete example |
| --- | --- | --- |
| `allow` | A policy judgment permitting the evaluated case | [allow.json](../contracts/examples/v0alpha1/decision/allow.json) |
| `review` | A policy judgment explicitly selecting review | [review.json](../contracts/examples/v0alpha1/decision/review.json) |
| `deny` | A policy judgment rejecting the evaluated case | [deny.json](../contracts/examples/v0alpha1/decision/deny.json) |
| `failure` | Evaluation was accepted but did not produce a policy judgment | [failure.json](../contracts/examples/v0alpha1/decision/failure.json) |

These files are contract examples, not evidence of model accuracy. The executable
[fixture quickstart](./quickstart.md) emits a visibly synthetic
[fixture Decision](../contracts/examples/v0alpha1/decision/fixture-review.json).
Do not relabel `failure` as `allow`, `deny` or `review`. An application can choose
an explicit handling workflow, such as human attention, while preserving the
original failure record. That application choice is outside the evaluator.

## Identity, rules and reasons

Every Decision carries `apiVersion`, `kind: Decision`, `outcome`, `policy`,
`ruleResults` and `reasonCodes`. `policy` names the artifact and its lowercase
SHA-256 canonical digest; an optional registry version is a separate label.
See [policy identity](./policy-authoring.md#identity-and-revision).

Results cover every policy rule exactly once in policy order. Each has a stable
`ruleId`, status and nonempty bounded reason-code list. A `matched` rule also has
the policy-authored `outcome`; `not_matched`, `indeterminate` and `failed` must
not carry an outcome. Optional confidence is finite and in 0–1. Confidence can
change profile routing but cannot create a policy judgment by itself.

After routing, the reducer applies this precedence, not first-match order:

1. Any matched deny yields `deny` with `policy.deny_rule_matched`.
2. Otherwise, any indeterminate or failed rule yields `failure` with
   `evaluation.unresolved_rule`.
3. Otherwise, matched review yields `review` with `policy.review_rule_matched`.
4. Otherwise, matched allow yields `allow` with `policy.allow_rule_matched`.
5. Otherwise, use the policy default with `policy.default_outcome`.

The corresponding reduction reason must appear at the top level. Thus accepted
deny evidence can coexist with another failed rule: the outcome remains deny,
and the rule results preserve the unresolved evidence. A default of review does
not turn an outage into a review judgment.

The public-package [executable reducer example](../decision/example_test.go)
demonstrates unresolved evidence, followed by deny taking precedence over it:

```sh
scripts/with-build-lock go test -count=1 -run '^ExampleReduce$' ./decision
```

`decision.Reduce` returns a `Reduction`, not a complete Decision or execution
trace. It does not call an evaluator or perform routing. The full
[conformance cases](../contracts/conformance/v0alpha1/reduction/cases.json) cover
the portable precedence rules.

## Failure details and retryability

`failure` is required only for a failure outcome and forbidden for the other
three outcomes. It contains `code`, `stage` and `retryable`, with an optional
bounded message. Supported stages are `configuration`, `policy_resolution`,
`evaluation`, `reduction` and `internal`. Top-level reasons must include its code
as well as the reduction reason; a single code can satisfy both requirements.

The standalone reducer's generic unresolved result uses
`evaluation.unresolved_rule`, stage `evaluation`, and `retryable: false`.
The [profile runner](./profile-execution.md#acceptance-and-execution) can enrich
that failure with an operational terminal code. In that case, its top-level
reasons are `evaluation.unresolved_rule` followed by the terminal code, and the
stage stays `evaluation`. This is existing runner behavior within the portable
schema, not a promise that all producers use the same failure enrichment.

For that runner, only timeout, unavailable and throttled terminals can be
retryable, and only when every unresolved rule is failed with exactly the same
terminal code as its sole reason. Indeterminate evidence or mixed causes makes
it non-retryable. A deny outcome never gains a failure object. Ordinary per-rule
failed evidence is not by itself an operational error or a trigger for a retry.

`retryable: true` classifies the final evidence; it does not authorize an automatic
caller retry. The caller still owns deadlines, idempotency, attempt/cost limits
and application side effects. A timeout does not prove that a remote operation
was never processed. A malformed record should be rejected, not repaired by
inventing a different judgment.

## Fallback is execution, not an outcome rewrite

Profiles keep retry, confidence, escalation and fallback settings outside the
policy. The runner may invoke only configured routes within their time and
attempt bounds. A primary operational fallback evaluates all rules; fallback
after an escalation outage evaluates only the escalated subset, preserving
accepted primary evidence. Missing or low confidence from escalation/fallback
stays indeterminate and cannot recurse into another escalation.

Fallback returns fresh rule evidence for deterministic reduction. It is not
permission to replace failure with a configured allow or a synthetic fixture.
Fixtures are forbidden in enforcement routes. See
[routing rules](./profile-execution.md#retry-and-routing-rules) for exact eligible
errors, retry budgets and confidence behavior.

## Metadata is bounded, not proof

Optional `evaluator` metadata identifies a profile digest, adapter/version,
mode, synthetic marker, route and attempt count. The profile runner includes
an `extensions["io.antaeus.execution"]` trace conforming to the
[execution-trace schema](../contracts/schemas/v0alpha1/execution-trace.schema.json).
The last evaluator is not necessarily the source of every rule in a mixed result.

Keep the `deterministic-fixture`/`synthetic: true` marker and fixture identity
visible. Neither a digest, mode label nor valid schema authenticates the producer,
proves semantic accuracy or establishes authorization. Render metadata as
untrusted text; its [text rules](./execution-text.md) are not sanitization.

## Validate at the correct boundary

`Decision.Validate()` checks a typed value's field invariants, uniqueness and
failure/metadata relationships. It cannot bind a result to a policy it has not
been given. `Decision.ValidateAgainst(policy)` additionally checks policy name
and canonical digest, exact ordered rule coverage and authored matched outcomes,
recomputes reduction, and requires its outcome and reasons to agree.

Neither method is a bounded JSON source loader, recovers duplicate/unknown fields
already lost by decoding, authenticates a producer, validates arbitrary extension
semantics, or proves that a reported failure/trace really happened. A transport
must enforce source limits and the published schema before relying on decoded
records; consumers of a known extension must validate its own schema and meaning.
Portable Decision JSON is bounded to 1 MiB; that byte limit is not enforced by
calling `Validate` on an already constructed Go value.

The [OpenAPI contract](../contracts/openapi/v0alpha1/openapi.yaml) specifies HTTP
200 for all four accepted outcomes and Problem Details for requests rejected
before evaluation. It is a contract, not an implemented server in this repo.
The CLI evaluation commands likewise exit zero for any emitted Decision, including failure. Loading
or pre-acceptance errors produce diagnostics instead; single-attempt adapter
errors remain Go/CLI errors, while accepted profile-runner errors can produce
failed evidence. Adapter panics propagate without a Decision. The
[evaluator guide](./evaluator-adapters.md#choose-the-execution-layer) explains
these distinct execution boundaries and the profile runner's cleanup behavior.
