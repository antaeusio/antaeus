---
type: Metatron Decision
title: Environment-only local secret references
status: canonical
scope: evaluator/local-secret-bindings
confidence: high
source_refs:
  - contracts/schemas/v0alpha1/local-secret-bindings.schema.json
  - contracts/examples/v0alpha1/local-secret-bindings/development.json
  - contracts/README.md
  - evaluator/localbinding
---

## Pattern

Represent local credential bindings as a versioned, non-secret
`LocalSecretBindings` artifact. Bind the tuple of evaluator adapter ID and
profile-local credential slot to an environment-variable reference containing
only `source: environment` and a syntactically valid variable `name`. The
artifact never contains a credential value, command-line value, `.env` path,
hosted secret identifier, or evaluator behavior override.

Resolve only references used by evaluators in the selected profile's configured
route. A usable binding is the tuple of that evaluator's adapter ID and its
`credentialSlot`, which must also be declared by the profile. Ignore and never
resolve bindings for adapters or slots outside the selected route. Read a
referenced variable from the existing process environment immediately before
evaluation starts. Read every configured-route reference exactly once and
capture each non-empty value for that evaluation. Pass a captured value only if
its evaluator is invoked, then release all captured values when evaluation
ends. Do not scan parent directories, load `.env` files, or inspect unrelated
variables.
Load a bindings artifact only from an explicit path; do not discover it through
a directory walk. Treat an unbound routed slot and an unset or zero-length
variable as the same pre-evaluation missing-credential configuration error,
without retry or fallback. Pass non-empty values unmodified; do not trim
whitespace.
The CLI reports this configuration error without starting evaluation; a service
rejects it before accepting an evaluation and creates no Decision. Output in
every mode may identify the adapter, slot, and source class, but must never
print the environment-variable name or value. Selecting an explicit bindings
file grants it authority to reference process variables for routed adapters;
operators must review it as sensitive local configuration. Portable bindings
must not rely on variable names that differ only by letter case.

Load Go binding artifacts through `localbinding.LoadFile` or parse bytes with
`localbinding.Parse`. Immediately before evaluation, call
`localbinding.Preflight` with the validated evaluator profile and an explicit
environment lookup. Preflight reads each distinct configured-route adapter and
slot tuple once, fails before evaluation if any reference is unbound, unset, or
empty, and ignores all unused bindings. The returned credential set exposes
copies only and must be cleared when evaluation ends.

## Rationale

Keeping machine-specific references outside immutable evaluator profiles
preserves profile portability and digest stability. A closed, environment-only
v0 shape prevents raw secrets and behavior changes from entering reusable
artifacts while leaving future secret stores to an explicit contract version.
