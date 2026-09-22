---
type: Metatron Decision
title: Provider-neutral evaluator and deterministic fixture
status: canonical
scope: evaluator/protocol
confidence: high
source_refs:
  - evaluator/evaluator.go
  - evaluator/decide.go
  - evaluator/fixture/fixture.go
  - contracts/schemas/v0alpha1/fixture-set.schema.json
  - contracts/examples/v0alpha1/fixture-set/quickstart.json
  - contracts/README.md
---

## Pattern

Keep semantic evaluation behind one provider-neutral Go interface. Requests
carry a verified policy name and digest, exact canonical JSON input, ordered
stable rule IDs and conditions, a deadline, a correlation ID, and an immutable
profile digest. Never send a rule's allow, review, or deny outcome to an
evaluator. Normalized results return every requested rule exactly once in
request order with matched, not_matched, indeterminate, or failed status,
optional bounded confidence, stable reason codes, and safe metadata.

The local runner constructs evaluator rules from the same validated artifact
whose digest is sent to the adapter. After evidence returns, it attaches each
matched rule's policy-authored outcome, applies the portable deterministic
reducer, and validates the complete Decision against that artifact. Fixture
identity, mode, and the synthetic marker remain visible in Decision metadata.

Provide a visibly synthetic deterministic fixture adapter for offline tests and
quickstarts. A versioned FixtureSet maps a named case plus exact policy and input
digests to exact normalized rule results. Missing cases, identity mismatches,
and incomplete mappings are errors; the adapter never guesses, hashes policy
text into a result, reads credentials, performs network access, or substitutes
for a semantic evaluator in enforcement.

## Rationale

A narrow evidence interface keeps provider behavior separate from policy
meaning and deterministic reduction. Digest-bound fixtures exercise the same
request and normalization boundary without credentials or nondeterminism, while
the explicit synthetic marker prevents test evidence from being mistaken for
semantic judgment.
