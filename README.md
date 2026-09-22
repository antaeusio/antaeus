# Antaeus

Antaeus is an open-source project for turning human-written semantic business policies into versioned, testable, auditable decisions that applications can safely consume.

> **Project status:** early implementation. The repository contains the initial portable policy and decision contracts, constrained JSON/YAML policy loading, a provider-neutral evaluator boundary, a deterministic synthetic fixture evaluator, and a minimal Go command foundation, but there is no release or semantic evaluator yet.

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

The Go module is `github.com/antaeusio/antaeus`. Development requires Go 1.26.0 or newer; Go 1.27.1 is the preferred toolchain. The current command exposes help and version information plus early policy validation and deterministic local fixture evaluation; no semantic evaluator or release artifact exists yet.

The language-neutral [portable contracts](./contracts/README.md) contain the authoritative JSON Schemas, OpenAPI description, examples, and conformance fixtures. The public `policy` package strictly loads constrained YAML 1.2 or JSON before validation, canonicalization, and digesting; `decision` implements typed invariants and deterministic reduction. The `evaluator` package defines normalized evidence exchange and can assemble a validated Decision from one evaluation, while `evaluator/fixture` provides an exact, network-free synthetic adapter for tests and the [local fixture quickstart](./docs/quickstart.md).

Run checks and build the development command through the repository-owned build lock:

```sh
scripts/check
scripts/build
.tmp/bin/antaeus version
```

The command can now validate a policy and execute an exact synthetic fixture
case locally. This credential-free path is test plumbing, not semantic model
inference; see the [five-minute quickstart](./docs/quickstart.md).

Build every planned binary target with `scripts/cross-build`. Generated files stay under `.tmp/` and are not release artifacts.

Do not treat proposed behavior as released functionality. The changelog and future release notes will identify what is actually available.

See [compatibility and platform support](./docs/compatibility.md) for the pre-v1 compatibility policy and planned release matrix.

## Contributing and security

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the contribution process, [GOVERNANCE.md](./GOVERNANCE.md) for decision-making, and [SECURITY.md](./SECURITY.md) for private vulnerability reporting. Participation is governed by [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).

All build-producing commands in a shared checkout must run through the checked-in scripts, which use `scripts/with-build-lock`.

## License

Antaeus is licensed under the [Apache License 2.0](./LICENSE).
