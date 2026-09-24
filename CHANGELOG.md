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

- Preserve UTF-8 character boundaries when truncating valid profile validation messages, retaining the 512-byte message cap, ASCII output, error codes and wrapped causes. This does not sanitize malformed adapter text or change policy/profile identity, Decisions or published schemas.
- For planned v0.1.0, publish complete CLI trust markers atomically without overwriting existing entries, reject dangling intermediate configuration symlinks, and check user/trust directory containment by filesystem identity. Existing marker keys and published schemas remain unchanged; granting trust now requires same-directory hard-link support. See the [configuration migration](./docs/cli-configuration.md#development-migration-for-the-planned-v010-release).
- For planned v0.1.0, align execution metadata and Decision adapter/fixture versions with the existing ECMA-262 nonblank patterns: reject U+FEFF-only text and accept U+0085 as nonblank. Add Go/JavaScript conformance without changing published schemas or other fields' explicit whitespace rules. This affects result/Decision validation, both Go execution paths, `regression.Run`, and CLI `evaluate`, `test` and `evaluate-profile` with unusual fixture versions. A U+FEFF-version fixture still loads but fails later result validation on single-attempt/regression paths; see the [field-specific migration](./docs/execution-text.md).
- For planned v0.1.0, Go profile-runner and CLI `evaluate-profile` failure Decisions expose the terminal operational code, append it after the generic unresolved reason, and classify transient retryability only when all unresolved rules failed solely for that cause. Deny precedence, the standalone reducer, legacy commands and published schemas are unchanged. See the [Go/CLI migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release).
- For planned v0.1.0, retries no longer sleep away the final budget when the next jittered delay leaves no time to retry. Eligible fallback evidence can now produce a policy judgment where the old runner failed at the deadline; without recovery, transient trace/rule codes can replace the deadline code. Published schemas and the reducer are unchanged. Cancellation and exhausted deadlines retain precedence. See the [Go/CLI migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release).
- For the planned first minor release v0.1.0, profile-runner Go callers must explicitly set `AllowSyntheticFixtures` for non-enforcement fixture execution. Enforcement still rejects fixtures. The fixture-only CLI opts in internally; its flags/output and published schemas are unchanged. See the [migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release). No release has been published.
- Documented existing adapter panic propagation, deferred cleanup and embedding-host isolation responsibilities; panics do not produce a Decision or completed trace.
- Tightened the unreleased v0alpha1 evaluator metadata so mode and synthetic status are explicit and deterministic fixtures carry exact set and adapter versions.
