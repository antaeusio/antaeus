# Contributing to Antaeus

Antaeus is in early implementation, with policy validation, deterministic fixture evaluation, regression suites and profile routing. v0.1.0 is its first release; the OpenAI semantic adapter is experimental and the public API is pre-v1. Contributions should focus on concrete design feedback, documentation corrections, tests, and small changes agreed with the maintainers before implementation.

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

## Local contribution workflow

You do not need an Antaeus account, a hosted-service subscription, provider API
keys, or access to a private repository to build, test, or contribute to this
repository. Do not create an `.env` file for the workflow below. GitHub access
is needed to submit an issue or pull request, not to run the local checks.

Install Git, Go and ShellCheck so they are available on `PATH`. The commands
below use a POSIX shell on macOS or Linux; Windows contributors can use a Linux
environment in WSL. This does not claim native Windows validation of these shell
scripts. The Go versions above match the current module and CI; CI pins
ShellCheck 0.11.0. Cloning and initial tool/module downloads require network
access. Once provisioned, the ordinary tests use local synthetic fixtures, not
provider services; the vulnerability scan separately needs the live advisory
database as described below.

Start with a clone of the public repository:

```sh
git clone https://github.com/antaeusio/antaeus.git
cd antaeus
scripts/check
scripts/build
.tmp/bin/antaeus version
.tmp/bin/antaeus validate contracts/examples/v0alpha1/policy/vendor-onboarding.yaml
```

`scripts/check` checks formatting and shell scripts, runs `go vet`, and runs the
Go test suite. It owns the build lock: do not wrap it in another lock. Build
outputs stay under ignored `.tmp/`. Continue with the
[local fixture quickstart](./docs/quickstart.md) to evaluate a named synthetic
case and run a regression suite; neither step requires a credential or makes a
semantic model call.

For a focused change, run the relevant package through the lock before the
complete checks, for example:

```sh
scripts/with-build-lock go test -count=1 ./evaluator/runner
```

Changes to execution-text validation also need the
[native JavaScript conformance check](./scripts/check-text-conformance.mjs):
`node scripts/check-text-conformance.mjs` (CI uses Node 24.21.0). Run build and
test commands sequentially in a shared checkout, including when a reviewer is
working there.

Use synthetic inputs in new tests and report the commands actually run in the
pull request. Missing provider access is not a reason to skip the local checks.
Contract changes must follow the [compatibility rules](./docs/compatibility.md);
do not edit a published schema version to make a failing test pass.

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

Both toolchain jobs also run pinned `govulncheck` v1.8.0 source analysis including
test packages, against the current Go vulnerability database. This step may
download the scanner's separate dependency graph and query `vuln.go.dev`; it
does not add a runtime dependency or execute the scanned tests. It uses text
output and symbol-level scanning so reachable findings fail the job, as do
scanner/download/database errors. There are no exclusions or ignored exit codes.
Verbose output retains package/module-only findings for review even when the
symbol gate passes. See [vulnerability scanning](./docs/vulnerability-scanning.md)
for reproduction, coverage limits and finding dispositions.

This workflow does not publish artifacts or deploy anything. Cross-compilation
is not native artifact smoke testing. Release verification remains a separate
gate before a release can be published. A passing
workflow does not imply that repository branch-protection settings require it.

## Changes and review

Use short, imperative commit subjects. Pull requests should explain the problem, the chosen approach, validation performed, compatibility effects, and any follow-up work. Meaningful logic requires an independent cross-model review under the policy in `AGENTS.md` before merge.

By participating, you agree to follow the [Code of Conduct](./CODE_OF_CONDUCT.md). Report security issues privately as described in [SECURITY.md](./SECURITY.md).
