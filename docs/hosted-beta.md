# Local core and hosted beta

Antaeus provides an open-source semantic policy engine and a hosted beta.
Both return explicit `allow`, `review`, `deny`, or `failure` outcomes. Your
application owns authentication, authorization, deterministic checks,
transactions, and the action taken after a Decision.

## Open-source engine and CLI

This repository contains the Apache-2.0 engine, CLI, portable contracts,
evaluator adapters, and local examples. A hosted Antaeus account is not
required to build, test, or use the core.

- The [local quickstart](./quickstart.md) uses synthetic deterministic fixture
  evidence, reads no credentials, and makes no network calls. It is not
  semantic model inference.
- Semantic evaluation uses a configured evaluator, such as the experimental
  [OpenAI adapter](./openai-adapter.md) or a
  [System One provider](./systemone-adapter.md). Provider setup and credentials
  are separate from hosted Antaeus workspace access.
- The [WebAssembly engine](./webassembly.md) supports embedding the engine in
  other runtimes.

## Hosted beta

The hosted service is available at [app.antaeus.io](https://app.antaeus.io).
Its [public documentation](https://antaeus.io/docs) describes the current
integration and is the source for hosted API behavior, limits, and pricing.

The beta includes:

- Workspace API keys for backend requests to the hosted Decisions API
- Managed policies with draft editing, testing, and publication of immutable
  policy versions identified by digest
- Inline-policy evaluation or evaluation against a published policy reference
- Usage and request history in the dashboard
- Prepaid paid usage, subject to the published
  [pricing](https://antaeus.io/docs/pricing) and
  [limits](https://antaeus.io/docs/limits)

The service is experimental, has shared capacity, and has no production SLA.
Start in shadow mode and keep significant decisions under human review. Handle
`review`, `deny`, and `failure` explicitly; operational failure must never
silently become permission to proceed.

To integrate, follow the [hosted quickstart](https://antaeus.io/docs): create a
workspace API key, keep it in your backend's environment or secret store, and
send a synthetic input before connecting application data. Never put API keys
in frontend code, Git, logs, URLs, or coding-agent conversations. Review the
[privacy notice](https://antaeus.io/privacy) before submitting inputs or policy
content to the service.

Managed policy maintenance also supports the
[documented HTTP API](https://antaeus.io/docs#policy-management). Hosted sync
currently uses HTTP; this repository does not provide an `antaeus sync` CLI
command. Local evaluator profiles and credential bindings do not configure
the hosted service.

This overview reflects the public hosted documentation checked on 9 October
2026. Consult the linked service docs for current details rather than treating
this repository's release version as the hosted service version.
