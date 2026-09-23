# Profile-driven evaluation

`evaluator/runner.Run` accepts a policy, an immutable evaluator profile, canonical
input, correlation ID, already-preflighted credentials, and an installed adapter
registry keyed by exact adapter ID/version. It returns a validated Decision with
a bounded execution trace. The CLI's `evaluate-profile` command connects local
configuration selection to this runner for the installed deterministic fixture
adapter. Configuration commands remain inspection and credential checks; no CLI
command invokes a remote provider yet.

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

Only `io.antaeus.fixture@0.1.0`, using `io.antaeus.rule-match@v0alpha1`, is
installed. All profile entries must use it and name the supplied fixture set's
exact name and version. The fixture case must match the policy and canonical
input identities. The adapter supports `json-input` and `structured-rule-results`,
not `confidence-scores`; confidence routing is therefore rejected. Profile
deadlines and routing go through the reusable runner, with no remote calls or
credential reads. Selected semantic profiles are rejected, including trusted
ones, before credential preflight, without prompting for unusable credentials
or a trust grant. Unused bindings are never resolved.

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
and requires every routed credential. Missing credentials, unsupported adapters,
invalid inputs, invalid registry labels, and pre-existing cancellation return Go
errors without invoking an evaluator or creating a Decision. Enforcement rejects
deterministic fixtures. Fixture evidence remains visibly synthetic in local use.

Once the first attempt starts, adapter failures, malformed results, cancellation,
and deadline exhaustion become failed rule evidence. The existing deterministic
reducer determines the outcome; an already accepted matched deny still outranks
unresolved rules. No failure synthesizes a policy outcome or invokes an
unconfigured evaluator.

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
clips waiting to the remaining total deadline. Tests inject a clock and jitter
source. Retries retain identical policy/profile/input identity, correlation ID,
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
