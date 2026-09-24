# Evaluator and adapter boundary

This guide describes the implemented Go APIs, not a released provider SDK or a
remote evaluator wire protocol. The CLI installs the credential-free
deterministic fixture and the experimental [OpenAI adapter](./openai-adapter.md)
(`adapters/openai`), which is a worked example of a remote adapter.
The [portable schemas](../contracts/README.md) remain authoritative for their
versioned artifacts; these Go types do not replace them. Public Go compatibility
follows the [development compatibility policy](./compatibility.md).

## Choose the execution layer

| Entry point | Responsibility | Failure boundary |
| --- | --- | --- |
| [`evaluator.Evaluator.Evaluate`](../evaluator/evaluator.go) | One implementation returns normalized evidence for requested rules | Returns evidence or a Go error; does not choose a policy outcome |
| [`evaluator.Decide`](../evaluator/decide.go) | One attempt, policy-derived rules, result validation and deterministic reduction | Adapter, cancellation, deadline and invalid-result errors remain Go errors |
| [`runner.Run`](../evaluator/runner/runner.go) | Validated profile, registered adapters, bounded retries/escalation/fallback and trace | Rejections before acceptance are Go errors; exhausted accepted execution becomes failed evidence and a Decision |

Both Decision-producing paths apply policy-authored outcomes only after evidence
returns. An accepted matched deny outranks unresolved evidence. A `failure`
Decision is not an instruction to allow, deny or silently substitute review.
Adapter panics are not ordinary failures: the profile runner propagates them
after deferred cleanup, without returning a Decision. Neither layer forcibly
stops an uncooperative adapter or detaches its goroutine. See the detailed
[profile execution and migration rules](./profile-execution.md).

## One normalized request

[`evaluator.Request`](../evaluator/evaluator.go) carries the policy name and digest,
canonical JSON input bytes, ordered rule IDs and `when` conditions, an absolute
deadline, correlation ID and immutable profile digest. It contains no rule
outcome, provider configuration or credential. Conditions and input are data,
not authority to change the adapter's configuration or role.

Use the orchestration layers to derive rules from the same validated policy
whose digest is sent. Do not treat `Request.Validate` as proof of canonical
identity: it checks bounded JSON-object shape, labels, rules and a nonzero
deadline, but does not verify JCS canonical bytes, resolve either digest or prove
that rules belong to the policy. `InputDigest` hashes the exact supplied bytes.
The single-attempt caller must supply verified canonical input; the profile
runner additionally canonicalizes and compares input bytes before acceptance.
Neither a digest string nor a correlation ID is authentication or a promise of
provider-side idempotency.

