# Contributing to Antaeus

Antaeus is at the repository-foundation stage and does not yet have a released API or runnable implementation. Early contributions should focus on concrete design feedback, documentation corrections, and small changes agreed with the maintainers before implementation.

## Before opening a change

1. Search existing issues and discussions for related work.
2. Open an issue before making a large change, adding a dependency, or proposing a public contract.
3. Keep changes narrowly scoped and avoid combining unrelated cleanup.
4. Never include credentials, production data, private customer data, or unpublished research material.

## Development expectations

- Follow the repository instructions in `AGENTS.md`.
- Keep the open-source core useful without an Antaeus account.
- Keep public contracts evaluator- and provider-neutral.
- Add tests with behavior changes once executable code exists.
- Run every build-producing command through `scripts/with-build-lock` in a shared checkout.
- Do not claim checks passed unless you ran them.

There is no supported build or test command yet. Those instructions will be added with the first vertical slice after the remaining architecture decisions are approved.

## Changes and review

Use short, imperative commit subjects. Pull requests should explain the problem, the chosen approach, validation performed, compatibility effects, and any follow-up work. Meaningful logic requires an independent cross-model review under the policy in `AGENTS.md` before merge.

By participating, you agree to follow the [Code of Conduct](./CODE_OF_CONDUCT.md). Report security issues privately as described in [SECURITY.md](./SECURITY.md).
