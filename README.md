# Antaeus

Antaeus is an open-source project for turning human-written semantic business policies into versioned, testable, auditable decisions that applications can safely consume.

> **Project status:** early, pre-v1. [v0.1.0](https://github.com/antaeusio/antaeus/releases/tag/v0.1.0) is the first release. It contains portable policy, decision, fixture, and regression contracts; constrained JSON/YAML policy loading; a provider-neutral evaluator boundary; a deterministic synthetic fixture evaluator; local validation, evaluation, and regression commands; and an experimental bring-your-own-key OpenAI semantic evaluator that has not yet been measured for accuracy.

## Install

```sh
brew install antaeusio/tap/antaeus                            # macOS or Linux
go install github.com/antaeusio/antaeus/cmd/antaeus@latest    # Go 1.26.0 or newer
antaeus version
```

Prebuilt archives for macOS, Linux, and Windows are attached to each
[GitHub release](https://github.com/antaeusio/antaeus/releases) with a
`SHA256SUMS` file and build-provenance attestations
(`gh attestation verify <archive> --repo antaeusio/antaeus`). Then follow the
[five-minute quickstart](./docs/quickstart.md), or try the
[OpenAI evaluator](./docs/openai-adapter.md) with your own API key.

## What Antaeus is for

Antaeus is intended for bounded decisions that require interpretation of language, intent, context, or exceptions.

Authentication answers **who**. Authorization answers **what they may do**. Antaeus addresses whether specific content or application state complies with a semantic business policy.

Antaeus does not replace authentication, authorization, deterministic business rules, transactions, or application side effects. If a requirement can be expressed safely and completely as ordinary deterministic code, it does not belong in Antaeus.

## Planned open-source capabilities

The public project is intended to work without an Antaeus account. Its planned scope includes:

- portable policy schemas and local validation;
- named policy cases and regression testing;
- a credential-free deterministic evaluator for repeatable tests;
- provider-neutral evaluator interfaces and conformance fixtures;
- optional local or bring-your-own-credential semantic evaluators;
- typed `allow`, `review`, `deny`, and `failure` outcomes;
- behavioral comparison across policy versions;
- a command-line interface and intentionally supported embeddable packages; and
- examples and release artifacts for supported platforms.

Provider credentials, customer data, hosted-service implementation, billing, and private production integrations do not belong in this repository.

## Development status

The Go module is `github.com/antaeusio/antaeus`. Development requires Go 1.26.0 or newer; Go 1.27.1 is the preferred toolchain. The current command exposes help and version information plus policy validation, deterministic local fixture evaluation, portable offline regression suites, and an experimental bring-your-own-key OpenAI semantic evaluator.

Use [`evaluate-profile`](./docs/profile-execution.md#cli-fixture-profiles) to run
an explicitly selected evaluator profile, with configuration precedence and a
bounded execution trace. Fixture profiles use the installed synthetic fixture and
read no credentials. The experimental [OpenAI adapter](./docs/openai-adapter.md)
performs real semantic evaluation with your own `OPENAI_API_KEY`; it sends policy
conditions and input to OpenAI and is not yet validated for enforcement.

For Go embedding and adapter authors, the [evaluator guide](./docs/evaluator-adapters.md)
explains normalized evidence, registration and execution boundaries, with an
executable fixture-backed example.

The [policy authoring guide](./docs/policy-authoring.md) covers the existing
artifact fields, constrained source syntax, valid/invalid examples and digest identity.

See [reading a Decision](./docs/decisions.md) for outcomes, failure details,
fallback boundaries and validation responsibilities.

The language-neutral [portable contracts](./contracts/README.md) contain the authoritative JSON Schemas, OpenAPI description, examples, and conformance fixtures. The public `policy` package strictly loads constrained YAML 1.2 or JSON before validation, canonicalization, and digesting; `decision` implements typed invariants and deterministic reduction. The `evaluator` package defines normalized evidence exchange and can assemble a validated Decision from one evaluation, while `evaluator/fixture` provides an exact, network-free synthetic adapter. The `regression` package runs identity-bound named cases through that same path; see the [local fixture quickstart](./docs/quickstart.md).

Run checks and build the development command through the repository-owned build lock:

```sh
scripts/check
scripts/build
.tmp/bin/antaeus version
```

The command can validate a policy, execute an exact synthetic fixture case, and
run a portable named regression suite locally. This credential-free path is test
plumbing, not semantic model inference; see the [five-minute quickstart](./docs/quickstart.md).

Build every supported binary target with `scripts/cross-build`. Generated files stay under `.tmp/`; published archives come only from the tag-triggered release workflow described in [releasing](./docs/releasing.md).

Do not treat proposed behavior as released functionality. The [changelog](./CHANGELOG.md) and release notes identify what is actually available.

See [compatibility and platform support](./docs/compatibility.md) for the pre-v1 compatibility policy and planned release matrix.

## Contributing and security

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the contribution process, [GOVERNANCE.md](./GOVERNANCE.md) for decision-making, and [SECURITY.md](./SECURITY.md) for private vulnerability reporting. Participation is governed by [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).

All build-producing commands in a shared checkout must run through the checked-in scripts, which use `scripts/with-build-lock`.

## License

Antaeus is licensed under the [Apache License 2.0](./LICENSE).