Request input is at most 1 MiB, with 1–256 unique ordered rules and conditions
of at most 16,384 Unicode code points. See the
[validator](../evaluator/validate.go) for exact field checks and the
[source-loading contract](../contracts/README.md#resource-limits) for artifact
parsing limits. An adapter must honor the supplied context and deadline promptly.

## Complete evidence, not a Decision

Return exactly one `RuleResult` for every requested rule, in request order.
Statuses are `matched`, `not_matched`, `indeterminate` and `failed`. Each result
has 1–16 unique stable reason codes; optional confidence must be finite and in
the inclusive range 0–1. Optional safe diagnostic messages are limited to 4,096
Unicode code points. Missing, duplicate, unknown or reordered IDs, invalid
statuses/confidence and malformed metadata fail `evaluator.ValidateResult`.
Normalize external ordering before returning; the runner does not repair it.

Confidence is evidence for routing, never a policy outcome. With profile
confidence routing enabled, every routed adapter must declare `confidence-scores`.
Missing or below-threshold confidence makes non-failed evidence indeterminate;
only primary evidence can trigger the single configured escalation. A normalized
per-rule `failed` result alone is not an operational retry signal.

Metadata identifies the effective adapter ID/version, mode, synthetic marker and
optional provider/model/revision/request ID. Fixture results require the exact
fixture-set name/version and `synthetic: true`; semantic results require
`synthetic: false` and no fixture identity. Execution text follows
[field-specific whitespace and length rules](./execution-text.md), not a universal
trim or sanitization rule. Do not put credentials, submitted content or raw
provider payloads into metadata, reason codes or diagnostics.

Metadata validation checks structure, not whether an adapter's claims are true.
`runner.Run` also matches returned adapter identity, version, mode, provider and
fixture identity to the configured entry. A resolved model can differ from the
requested alias. A returned revision must match a configured pinned revision;
missing revision evidence is not invented. The trace records requested/resolved
model information, but the Decision's evaluator identifies only the last
attempted route, not the source of every rule in a mixed result.

Not every result field is exported: `evaluator.Decide` preserves safe rule
messages, while the profile runner omits them. Provider request IDs are not
currently copied into either Decision path or the profile trace. The profile
trace carries resolved revision evidence; the single-attempt Decision does not.

## Register an adapter for profile execution

[`runner.Registry`](../evaluator/runner/runner.go) maps exact adapter ID/version
pairs (`profile.ComponentIdentity`) to an installed `runner.Adapter`. Registration
declares mode, exact protocol identity, supported capability IDs, the parameter
validator, fixture version when applicable, and an `Evaluate` function accepting
the request plus per-attempt `runner.Configuration`.

The profile runner validates the profile and parameter objects before accepting
execution, then checks all routed registrations and required capabilities.
Semantic parameter validators receive the JCS-canonical parameter bytes; the
fixture's closed parameters are validated by the core. Profile inspection alone
does not prove that an installed adapter accepts its parameters. Keep parameter
validation deterministic and free of network calls or credential lookup.

`Configuration.Evaluator` is a per-attempt copy of non-secret settings;
`Configuration.Credential` contains only that adapter's credential bytes.
The caller must select/trust configuration and preflight reachable credentials
before calling the runner, then clear its own credential set. The runner never
reads the environment and clears its own credential copies. Do not retain
request buffers, configuration or credential bytes after returning. Copies
preserve canonical numeric values, not exact Go dynamic numeric types, and do
not make concurrent mutation of caller-owned input safe.

Fixtures require `AllowSyntheticFixtures: true` and are always rejected with
`Enforcement: true`. This is a caller intent guard, not authorization or a sandbox.
The registered fixture version is checked before invocation; returned fixture-set
identity is checked after invocation. The CLI checks both before invocation.

## Operational errors and ownership

For profile routing, only an `*evaluator.Error` with `Retryable: true` and code
`evaluator.timeout`, `evaluator.unavailable` or `evaluator.throttled` is an
eligible adapter-reported transient failure. Eligibility still depends on the
profile, remaining time and the total attempt cap. An observed per-attempt
deadline can also become a timeout; caller/total cancellation and deadlines take
precedence. Other returned errors become `evaluation.adapter_failed`, and
invalid normalized evidence becomes `evaluation.invalid_result`.

The runner owns retry timing and route selection; do not add hidden retry loops
inside an adapter. Raw returned error messages are not copied into its Decision
or trace. This is not a blanket redaction guarantee: `evaluator.Error.Error()`
includes `Message`, and single-attempt errors can propagate to callers. Keep
errors safe at their source. The final Decision's `failure.retryable` classifies
evidence and does not grant automatic caller retry permission.

Installed adapters are trusted Go code, not isolated plugins. They remain
responsible for transport-specific origin restrictions, response bounds,
timeouts, cancellation, credential handling and safe normalization. Registering
capabilities does not enforce a transport policy or prove semantic quality.

## Executable fixture example and checks

[`ExampleRun`](../evaluator/runner/example_test.go) loads the committed policy,
profile and fixture, registers the real synthetic adapter and runs a Decision.
Its input literal is already canonical JSON and it needs no secret references.
Run it from the repository root:

```sh
scripts/with-build-lock go test -count=1 -run '^ExampleRun$' ./evaluator/runner
```

This is test evidence, not semantic inference. For CLI commands use the
[fixture quickstart](./quickstart.md) or [profile example](./profile-execution.md#cli-fixture-profiles).
Adapter changes should cover exact rule coverage, invalid metadata/confidence,
parameter rejection, identity mismatch, cancellation/deadline behavior and safe
errors. Run the relevant tests under `scripts/with-build-lock`, followed by
`scripts/check` directly (it owns its lock). A remote adapter additionally needs
its own verified provider contract and transport tests, as in `adapters/openai`;
the fixture example is not evidence that a remote provider is ready for use.
