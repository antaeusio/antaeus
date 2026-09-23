# Antaeus portable contracts

This directory is the language-neutral source of truth for Antaeus policy and decision data. Go types are implementation aids and must remain conformant with these schemas and fixtures.

## Contract version

The initial version is `v0alpha1`:

- policies use `apiVersion: policy.antaeus.io/v0alpha1` and `kind: Policy`;
- decision requests use `apiVersion: decision.antaeus.io/v0alpha1` and `kind: DecisionRequest`;
- decisions use `apiVersion: decision.antaeus.io/v0alpha1` and `kind: Decision`;
- regression suites and result sets use `apiVersion: regression.antaeus.io/v0alpha1`; and
- evaluator profiles use `apiVersion: evaluator.antaeus.io/v0alpha1` and `kind: EvaluatorProfile`; and
- local secret bindings use `apiVersion: config.antaeus.io/v0alpha1` and `kind: LocalSecretBindings`.

Published schema versions are immutable. An incompatible structural or semantic change receives a new `apiVersion` and new schema path.

## Resource limits

Implementations must reject inputs exceeding any applicable limit before unbounded allocation or evaluation:

| Resource | Limit |
| --- | ---: |
| Policy source | 1 MiB |
| Decision request JSON | 1 MiB |
| Decision JSON | 1 MiB |
| Regression suite source | 1 MiB |
| Evaluator profile source | 1 MiB |
| Local secret bindings source | 1 MiB |
| JSON/YAML nesting depth | 32 |
| Aggregate parsed nodes | 10,000 |
| Rules per policy | 256 |
| Cases per fixture or regression suite | 256 |
| Policy or rule description | 4,096 Unicode code points |
| Rule condition (`when`) | 16,384 Unicode code points |
| Decision message | 4,096 Unicode code points |
| Reason codes per object | 16 |
| Extensions per Decision | 32 |

Nesting depth counts the active object/array container stack, including a root
object or array; scalar leaves do not add a level. Aggregate parsed nodes count
every object, array, scalar value, and object/mapping key. Implementations must
accept inputs exactly at a published limit and reject inputs above it before
typed evaluation.

YAML authoring is restricted to one UTF-8 YAML 1.2.2 document representing the JSON data model. Duplicate or non-string keys, directives, custom tags, anchors, aliases, merge keys, multiple documents, invalid Unicode, non-finite numbers, and values outside these limits are rejected. To avoid legacy parser-dependent folding, YAML source line breaks must be LF or CRLF; bare CR and U+0085, U+2028, or U+2029 are rejected anywhere in YAML source, including quoted scalars. JSON input rejects duplicate keys, unpaired Unicode surrogate escapes, and trailing documents.

For a `RegressionSuite`, the source-size and case-count limits apply to the
suite envelope. Nesting depth and parsed-node limits apply independently to
each inline `input`, measured from that input object's root; the fixed suite
envelope does not consume either input budget.

Conformance fixtures define portable accept/reject behavior, not a portable
error taxonomy. The Go `policy.ParseError.Code` values use the `source.*`
namespace and are specific to the Go API.

## Canonical identity

A valid policy is serialized with RFC 8785 JSON Canonicalization Scheme (JCS), encoded as UTF-8, and identified by lowercase `sha256:<64 hex digits>`. Source comments, whitespace, mapping order, quoting, and YAML-versus-JSON syntax do not affect the digest. Array order and Unicode code points do.

## HTTP semantics

`POST /v0alpha1/decisions` returns HTTP 200 with a Decision for `allow`, `review`, `deny`, and `failure`. A `failure` Decision means evaluation was accepted but did not produce a policy judgment.

Every Decision and rule result carries at least one stable reason code. A failure Decision's top-level reason codes include its structured failure code. Results appear exactly once in policy order, and semantic validation recomputes the deterministic reduction rather than trusting the claimed outcome.

## Deterministic reduction

After evaluator routing is complete, every policy rule has exactly one result in policy order. `matched` carries that rule's configured policy outcome. `not_matched` carries no outcome. Both `indeterminate` and `failed` are unresolved and carry no outcome.

Implementations reduce the complete result list in this exact precedence order:

1. any matched `deny` rule yields `deny` with required top-level reason code `policy.deny_rule_matched`;
2. otherwise, any `indeterminate` or `failed` result yields `failure` with required top-level reason code `evaluation.unresolved_rule`;
3. otherwise, any matched `review` rule yields `review` with required top-level reason code `policy.review_rule_matched`;
4. otherwise, any matched `allow` rule yields `allow` with required top-level reason code `policy.allow_rule_matched`; and
5. otherwise, the policy's `defaultOutcome` applies with required top-level reason code `policy.default_outcome`.

