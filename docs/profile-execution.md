# Profile-driven evaluation

For the portable record and reducer semantics, see [reading a Decision](./decisions.md).

`evaluator/runner.Run` accepts a policy, an immutable evaluator profile, canonical
input, correlation ID, already-preflighted credentials, and an installed adapter
registry keyed by exact adapter ID/version. It returns a validated Decision with
a bounded execution trace. The CLI's `evaluate-profile` command connects local
configuration selection to this runner for the installed deterministic fixture
adapter and the experimental [OpenAI adapter](./openai-adapter.md). Configuration
commands remain inspection and credential checks and never call a provider.

## CLI fixture profiles

After building the development binary with `scripts/build`, run from the
repository root:

```sh
.tmp/bin/antaeus evaluate-profile \
  --profile contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json \
  --policy contracts/examples/v0alpha1/policy/vendor-onboarding.yaml \
  --input contracts/examples/v0alpha1/input/aggregate-analytics.json \
  --fixture-set contracts/examples/v0alpha1/fixture-set/quickstart.json \
  --case aggregate-analytics
```

Profile and reference-only bindings selection follows the same explicit CLI,
current-project, and OS-user precedence as [configuration commands](./cli-configuration.md).
Use `--profile` and optionally `--bindings`, or select artifacts through those
manifests. There is no implicit profile default, parent-directory search, `.env`
loading, or raw-secret flag. Malformed lower-priority configuration still fails
closed. Existing `evaluate` and `test` commands do not consume these manifests.

Three adapters are installed, all using `io.antaeus.rule-match@v0alpha1`: the
synthetic `io.antaeus.fixture@0.1.0` and the semantic
[`io.antaeus.openai@0.1.0`](./openai-adapter.md) and
[`io.antaeus.systemone@0.2.0`](./systemone-adapter.md) (with `0.1.0` still installed for existing CLM profiles). Fixture profiles use only
the fixture adapter; semantic profiles may combine the two semantic adapters. Fixture profiles require `--fixture-set` and `--case`,
must name the supplied fixture set's exact name and version, and the case must
match the policy and canonical input identities. Semantic profiles reject those
flags. Only the System One adapter reports `confidence-scores`, so confidence
routing is available only on routes made entirely of System One evaluators. Profile deadlines and routing go through the reusable runner. Fixture
runs make no remote calls or credential reads. Profiles using any other adapter
are rejected, including trusted ones, before credential preflight, without
prompting for unusable credentials or a trust grant. Unused bindings are never
resolved.
Fixture execution neither consults nor modifies saved trust markers because no
credential authority is needed; a corrupt marker cannot grant authority or block
a fixture. Malformed manifests/artifacts and invalid reference combinations still
fail closed before execution, even for unsupported profiles.

Successful execution prints one complete Decision, including the selected
profile's canonical digest, synthetic fixture metadata, and bounded execution
trace. Local artifacts do not invent registry version labels. All Decision
outcomes (allow, review, deny, failure) exit 0; inspect `outcome`, not just the
exit code. Invalid arguments exit 64; loading, configuration, pre-acceptance,
and output errors exit 1 with diagnostics only on stderr. In contrast to the
older single-attempt command, an adapter failure after profile-runner acceptance
(for example, an input digest mismatch) produces a typed failure Decision.
Fixture output is synthetic test evidence, never semantic inference or an
enforcement fallback.

Before calling `Run`, use the local configuration trust/selection and preflight
workflow, or an approved deployment's explicit bindings. The runner never reads
the environment or discovers configuration. The caller owns and clears the
preflighted credential set. The runner copies required credentials and clears
its copies on completion, passing only the invoked adapter's credential to each
attempt and clearing that attempt's buffer immediately on return.

## Acceptance and execution

Before accepting evaluation, the runner snapshots and validates policy/profile
artifacts, verifies canonical input, validates all semantic adapter parameters,
checks exact installed identities/protocols/capabilities for all routed adapters,
and requires every routed credential. Each routed fixture registration must
match the profile's pinned `fixtureVersion` before any adapter is invoked;
expected result metadata is derived from that pinned identity. Missing
credentials, unsupported adapters,
invalid inputs, invalid registry labels, and pre-existing cancellation return Go
errors without invoking an evaluator or creating a Decision. Enforcement rejects
deterministic fixtures even when `AllowSyntheticFixtures` is true. Otherwise,
fixture execution requires the explicit `AllowSyntheticFixtures: true` Go input;
the zero value rejects it before any adapter call or Decision. The guard covers
every configured route, including uninvoked escalation and fallback. Semantic
profiles do not need this opt-in. This is an intentional-use guard, not an
authorization mechanism: a trusted embedding boundary chooses execution mode.
The CLI sets the opt-in internally, without a bypass flag, only when every
profile entry uses the installed fixture adapter; semantic profiles never receive it.
Fixture evidence remains visibly synthetic in local use.

