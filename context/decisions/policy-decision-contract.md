---
type: Metatron Decision
title: Portable policy and decision contract
status: canonical
scope: api/contracts
confidence: high
source_refs:
  - contracts/README.md
  - contracts/schemas/v0alpha1/policy.schema.json
  - contracts/schemas/v0alpha2/decision-request.schema.json
  - contracts/schemas/v0alpha2/decision.schema.json
  - contracts/openapi/v0alpha2/openapi.yaml
  - policy/policy.go
  - decision/decision.go
---

## Pattern

Treat the versioned JSON Schemas and conformance fixtures under `contracts/` as
the cross-language source of truth. A v0alpha1 policy uses constrained YAML or
equivalent JSON, stable explicit rule IDs, an explicit default outcome, RFC 8785
canonical JSON, and a lowercase SHA-256 content digest. Keep provider selection,
credentials, model settings, authorization, and side effects outside policy
artifacts.

Return exactly one provider-neutral Decision outcome: allow, review, deny, or
failure. Failure means evaluation was accepted but did not produce a policy
judgment; never rewrite it implicitly. Keep rule results in policy order and
reduce completed results under decision v0alpha2 deterministically: matched deny,
failed evidence as failure, indeterminate evidence as review, matched review,
matched allow, then the policy default. Preserve published v0alpha1 contracts;
the current runtime emits v0alpha2 and rejects v0alpha1 DecisionRequests. Use RFC 9457 Problem Details
only when a request is rejected before evaluation.

## Rationale

Immutable portable contracts and byte-identical policy identities let the Go
engine, CLI, and other implementations agree without making Go types
authoritative. Stable rule IDs and explicit failure semantics support honest
regression, audit, and transport behavior while preventing evaluator or hosted
service details from changing policy meaning.
