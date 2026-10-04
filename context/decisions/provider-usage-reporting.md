---
type: Metatron Decision
title: Provider accounting in a separate Decision extension
status: proposed
scope: evaluator/provider-usage
confidence: high
source_refs:
  - docs/provider-usage.md
  - contracts/schemas/v0alpha1/usage-report.schema.json
  - evaluator/runner/usage.go
  - adapters/systemone/usage.go
---

## Pattern

Report provider token accounting in the independent versioned
`io.antaeus.usage` Decision extension. Preserve the closed v0alpha1 execution
trace; join exactly one accounting record per invoked attempt by its ordinal
trace index. Opt in by immutable adapter version. Preserve usage even when
evidence is invalid or the attempt fails, with explicit unavailable and invalid
states rather than fabricated zero counters. Validate bounded, interoperable
integers and copy only counters and safe adapter failure codes. Provider usage
does not change policy semantics or determine customer charges.

## Rationale

The Decision already supports separately versioned reverse-DNS extensions.
Using that mechanism retains released trace and host compatibility while
making retries, fallback costs, and incomplete accounting visible. Reporting
usage on a new System One adapter version preserves existing profile behavior.
This proposal requires human-reviewed PR acceptance before becoming canonical.