Once the first attempt starts, returned adapter errors, malformed results, cancellation,
and deadline exhaustion become failed rule evidence. The existing deterministic
reducer determines the outcome; an already accepted matched deny still outranks
unresolved rules. No failure synthesizes a policy outcome or invokes an
unconfigured evaluator.

For a failure outcome with an operational trace terminal, the runner enriches
the reducer's generic failure: `failure.code` is that terminal code, `stage`
remains `evaluation`, and top-level `reasonCodes` are exactly
`["evaluation.unresolved_rule", terminalCode]` in that order. Supported terminal
codes are `evaluator.timeout`, `evaluator.unavailable`, `evaluator.throttled`,
`evaluation.deadline_exceeded`, `evaluation.cancelled`, `evaluation.attempt_limit`,
`evaluation.adapter_failed`, and `evaluation.invalid_result`.

Only the first three transient codes can be retryable, and only when every
unresolved rule is `failed` with exactly that same code as its sole reason.
Any indeterminate rule, different failed cause or additional failed reason makes
the overall failure non-retryable. Resolved rules do not disqualify it. Ordinary
unresolved evidence without an operational terminal keeps the generic failure
and single `evaluation.unresolved_rule` top-level reason. Deny retains precedence
and never gains a failure object; per-rule evidence and the trace preserve the
details of mixed failures.

Retryability classifies evidence; it is not permission to retry automatically.
Callers still own deadlines, idempotency, retry limits, costs and side effects.
An unexpired caller deadline too short for the next wait can leave a retryable
transient failure; an actually expired deadline or observed cancellation is
non-retryable. Neither allows the current invocation to exceed its deadline.

Programming panics are an explicit exception: adapter panics propagate after
deferred credential cleanup and attempt/total context cancellation. They return
no Decision or completed trace, even if cancellation or a deadline is also
observable. The runner does not recover, log or serialize the panic. An unhandled
runtime/host panic can still print sensitive values or stacks; this is not a
redaction guarantee. Embedding hosts must design supervision/isolation and avoid
blindly reusing potentially corrupted adapter state. Panic containment needs a
distinct quarantine signal and cannot be treated as an ordinary retryable error.

### Development migration for the planned v0.1.0 release

Go callers intentionally executing fixtures must add `AllowSyntheticFixtures:
true` to `runner.Input`; callers relying on the previous zero-value permission
now receive a pre-acceptance error. `Enforcement: true` always prohibits fixtures.
No opt-in change is needed for semantic profiles. The fixture-only CLI retains
its flags and exit classes, and the legacy `evaluate`/`test` paths are unchanged.
Published portable schemas and policy/profile identities are unchanged.
This is a documented first-minor development change, not a patch backport or a
claim that v0.1.0 has been released.

Retry-budget handling also changes in this minor release. Previously a retry
delay at least as long as the remaining budget slept until the deadline and
ended with `evaluation.deadline_exceeded`. It now skips that futile wait and may
invoke an eligible fallback while time remains. Successful fallback evidence can
therefore produce a real `allow`, `review` or `deny` where the old runner returned
`failure`; the reducer and policy meaning are unchanged. Without a successful
recovery, the last transient code (for example `evaluator.timeout`) can replace
the old deadline code in trace terminal and failed-rule reasons. The generic
core failure mapping is a separate change described below. Go and CLI consumers
must not assume that this budget boundary always returns failure or a deadline
code; inspect the actual outcome, evidence and trace. No provider may run beyond
the total/caller deadline, and an already expired or cancelled run still stops.

Operational failure metadata also changes in the planned v0.1.0 release for
both Go `runner.Run` and CLI `evaluate-profile`. Previously every reduced failure
used `failure.code: evaluation.unresolved_rule`, `retryable: false`, and one
top-level reason. Operational failures now use the mapping above, including
the additional terminal reason and conditional transient retryability. For
example, an accepted fixture input-identity mismatch now returns
`failure.code: evaluation.adapter_failed` with both reasons and remains
non-retryable. Consumers matching only the old generic code or exact reason
array must migrate; do not infer automatic retry permission from the new flag.
Non-operational unresolved failures, policy judgments, the standalone reducer,
legacy `evaluate`/`test`, and regression execution are unchanged. This uses the
portable Decision contract's existing allowance for operational failure details;
no published schema, trace version or extension-key binding changes.

