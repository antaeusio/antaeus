# Changelog

All notable changes to Antaeus will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and released versions follow the project's [compatibility policy](./docs/compatibility.md) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Profile-driven execution with bounded retries, deadlines, confidence escalation, operational fallbacks, and portable attempt traces.
- CLI configuration inspection, credential checks, and project-scoped digest trust/revocation with strict local manifests.
- Local profile and credential-reference precedence with immutable snapshots, project-digest trust checks, and redacted source summaries.
- Initial repository foundation.
- Go module and thin command foundation with version reporting.
- Locked local validation and supported-target cross-build entry points.
- Pre-v1 compatibility and planned platform-support documentation.
- Portable v0alpha1 policy, decision request, Decision, and Problem Details schemas.
- OpenAPI 3.1.2 description, examples, and offline conformance fixtures.
- Public Go policy and decision types, canonical policy digests, validation, and deterministic reduction.
- Provider-neutral evaluator types plus a digest-bound deterministic fixture-set contract and adapter.
- Local evaluator execution that attaches policy outcomes after evidence, reduces deterministically, and records explicit fixture identity in Decision metadata.
- Constrained JSON and YAML 1.2 policy loading with resource limits, stable parser errors, and language-neutral rejection fixtures.
- CLI policy validation and exact, credential-free local fixture evaluation with RFC 8785 input canonicalization.

### Changed

- For planned v0.1.0, retries no longer sleep away the final budget when the next jittered delay leaves no time to retry. Eligible fallbacks may run sooner; without one, the last transient failure returns promptly. This changes Go/CLI execution timing and possibly the recorded route, not published schemas. Cancellation and exhausted deadlines retain precedence.
- For the planned first minor release v0.1.0, profile-runner Go callers must explicitly set `AllowSyntheticFixtures` for non-enforcement fixture execution. Enforcement still rejects fixtures. The fixture-only CLI opts in internally; its flags/output and published schemas are unchanged. See the [migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release). No release has been published.
- Documented existing adapter panic propagation, deferred cleanup and embedding-host isolation responsibilities; panics do not produce a Decision or completed trace.
- Tightened the unreleased v0alpha1 evaluator metadata so mode and synthetic status are explicit and deterministic fixtures carry exact set and adapter versions.