The required reduction code must appear in the Decision's top-level `reasonCodes`; implementations may add other bounded reason codes. A `failure` Decision must also include its structured `failure.code` in the top-level reason codes. One code can satisfy both requirements when the structured code is `evaluation.unresolved_rule`.

An accepted evaluation produces one rule result per policy rule even when a configuration, evaluator, reduction, or internal failure prevents a judgment. Such rules use `indeterminate` or `failed`, so the reducer can reproduce `failure`; a matched `deny` still takes precedence. The reducer determines only the terminal outcome, its required reason code, and the standard unresolved-rule failure. Other structured failure details describe the execution failure and remain subject to schema and semantic validation.

The language-neutral cases in `conformance/v0alpha1/reduction/cases.json` are normative input-to-output fixtures for this behavior.

## Evaluator fixtures

The provider-neutral evaluator boundary receives verified policy identity,
exact canonical JSON input, ordered rule IDs and conditions, deadline,
correlation ID, and profile digest. It never receives a rule's configured
policy outcome. A normalized evaluator result returns every requested rule
exactly once in request order with `matched`, `not_matched`, `indeterminate`, or
`failed`, optional finite confidence, stable reason codes, and bounded safe
diagnostics.

`fixture-set.schema.json` defines the credential-free deterministic fixture
format. Each named case binds an exact policy name and digest plus the SHA-256
digest of exact canonical input bytes to exact normalized rule results. Missing
cases, identity mismatches, and incomplete rule mappings are errors. Fixture
results are always labeled `deterministic-fixture` and synthetic; they are test
evidence, not semantic inference or an enforcement fallback.

Decisions produced from fixture evidence record evaluator `mode`, `synthetic`,
`fixtureSet`, and `fixtureVersion` alongside the profile digest, adapter and
adapter version, route, and attempt count. Semantic evaluator metadata requires `synthetic: false` and
cannot carry fixture identity.

Case names are unique within a fixture set, and rule IDs are unique within each
case. JSON property names are case-sensitive and exact; unknown, case-variant,
and duplicate properties are rejected before typed decoding.

## Evaluator profiles

`evaluator-profile.schema.json` defines the immutable, non-secret mechanics for
obtaining evaluator evidence. A profile declares bounded total and per-attempt
timeouts, credential slot names, evaluator adapter and protocol identities,
capability requirements, retry behavior, confidence routing, escalation,
operational fallbacks, and failure-only terminal behavior. Confidence can
change routing only; it never creates or replaces a policy outcome.

Retries and fallbacks name only `timeout`, `unavailable`, and `throttled` as
eligible transient classes. `maxAttempts` includes the first attempt; one
attempt has an empty `retryOn` list and no backoff fields, while multiple
attempts require at least one class plus explicit initial backoff, maximum
backoff, and multiplier values. An invoked escalation that ends in an eligible
operational failure continues into the configured fallback list. Invalid input,
unsupported capability, malformed configuration, missing or rejected
credentials, and deterministic request failures are not retryable.
A fallback invoked after primary failure evaluates every rule. A fallback
invoked after escalation failure evaluates only the rules sent to escalation;
accepted primary results remain unchanged.

V0 reserves `json-input`, `structured-rule-results`, and `confidence-scores`
as capability IDs. When confidence routing is enabled, every routed evaluator
must declare `confidence-scores`. The threshold applies independently to each
rule result; missing confidence is treated as below the
threshold. Escalation evaluates only primary results below the threshold;
primary results at or above it remain unchanged, while escalation evidence
replaces the below-threshold results. Only primary evidence can trigger
escalation. Low or missing confidence from escalation or fallback evidence
remains `indeterminate` and never triggers another escalation.

Profiles contain logical credential slot names, never secret values,
environment-variable names, hosted secret identifiers, or arbitrary endpoint
overrides. Runtime bindings resolve slots separately. Deterministic fixture
evaluators cannot declare credentials, providers, models, or instruction
templates and are never valid enforcement fallbacks.
V0 profiles cannot mix semantic and deterministic-fixture evaluators, removing
any route by which synthetic evidence could become an enforcement fallback.

Profile authoring uses the same constrained YAML 1.2.2 or JSON data model,
1 MiB source bound, nesting and node limits, RFC 8785 canonicalization, and
lowercase SHA-256 content identity as policies. A registry version is a label
for that digest and cannot change profile content identity. The JCS-canonical
profile representation is also bounded to 1 MiB so profiles constructed through
the Go API obey a representation-independent size limit.

Profile numbers use the RFC 8785 IEEE-754 binary64 data model. JSON and YAML
spellings that represent the same number normalize to the same JCS value before
typed validation. Non-finite or unrepresentable values and values whose
normalized magnitude falls outside the interoperable range
`-9007199254740991` through `9007199254740991` are rejected before profile
validation or digesting.

