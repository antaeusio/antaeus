# Changelog

All notable changes to Antaeus will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and released versions follow the project's [compatibility policy](./docs/compatibility.md) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-09-25

Adds a CPU-only semantic evaluator you can run yourself. The System One adapter
gains provider `antaeus`, which works with the open-source
[`antaeusio/nli-server`](https://github.com/antaeusio/nli-server) and reads its
optional key from `ANTAEUS_API_KEY`. Its quality on policy tasks has not been
measured, so use confidence gating and review before relying on it. Existing
CLM profiles keep working unchanged. Go users who register the System One
adapter themselves should read the migration note below. Public Go APIs and CLI
behavior may still change in minor releases before v1.0.0.

Install with `brew install antaeusio/tap/antaeus` (or `brew upgrade antaeus`),
`go install github.com/antaeusio/antaeus/cmd/antaeus@v0.3.0`,
`docker pull ghcr.io/antaeusio/antaeus:0.3`, or an archive from this release.

### Added

- System One adapter `io.antaeus.systemone@0.2.0` with provider `antaeus` for Antaeus System One servers, such as the open-source CPU-only [`antaeusio/nli-server`](https://github.com/antaeusio/nli-server). It takes an optional `antaeus-api-key` credential (`ANTAEUS_API_KEY`), records the provider in Decisions, and also accepts `contrastive-lm`. `io.antaeus.systemone@0.1.0` stays installed and unchanged, so existing CLM profiles keep working. Example profile: `examples/antaeus/nli-server.json`.

### Changed

- Go API: `systemone.AdapterVersion`, `Identity`, `Registration`, and `DefaultReferences` now refer to version 0.2.0. To keep running 0.1.0 profiles in your own registry, also register `systemone.LegacyIdentity: systemone.LegacyRegistration()` and use `systemone.LegacyDefaultReferences()` for that identity. The CLI installs both versions.

## [0.2.0] - 2026-09-25

Adds a second, self-hosted semantic evaluator and a container image. The
experimental System One adapter runs policies against your own Contrastive
Language Model (CLM) server and returns confidence scores, so low-confidence
judgments can become an explicit `failure` for human review. The CLM model's
quality on policy tasks has not been measured. Antaeus is also published as a
small multi-architecture container image. Public Go APIs and CLI behavior may
still change in minor releases before v1.0.0.

Install with `brew install antaeusio/tap/antaeus` (or `brew upgrade antaeus`),
`go install github.com/antaeusio/antaeus/cmd/antaeus@v0.2.0`,
`docker pull ghcr.io/antaeusio/antaeus:0.2`, or an archive from this release.

### Added

- Container image `ghcr.io/antaeusio/antaeus` for linux/amd64 and linux/arm64, built on a digest-pinned distroless base with CA certificates, running as numeric user 65532 and with build-provenance attestations. Tags are `vX.Y.Z`, plus `X.Y` and `latest` for the newest stable release; pre-releases get only their own tag. `scripts/container-image` builds it locally.
- Experimental System One semantic adapter `io.antaeus.systemone@0.1.0` for a self-hosted Contrastive Language Model (CLM) server. It asks one yes/no question per rule, returns confidence scores for confidence routing, requires `https` (or loopback `http`) endpoints, follows no redirects, and takes an optional `clm-api-key` credential (`CLM_API_KEY`). CLI `evaluate-profile` runs System One profiles and semantic profiles that combine it with the OpenAI adapter; any semantic profile selected by project configuration requires saved project trust, even without a credential, because the profile decides where input is sent. Example profiles are in `examples/clm/`, and the [adapter guide](./docs/systemone-adapter.md) covers setup.
- README with the brand lockup for light and dark themes, a worked marketplace-moderation example with real output, a "how it works" diagram, and a documentation index. The example inputs are in `examples/marketplace/`. `docs/assets/social-preview.png` is the repository's link-preview image.

## [0.1.0] - 2026-09-24

First public release. Antaeus turns human-written policies into versioned,
testable decisions (`allow`, `review`, `deny`, or `failure`) and runs locally
without an account. It includes a credential-free deterministic quickstart and
an **experimental** bring-your-own-key OpenAI semantic evaluator. The semantic
evaluator has not been measured on a frozen corpus and is not suitable for
enforcement. Public Go APIs and CLI behavior may still change in minor
releases before v1.0.0; see [compatibility](./docs/compatibility.md).

Install with `brew install antaeusio/tap/antaeus`, with
`go install github.com/antaeusio/antaeus/cmd/antaeus@v0.1.0`, or by downloading
an archive from this release and checking it against `SHA256SUMS`. Archives
carry GitHub build-provenance attestations
(`gh attestation verify <archive> --repo antaeusio/antaeus`).

### Added

- Release archives for macOS (amd64, arm64), Linux (amd64, arm64), and Windows (amd64) with SHA-256 checksums and build-provenance attestations, plus a Homebrew tap. `antaeus version` reports the module version for `go install` builds.

- Experimental OpenAI Responses semantic adapter `io.antaeus.openai@0.1.0` with strict structured output, `store: false`, a digest-pinned instruction template, a fixed origin without redirects, bounded bodies and typed retryable failures. CLI `evaluate-profile` now runs OpenAI profiles using the `OPENAI_API_KEY` adapter default or explicit bindings, requiring saved project trust for project-supplied credential configuration. See the [adapter guide](./docs/openai-adapter.md).
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

- CLI `evaluate-profile` no longer requires `--fixture-set` and `--case` for semantic profiles; fixture profiles still require both, and semantic profiles reject them. The diagnostic for uninstalled adapters changed from "only the deterministic fixture adapter is installed" to name both installed adapters.
- Preserve UTF-8 character boundaries when truncating valid profile validation messages, retaining the 512-byte message cap, ASCII output, error codes and wrapped causes. This does not sanitize malformed adapter text or change policy/profile identity, Decisions or published schemas.
- Publish complete CLI trust markers atomically without overwriting existing entries, reject dangling intermediate configuration symlinks, and check user/trust directory containment by filesystem identity. Existing marker keys and published schemas remain unchanged; granting trust now requires same-directory hard-link support. See the [configuration migration](./docs/cli-configuration.md#development-migration-for-the-planned-v010-release).
- Align execution metadata and Decision adapter/fixture versions with the existing ECMA-262 nonblank patterns: reject U+FEFF-only text and accept U+0085 as nonblank. Add Go/JavaScript conformance without changing published schemas or other fields' explicit whitespace rules. This affects result/Decision validation, both Go execution paths, `regression.Run`, and CLI `evaluate`, `test` and `evaluate-profile` with unusual fixture versions. A U+FEFF-version fixture still loads but fails later result validation on single-attempt/regression paths; see the [field-specific migration](./docs/execution-text.md).
- Go profile-runner and CLI `evaluate-profile` failure Decisions expose the terminal operational code, append it after the generic unresolved reason, and classify transient retryability only when all unresolved rules failed solely for that cause. Deny precedence, the standalone reducer, legacy commands and published schemas are unchanged. See the [Go/CLI migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release).
- Retries no longer sleep away the final budget when the next jittered delay leaves no time to retry. Eligible fallback evidence can now produce a policy judgment where the old runner failed at the deadline; without recovery, transient trace/rule codes can replace the deadline code. Published schemas and the reducer are unchanged. Cancellation and exhausted deadlines retain precedence. See the [Go/CLI migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release).
- Profile-runner Go callers must explicitly set `AllowSyntheticFixtures` for non-enforcement fixture execution. Enforcement still rejects fixtures. The fixture-only CLI opts in internally; its flags/output and published schemas are unchanged. See the [migration note](./docs/profile-execution.md#development-migration-for-the-planned-v010-release).
- Documented existing adapter panic propagation, deferred cleanup and embedding-host isolation responsibilities; panics do not produce a Decision or completed trace.
- Tightened the unreleased v0alpha1 evaluator metadata so mode and synthetic status are explicit and deterministic fixtures carry exact set and adapter versions.

### Fixed

- `scripts/with-build-lock` keeps the lock until an interrupted command's whole process group has stopped (escalating to KILL after a bounded wait), serializes stale-lock takeover, waits for owners that have not yet written metadata, and no longer treats another user's process as dead. `scripts/test-build-lock` covers these cases ([#42](https://github.com/antaeusio/antaeus/issues/42)).
- `scripts/cross-build` pins `GOAMD64=v1` and `GOARM64=v8.0` instead of inheriting the caller's environment.

[Unreleased]: https://github.com/antaeusio/antaeus/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/antaeusio/antaeus/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/antaeusio/antaeus/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/antaeusio/antaeus/releases/tag/v0.1.0
