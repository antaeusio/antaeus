---
type: Metatron Decision
title: New System One adapter version per behavior change
status: canonical
scope: adapters/systemone
confidence: high
source_refs:
  - adapters/systemone/systemone.go
  - docs/systemone-adapter.md
  - internal/cli/evaluate_profile.go
---

## Pattern

An adapter identity (`io.antaeus.systemone@<version>`) is part of every
profile digest, so an installed version's accepted providers, error
classification, retry signals, and recorded metadata never change. Adding a
provider or changing any of those behaviors creates a new adapter version.
Earlier versions stay installed in the CLI with their original behavior so
existing profiles keep their digests and results. Gate new behavior on the
adapter's own version inside the shared implementation rather than forking it.

Enable a new System One provider only after its live wire contract is verified
with captured responses, and keep those responses as test fixtures with their
provenance recorded. A hosted provider whose credential is issued by a third
party requires its credential slot and pins its endpoint, so a profile cannot
send that credential to another host.

## Rationale

Profiles are immutable and reviewed by digest; silently changing what an
installed adapter version does would change evaluation behavior without
changing any identity a reviewer can see. Pinning third-party endpoints keeps
a shared or project-supplied profile from redirecting a hosted-service key.
