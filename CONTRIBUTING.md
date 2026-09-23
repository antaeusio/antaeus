# Contributing to Antaeus

Antaeus is in early implementation, with policy validation, deterministic fixture evaluation, regression suites and profile routing. It has no released API or live semantic adapter yet. Contributions should focus on concrete design feedback, documentation corrections, tests, and small changes agreed with the maintainers before implementation.

## Before opening a change

1. Search existing issues and discussions for related work.
2. Open an issue before making a large change, adding a dependency, or proposing a public contract.
3. Keep changes narrowly scoped and avoid combining unrelated cleanup.
4. Never include credentials, production data, private customer data, or unpublished research material.

## Development expectations

- Follow the repository instructions in `AGENTS.md`.
- Use Go 1.26.0 or newer; Go 1.27.1 is the preferred development toolchain.
- Keep the open-source core useful without an Antaeus account.
- Keep public contracts evaluator- and provider-neutral.
- Add tests with behavior changes.
- Run `scripts/check` before submitting Go or shell changes.
- Use `scripts/build` for a local binary and `scripts/cross-build` for the supported target matrix. These entry points acquire `scripts/with-build-lock`; do not invoke underlying build commands directly in a shared checkout.
- Do not claim checks passed unless you ran them.

## Automated validation

Pull requests and pushes to `main` run the [Go validation workflow](./.github/workflows/validate.yml).
It asserts exact Go 1.26.8 and 1.27.1 toolchains and the unchanged Go 1.26.0
language floor, verifies downloaded modules and `go mod tidy` cleanliness, and
runs formatting, pinned ShellCheck, vet, unit and offline contract tests under
both toolchains. The preferred-toolchain job also runs Linux amd64 race tests,
the pinned-Node native-JavaScript execution-text companion, and pure-Go
cross-builds for all five planned binary targets. Actions are pinned to commit
hashes; the ShellCheck release archive is checksum-verified before execution.

CI uses read-only repository permissions, does not retain checkout credentials,
and requires no provider secrets. Tests use local fixtures rather than evaluator
services; dependency/tool downloads are allowed during provisioning. Check,
race and cross-build steps then disable the module proxy and require read-only
module resolution. This is not a network sandbox for test code. The scripts
keep their local build-lock protection even in isolated CI checkouts. New PR
revisions cancel superseded PR runs; main pushes have distinct concurrency
groups so they neither cancel running commits nor replace pending commits.

This workflow does not publish artifacts or deploy anything. Cross-compilation
is not native artifact smoke testing. Vulnerability scanning and release
verification remain separate gates before a release can be published. A passing
workflow does not imply that repository branch-protection settings require it.

## Changes and review

Use short, imperative commit subjects. Pull requests should explain the problem, the chosen approach, validation performed, compatibility effects, and any follow-up work. Meaningful logic requires an independent cross-model review under the policy in `AGENTS.md` before merge.

By participating, you agree to follow the [Code of Conduct](./CODE_OF_CONDUCT.md). Report security issues privately as described in [SECURITY.md](./SECURITY.md).
