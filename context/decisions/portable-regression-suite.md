---
type: Metatron Decision
title: Portable deterministic regression suites
status: canonical
scope: regression/contracts
confidence: high
source_refs:
  - regression/regression.go
  - regression/run.go
  - contracts/schemas/v0alpha1/regression-suite.schema.json
  - contracts/schemas/v0alpha1/regression-result-set.schema.json
  - contracts/examples/v0alpha1/regression-suite/quickstart.json
---

## Pattern

Represent offline regression checks as a versioned `RegressionSuite` bound to
one exact policy name and digest plus one exact fixture-set name and version.
Each uniquely named case carries an inline JSON object, selects an explicit
fixture case, and declares the exact terminal outcome and ordered top-level
reason codes expected from deterministic reduction. Strictly validate and RFC
8785-canonicalize every input before evaluation.

Run every case through the same provider-neutral single-attempt evaluator path
and shared fixture profile as local evaluation. The exact UTF-8 profile
preimage is `antaeus.local.fixture/v0alpha1`. Emit a complete
`RegressionResultSet` containing the
expected value and actual validated Decision for every case. Expectation
mismatches are data: mark the case and aggregate result failed while preserving
the actual Decision. Invalid identities, missing fixtures, malformed inputs,
cancellation, and evaluator errors abort the run as operational errors.
The CLI exits 0 for a passing suite, 2 for expectation mismatches after printing
the complete result set, and 1 for operational errors without a result set.

Regression suites and result sets are portable public contracts. Fixture-backed
results remain visibly synthetic, perform no network or credential discovery,
and do not demonstrate semantic evaluator quality.

## Rationale

Exact identity binding prevents a suite from silently exercising different
policy or fixture content. Preserving complete Decisions makes failures
auditable and keeps regression behavior language-neutral without inventing a
second evaluation path.
