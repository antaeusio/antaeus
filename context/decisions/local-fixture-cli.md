---
type: Metatron Decision
title: Local fixture CLI execution
status: canonical
scope: cli/local-evaluation
confidence: high
source_refs:
  - internal/cli/run.go
  - internal/cli/input.go
  - docs/quickstart.md
  - contracts/examples/v0alpha1/input/aggregate-analytics.json
---

## Pattern

Use `antaeus validate <policy-file>` to load a constrained JSON/YAML policy and
emit one JSON identity summary. Use `antaeus evaluate --policy <file> --input
<file> --fixture-set <file> --case <name>` for a single credential-free local
attempt. Bound and strictly parse input as one JSON object, reject duplicate
keys and invalid Unicode, then canonicalize it with RFC 8785 before computing
fixture identity.

The evaluate command selects only the explicit named case, uses the built-in
profile identity whose exact UTF-8 preimage is
`antaeus.local.fixture/v0alpha1`, invokes the same
provider-neutral evaluator runner as embedders, and emits the complete validated
Decision as one JSON object on stdout. All Decision outcomes, including deny,
review, and typed failure, are successful command execution. Usage errors exit
64; loading, configuration, adapter, and output errors exit 1 and write only to
stderr.

Always describe fixture decisions as synthetic test evidence. They perform no
network access or credential discovery and must not be presented as semantic
inference or an enforcement fallback.

## Rationale

One narrow local path proves policy loading, canonical identity, evaluator
normalization, deterministic reduction, and Decision serialization end to end
without coupling the CLI to a provider or hosted service.
