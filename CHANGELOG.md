# Changelog

All notable changes to Antaeus will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and released versions follow the project's [compatibility policy](./docs/compatibility.md) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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

### Changed

- Tightened the unreleased v0alpha1 evaluator metadata so mode and synthetic status are explicit and deterministic fixtures carry exact set and adapter versions.
