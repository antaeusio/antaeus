# Repository Context

## Intent
Antaeus is an independently useful open-source engine and CLI for turning
human-written semantic business policies into versioned, testable, auditable
decisions. Prefer portable contracts, deterministic behavior where possible,
explicit failure, bounded evaluator interfaces, and clean security boundaries
over provider convenience or hosted-service coupling.

## Constraints
- Binding conventions for this repository live as one decision per file under
  `context/decisions/` (Open Knowledge Format). Read the contents of the relevant
  files there before planning or modifying code — opening the files, not just
  listing the directory; they are part of this context.
- Decision files are authored and edited on working branches and land only via
  human-reviewed pull requests (review gate: `pr`).

## Evolved Context
<!-- Dated, temporal observations only ([YYYY-MM-DD] observation) — facts that
     will age out, like a pinned version or an environment quirk. Append, never
     rewrite or reorder. New conventions belong in context/decisions/ as decision
     files authored on a working branch and landed via a reviewed pull request;
     refinements of an existing decision are proposed the same way. -->
