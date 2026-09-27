---
type: Metatron Decision
title: WebAssembly engine behind a versioned JSON host interface
status: canonical
scope: host/webassembly
confidence: high
source_refs:
  - cmd/antaeus-wasm/main.go
  - internal/host/host.go
  - internal/remote/client_js.go
  - docs/webassembly.md
  - scripts/build-wasm
  - scripts/check-wasm.mjs
---

## Pattern

JavaScript hosts run the same Go engine compiled to WebAssembly, never a
reimplementation. The `js/wasm` command only adapts `globalThis.antaeus` to
`internal/host`, which owns the whole request and response contract as JSON
documents and is tested natively. Keep policy, profile, credential, and
reduction behavior in the shared engine packages; the host package may only
parse requests, reject configuration before evaluation with `host.*` codes,
bind supplied credentials through `localbinding.Preflight`, cap the evaluation
at the host's absolute deadline, and return the runner's Decision unchanged.
Bump the interface version for any incompatible change.

In `js/wasm` builds, remote adapters reach providers only through the host's
`fetch` with `redirect: "manual"`, abort on context cancellation, and stream
bodies so adapter size limits bound memory. JavaScript callbacks stay
registered until their promise settles. Ship the matching `wasm_exec.js` and
Go's license with the module, and build it only through `scripts/build-wasm`.

## Rationale

One engine keeps hosted and local Decisions identical and auditable. A JSON
interface is language-neutral and testable without a JavaScript runtime, while
the fetch transport is required because Go's WebAssembly port dials sockets
whenever a client configures a dialer, which Workers cannot do.
