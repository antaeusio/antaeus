# Migrating uncertainty to review

Antaeus v0.7.0 introduces
`decision.antaeus.io/v0alpha2`, `evaluator.antaeus.io/v0alpha2`, and
`regression.antaeus.io/v0alpha2` result sets. Published v0alpha1 schemas,
examples, OpenAPI, and conformance fixtures remain immutable. Existing released
binaries keep their original behavior.

## Decision behavior

The new reducer uses this precedence:

1. Matched deny: `deny`, `policy.deny_rule_matched`.
2. Any failed evidence: `failure`, `evaluation.unresolved_rule`.
3. Any indeterminate evidence: `review`, `evaluation.indeterminate_rule`.
4. Matched review: `review`, `policy.review_rule_matched`.
5. Matched allow: `allow`, `policy.allow_rule_matched`.
6. Otherwise, the policy default: `policy.default_outcome`.

A completed evaluation with ambiguous evidence or low/missing confidence returns
review even when another rule matches allow. Indeterminate rules retain their
confidence and reasons, including `evaluation.low_confidence`; they carry no
matched outcome. Review never contains a failure object. A matched deny still
wins over uncertain or failed evidence. Timeout, provider unavailability,
malformed output, cancellation, and execution errors remain failures unless
accepted deny evidence takes precedence. Pre-acceptance errors remain errors.

The unchanged v0alpha1 execution-trace schema still uses
`evaluation.unresolved_rule` for remaining indeterminate or failed evidence,
including review and deny Decisions. Inspect the Decision outcome rather than
classifying a trace terminal as failure. Operational failure metadata and
retryability remain unchanged.

## Consumer changes

Update DecisionRequest and Decision validators to v0alpha2. The portable HTTP
operation is described at `/v0alpha2/decisions`; this repository publishes the
contract, not a deployed HTTP service. All accepted outcomes use HTTP 200.

Update evaluator profiles to `evaluator.antaeus.io/v0alpha2` and set terminal
`onIndeterminate: review`, `onFailure: failure`. No alternate terminal action is
supported. The new runtime rejects v0alpha1 requests and profiles before
execution; it does not silently reinterpret them. The public Go APIs and CLI
use the new versions. Recompute profile digests and renew explicit trust for
changed profiles; do not reuse old immutable registry labels or trust grants.

The built-in local fixture profile now uses registry version `v0alpha2` and
the exact digest preimage `antaeus.local.fixture/v0alpha2`. Its previous
v0alpha1 identity remains historical; fixture set format and adapter versions
are unchanged.

Policy v0alpha1, rule-match protocol v0alpha1, fixture sets, local configuration,
credential bindings, execution traces, and usage reports retain their versions.
Regression suites keep v0alpha1, but generated result sets use v0alpha2 because
they embed new Decisions. Reissue suite versions when uncertainty expectations
change from failure to review, and update their expected reasons. Retain
historical result sets under their original schema and engine release.

Native and WebAssembly hosts preserve their version-1 invocation envelope while
requiring new profile versions and emitting new Decisions. Upgrade binaries,
WASM artifacts, profiles, validators, and downstream projections together. A
hosted consumer must use released artifacts and explicitly qualify its new
reducer, failure handling, accounting, and rollback before rollout. Do not
rewrite historical Decisions or relabel old failures during this migration.