Known objects reject unknown properties. Adapter-specific `parameters` require
a schema associated with the exact adapter ID and version; the core schema
closes the built-in fixture adapter parameters. Implementations must also
enforce semantic invariants that JSON Schema cannot express: unique evaluator
and credential-slot IDs, valid references, an acyclic route, no evaluator in
more than one route position, per-attempt timeouts within the total timeout,
and confidence routing only through declared capabilities.
Semantic validation also requires initial backoff not to exceed maximum
backoff. Instruction-template behavior is always bound by a content digest; an
optional namespaced ID is descriptive only.

## Local secret bindings

`local-secret-bindings.schema.json` defines the non-secret local mapping from
an evaluator adapter ID and profile-local credential slot to an environment
variable reference. V0 accepts only `source: environment`; raw values, `.env`
paths, hosted secret identifiers, and evaluator behavior overrides are not
part of this artifact. An artifact that exists contains at least one adapter
and one slot binding, and both dimensions are bounded to 16 entries.

The variable name is configuration metadata, not a credential value, but may
still reveal sensitive inventory. Output may identify only the adapter, slot,
and source class. No output mode—including verbose, debug, and error output—
prints the variable name or value. Runtimes inspect only references used by
evaluators in the selected profile's configured route and do not discover
`.env` files, inspect unrelated environment variables, or walk directories to
discover a bindings artifact. Local bindings are loaded only from an explicitly
supplied path.

Immediately before evaluation starts, the runtime reads every reference used by
the configured route exactly once and captures each non-empty value for that
evaluation. It supplies a captured value only if its evaluator is invoked and
releases all captured values when the evaluation ends. An unbound routed slot
and an unset or zero-length referenced variable are the same missing-credential
configuration error. The CLI reports it before evaluation; an HTTP service
rejects it before accepting an evaluation and does not create a Decision. It is
not retried and does not trigger evaluator fallback. Environment values are
captured and passed as exact bytes; whitespace is not trimmed, and a
whitespace-only non-empty value is therefore not treated as missing. Bindings
for the deterministic fixture adapter are invalid.

Local secret bindings use the same constrained JSON or YAML authoring rules,
1 MiB source limit, nesting and aggregate-node bounds, duplicate-key rejection,
and exact property names as evaluator profiles. Adapter keys intentionally name
the adapter ID rather than a profile or adapter version. For the selected
profile, a usable binding is the tuple of a configured route evaluator's
adapter ID and that evaluator's `credentialSlot`, which must also appear in the
profile's `credentialSlots`. Bindings for adapters or slots not referenced by
the selected profile's configured route are ignored and never resolved.
Within the syntactic pattern, v0 does not restrict which process environment
variable may be referenced. Selecting the explicit bindings file grants it
authority to name process variables for the routed adapters, so operators must
review it as sensitive local configuration even though it contains no values.
Variable lookup follows host process semantics; portable configurations must
not depend on environment names that differ only by letter case.

## Regression suites

`regression-suite.schema.json` defines named offline checks bound to one exact
policy digest and fixture-set version. Each case contains one inline JSON
object, selects one fixture case, and expects an exact terminal outcome and
ordered top-level reason codes. Inputs use the same strict parsing limits and
RFC 8785 canonicalization as local evaluation.

Policy identity is content-addressed. Regression-suite and fixture-set
name/version pairs are declared labels rather than content hashes, so a
publisher must issue a new version whenever either resource changes.

`regression-result-set.schema.json` records the expectation and complete actual
Decision for every case. Expectation mismatches set the case status to `failed`
and the aggregate `passed` field to `false`; configuration and evaluation errors
do not produce a partial result set. These fixtures remain synthetic and
credential-free. Regression and single-case local evaluation share the fixture
profile whose exact UTF-8 preimage is `antaeus.local.fixture/v0alpha1`.

Requests rejected before evaluation use a non-2xx status with RFC 9457 Problem Details (`application/problem+json`). Policy `deny` and `review` outcomes are not transport errors.

## Layout

- `schemas/v0alpha1/` contains authoritative JSON Schemas.
- `openapi/v0alpha1/openapi.yaml` describes the portable synchronous HTTP operation.
- `examples/v0alpha1/` contains readable valid examples.
- `examples/v0alpha1/input/` contains canonical local-evaluation inputs bound by fixture digests.
- `examples/v0alpha1/regression-suite/` and `regression-result-set/` contain a complete offline regression example.
- `examples/v0alpha1/evaluator-profile/` contains a credential-free deterministic profile.
- `examples/v0alpha1/local-secret-bindings/` contains non-secret environment-variable references.
- `conformance/v0alpha1/` contains machine-oriented valid, invalid, and canonicalization fixtures.
