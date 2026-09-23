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
---

## Pattern

Represent local credential bindings as a versioned, non-secret
`LocalSecretBindings` artifact. Bind the tuple of evaluator adapter ID and
profile-local credential slot to an environment-variable reference containing
only `source: environment` and a syntactically valid variable `name`. The
artifact never contains a credential value, command-line value, `.env` path,
hosted secret identifier, or evaluator behavior override.

Resolve only references required by the selected profile. Read a referenced
variable from the existing process environment as late as possible, and do not
scan parent directories, load `.env` files, or inspect unrelated variables.
Ordinary output and logs may identify the adapter, slot, and source class, but
must redact the environment-variable name and value.

## Rationale

Keeping machine-specific references outside immutable evaluator profiles
preserves profile portability and digest stability. A closed, environment-only
v0 shape prevents raw secrets and behavior changes from entering reusable
artifacts while leaving future secret stores to an explicit contract version.
