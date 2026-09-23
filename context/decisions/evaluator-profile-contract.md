---
type: Metatron Decision
title: Immutable non-secret evaluator profiles
status: canonical
scope: evaluator/profile-contract
confidence: high
source_refs:
  - contracts/schemas/v0alpha1/evaluator-profile.schema.json
  - contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json
  - contracts/examples/v0alpha1/evaluator-profile/semantic-routing.json
  - contracts/README.md
  - evaluator/profile
---

## Pattern

Keep all non-secret evaluator mechanics in an immutable versioned
`EvaluatorProfile`, separate from policy meaning, deployment selection, runtime
secret bindings, and secret values. Profiles declare bounded deadlines,
evaluator adapter and protocol identities, capabilities, retry behavior,
confidence routing, escalation, operational fallbacks, failure-only terminal
behavior, and logical credential slots. Confidence affects routing only and
never synthesizes or replaces allow, review, or deny.

V0 routing is acyclic: one primary, at most one escalation evaluator, then an
ordered bounded fallback list. Preserve unresolved evidence after routing is
exhausted so the deterministic reducer produces failure unless a matched deny
has higher precedence. Deterministic fixtures are visibly synthetic,
credential-free, and prohibited from enforcement routes. A v0 profile cannot
mix semantic and deterministic-fixture evaluators. Eligible operational
failures from either a primary or an invoked escalation continue into the
ordered fallback list.
A fallback after primary failure evaluates all rules. A fallback after
escalation failure evaluates only the escalated rule results, preserving
accepted primary results.

Reserve `json-input`, `structured-rule-results`, and `confidence-scores` as v0
capability IDs. Confidence thresholds apply per rule, missing confidence counts
as below threshold, and only primary evidence can trigger the one escalation.
When confidence routing is enabled, every routed evaluator must declare
`confidence-scores`. Escalation re-evaluates only below-threshold primary rule
results; accepted primary results remain unchanged and escalation evidence
replaces only the re-evaluated results. Low or missing
confidence from escalation or fallback evidence remains indeterminate and
cannot recurse into another escalation.

Profiles never contain credential values, environment-variable names, hosted
secret identifiers, or ambient behavior overrides. Adapter-specific parameters
must validate against the schema registered for the exact adapter ID and
version. The evaluator-profile schema closes the built-in fixture adapter
parameters.
Implementations additionally enforce relational invariants—unique IDs, valid
references, acyclicity, route-position uniqueness, deadline consistency, and
capability-backed confidence—because JSON Schema cannot express the full graph.
The relational validator also requires initial backoff not to exceed maximum
backoff. Instruction templates require a content digest; any template ID is an
optional descriptive label rather than mutable identity.

Load Go profiles through `profile.LoadFile` or parse bytes with `profile.Parse`.
Both use the same constrained JSON/YAML source rules and limits as policies,
then enforce the closed v0alpha1 shape and relational invariants before
canonicalization or digesting. Core parsing validates the built-in fixture
parameters. Before executing any semantic profile, call
`Artifact.ValidateParameters` with a validator registered for each exact
adapter ID and version; loading for inspection does not imply that an installed
adapter accepts its parameter object.

## Rationale

Separating policy semantics from reproducible evaluation mechanics keeps
policies portable while making evaluator behavior reviewable and addressable.
Logical slots let local and hosted runtimes bind secrets without leaking secret
inventory or values into reusable public artifacts.
