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

Requests rejected before evaluation use a non-2xx status with RFC 9457 Problem Details (`application/problem+json`). Policy `deny` and `review` outcomes are not transport errors.

## Layout

- `schemas/v0alpha1/` contains authoritative JSON Schemas.
- `openapi/v0alpha1/openapi.yaml` describes the portable synchronous HTTP operation.
- `examples/v0alpha1/` contains readable valid examples.
- `conformance/v0alpha1/` contains machine-oriented valid, invalid, and canonicalization fixtures.
