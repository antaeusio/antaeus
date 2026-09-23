# v0alpha1 conformance fixtures

Every implementation claiming v0alpha1 support must consume the same files in this directory and the valid examples under `../../examples/v0alpha1/`.

## Canonical policy

Both of these authoring forms represent the same policy:

- `../../examples/v0alpha1/policy/vendor-onboarding.yaml`
- `../../examples/v0alpha1/policy/vendor-onboarding.json`

They must produce the exact UTF-8 bytes in `canonical/vendor-onboarding.canonical.json` after validation and RFC 8785 canonicalization, and the digest in `canonical/vendor-onboarding.sha256`.

## Invalid policies

- `policy/invalid-empty-rules.json` fails structural schema validation.
- `policy/invalid-unknown-property.json` fails structural schema validation.
- `policy/invalid-duplicate-rule-id.json` passes structural validation but fails the semantic uniqueness invariant.

## Invalid decisions

- `decision/invalid-failure-missing-details.json` fails because a failure outcome requires a structured failure object.
- `decision/invalid-non-match-outcome.json` fails because only a matched rule carries its policy outcome.

## Deterministic reduction

`reduction/cases.json` contains one policy plus ordered rule-result inputs and expected reduced outputs. Implementations must reproduce the specified outcome, required reason codes, and unresolved-rule failure exactly. The cases cover every precedence step, including matched deny over unresolved evidence and unresolved evidence over matched review.

## Profile-runner failure assembly

`runner/failure-mapping.json` adds runner-level cases without changing the
deterministic reduction fixtures. Its `policy` path is relative to that fixture
file. Each `cases` entry supplies final rule evidence in policy order (attach
the corresponding rule ID and, only for matched rules, its policy outcome),
the trace terminal and expected outcome, exact ordered top-level reasons and
optional failure object. Reduce first, then enrich only operational failures.
These are final-assembly cases, not adapter routing scripts; they cover mixed
unresolved evidence, resolved rules, deny precedence and all operational codes.

`callerDeadlines` entries use the same policy and the fixture's relative `profile`
path (2000 ms primary attempt timeout, 10000 ms total timeout). Preserve that
profile except for the explicit retry/fallback overrides below. Start a fake clock, set the caller
deadline to `callerBudgetMs` after start (shorter than the profile total timeout),
and configure primary throttling retries with two attempts and fixed
`retryDelayMs` backoff/jitter, with no fallbacks. The adapter advances that clock
by `adapterElapsedMs`, then returns retryable `evaluator.throttled`. Exactly one
attempt and no sleeps must occur: a delay at least as long as the remaining
budget is futile, including exact equality. Compare the final core fields to `expected`
and trace terminal to `expected.failure.code`. Unexpired futile waits retain
transient classification; actual expiry is a non-retryable deadline failure.

## Invalid fixture sets

- `fixture-set/invalid-unknown-property.json` fails closed on an unknown rule-result property.
- `fixture-set/invalid-bad-digest.json` rejects a non-digest policy identity.
- `fixture-set/invalid-empty-rule-results.json` rejects a case without exact rule evidence.

## Evaluator profiles

The evaluator-profile fixtures reject unknown fields, outcome-replacing
terminal behavior, credentials on deterministic fixtures, and incomplete
confidence routing. They also reject adapters masquerading as deterministic
fixtures and profiles that mix semantic with synthetic evaluators. Relational
routing fixtures will accompany the Go semantic validator; JSON Schema alone
cannot prove reference integrity or acyclicity.
The valid semantic example exercises explicit retry backoff, confidence-based
escalation, credential slots, a digest-bound instruction template, and an
operational fallback.
Parser-level fixtures also reject non-finite binary64 values and normalized
numbers outside the interoperable IEEE-754 safe range before structural
validation.

## Local secret bindings

The valid example maps an adapter and logical credential slot to an environment
variable reference without containing a value. Invalid fixtures reject a raw
value field, inline and `.env` source classes, `.env` discovery metadata, hosted
secret identifiers, evaluator behavior overrides, an unsafe environment-variable
name, and an adapter entry without any slot bindings. Programmatic schema tests
cover the adapter and slot count boundaries, identifiers, discriminators,
required reference fields, and the forbidden deterministic fixture adapter.

## Regression suites

`regression-suite/valid-input-max-depth.json` fixes the maximum inline-input
nesting boundary. `invalid-input-over-depth.json` exceeds it by one container.
The other invalid fixtures cover unknown properties and present descriptions
that are null, empty, or whitespace-only across the portable Unicode whitespace
set, plus a whitespace-only suite version.

## Policy source parsing

`policy-source/valid-core-scalars.yaml` confirms YAML 1.2 core-schema behavior
for plain strings that YAML 1.1 parsers commonly misresolve. The invalid YAML
fixtures cover duplicate and non-string keys, directives, tags, anchors,
aliases, merge keys, multiple documents, non-finite numbers, and lone JSON
surrogates. Generated tests cover invalid UTF-8, the deliberately excluded
line-break characters, and
both sides of the byte, depth, and aggregate-node boundaries. CI
remains network-free for all conformance cases.
