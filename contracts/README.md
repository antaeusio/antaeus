# Antaeus portable contracts

This directory is the language-neutral source of truth for Antaeus policy and decision data. Go types are implementation aids and must remain conformant with these schemas and fixtures.

## Contract version

The initial version is `v0alpha1`:

- policies use `apiVersion: policy.antaeus.io/v0alpha1` and `kind: Policy`;
- decision requests use `apiVersion: decision.antaeus.io/v0alpha1` and `kind: DecisionRequest`; and
- decisions use `apiVersion: decision.antaeus.io/v0alpha1` and `kind: Decision`.

Published schema versions are immutable. An incompatible structural or semantic change receives a new `apiVersion` and new schema path.

## Resource limits

Implementations must reject inputs exceeding any applicable limit before unbounded allocation or evaluation:

| Resource | Limit |
| --- | ---: |
| Policy source | 1 MiB |
| Decision request JSON | 1 MiB |
| Decision JSON | 1 MiB |
| JSON/YAML nesting depth | 32 |
| Aggregate parsed nodes | 10,000 |
| Rules per policy | 256 |
| Policy or rule description | 4,096 Unicode code points |
| Rule condition (`when`) | 16,384 Unicode code points |
| Decision message | 4,096 Unicode code points |
| Reason codes per object | 16 |
| Extensions per Decision | 32 |

YAML authoring is restricted to one UTF-8 YAML 1.2.2 document representing the JSON data model. Duplicate or non-string keys, directives, custom tags, anchors, aliases, merge keys, multiple documents, invalid Unicode, non-finite numbers, and values outside these limits are rejected. JSON input rejects duplicate keys and trailing documents.

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

Requests rejected before evaluation use a non-2xx status with RFC 9457 Problem Details (`application/problem+json`). Policy `deny` and `review` outcomes are not transport errors.

## Layout

- `schemas/v0alpha1/` contains authoritative JSON Schemas.
- `openapi/v0alpha1/openapi.yaml` describes the portable synchronous HTTP operation.
- `examples/v0alpha1/` contains readable valid examples.
- `conformance/v0alpha1/` contains machine-oriented valid, invalid, and canonicalization fixtures.