Calls are synchronous. Adapters must honor context cancellation and return
promptly; the runner does not detach goroutines or forcibly stop an uncooperative
implementation. Each attempt's context and request carry the earlier of its
timeout and the total deadline. The total deadline is also capped by any caller
deadline and includes retries, backoff, escalation, and fallback. Results returned
after a deadline are rejected even if the adapter reports success.

The overall attempt cap is 64, matching the existing Decision contract, even when
the sum of individual profile retry limits is larger. Reaching that cap records
`evaluation.attempt_limit` and leaves the current rule subset failed.

## Retry and routing rules

Only `*evaluator.Error` with `Retryable: true` and one of these exact codes can
trigger configured retries or operational fallback:

| Adapter error code | Profile failure class |
| --- | --- |
| `evaluator.timeout` | `timeout` |
| `evaluator.unavailable` | `unavailable` |
| `evaluator.throttled` | `throttled` |

An elapsed per-attempt deadline is also a timeout when the total evaluation is
still live. Arbitrary errors, credential rejection, invalid result identity or
coverage, invalid confidence, and unsupported responses are non-retryable.
Raw error messages are never copied into Decisions or traces.

For retry number `n` (1 is the first retry), the base delay in milliseconds is
`min(maxBackoffMs, initialBackoffMs * multiplier^(n-1))`, truncated to an integer.
The production clock uses equal jitter uniformly over the inclusive interval
from half that base delay to the full base delay, at nanosecond resolution, and
consumes one jitter draw per contemplated retry. If that delay is greater than
or equal to the remaining total budget, retries stop without sleeping: preserve
the last transient failure so an eligible configured fallback can use the time
left. Without an eligible fallback, return that failure promptly. An already
expired deadline or cancellation wins over the prior error; neither starts a
fallback. Otherwise wait normally within the context deadline. This means jitter
can affect retry-versus-fallback selection near the budget boundary; the trace
records what actually ran. Tests inject a clock and jitter source.
Retries retain identical policy/profile/input identity, correlation ID,
and rule subset. An adapter that supports provider idempotency must derive and
reuse an appropriate stable key for that evaluator and logical request.

Only primary evidence can trigger confidence escalation. Missing or
below-threshold confidence changes a non-failed rule to indeterminate. When
configured, escalation reevaluates only those primary rule indexes; accepted
primary evidence stays unchanged. A high-confidence indeterminate result remains
indeterminate. Low confidence from escalation or fallback remains indeterminate
and never recurses or triggers operational fallback by itself.

Primary operational failure can invoke ordered fallbacks over all rules.
Escalation operational failure can invoke ordered fallbacks over only the
escalated subset. Each failed fallback must itself match `fallbackOn` before the
next fallback is attempted. Normalized per-rule `failed` evidence without a
classified adapter error stays unresolved and is not an operational retry signal.

## Adapter boundary and audit data

Execution metadata follows the [field-specific text rules and migration](./execution-text.md).
Do not substitute Go's whitespace classification or requested-model identifiers
for the published resolved-text pattern.

An installed adapter declares its mode, protocol, capabilities, parameter
validator, and synchronous Evaluate function. Fixture registrations also declare
the fixture version. Adapter configuration and evidence-request buffers are
copied for each invocation. Requests contain policy conditions but no authored
allow/review/deny outcomes. Installed adapters remain responsible for approved
origins, provider request/response bounds, and returning normalized safe metadata.

Returned metadata must match the configured adapter identity, version, provider,
mode, and fixture identity. A provider-reported resolved model may differ from a
requested mutable alias. An explicit resolved revision must match a pinned
requested revision. Missing revision evidence is recorded as unavailable, not
inferred from the request. Adapter rule messages are omitted; bounded normalized
reason codes and confidence are retained.

Decisions contain compact route IDs, total attempts, a fallback flag, profile
digest, and the last actually attempted evaluator's metadata. Mixed evidence can
come from multiple routes; the last evaluator is not the source of every rule.
`extensions["io.antaeus.execution"]` follows the
[execution trace schema](../contracts/schemas/v0alpha1/execution-trace.schema.json).
It records every attempt's route class, profile-local evaluator ID, exact adapter
version, 1-based per-evaluator attempt number, ascending 0-based policy rule
indexes, outcome code, latency, and requested/resolved model information.
Profile digest links to the immutable protocol, parameters, and template details.
The trace terminal code describes routing completion, not a policy judgment.
For example, a terminal outage may coexist with a deny from accepted evidence.

The trace contains no credential values, reference names, prompts, input bodies,
raw provider payloads, or exception messages. The schema bounds it to 64 attempts
and 256 indexes per attempt. Cancellation before the first actual attempt remains
a pre-acceptance error, so successful runner returns always have at least one
attempt and a portable Decision.
