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

Serialize calls: the engine runs one call at a time, queued through
JavaScript promises created in each caller's context. One Go runtime serves
every call, and interleaved calls would run one request's work inside another
request's JavaScript context, which Cloudflare Workers cancel when that request
ends. Never replace the queue with a Go-side lock, and never let JavaScript
interop panic: guard every host call, describe any rejection value safely, and
turn engine panics into `host.internal_error` so the engine keeps serving.
Reject oversized requests before copying them into Go memory, which is never
returned to the host.

In `js/wasm` builds, remote adapters reach providers only through the host's
`fetch` with `redirect: "manual"`, abort on context cancellation, and stream
bodies, copying only the bytes each read requests so adapter size limits bound
the engine's allocations. Decode request envelopes with `strictsource`, like
every other source document. Keep the shared parity cases in
`internal/host/testdata/parity` passing on both the native and WebAssembly
builds. JavaScript callbacks stay
registered until their promise settles. Ship the matching `wasm_exec.js` and
Go's license with the module, build it only through `scripts/build-wasm`, and
test the packaged archive with `scripts/check-wasm.mjs` before release.

## Rationale

One engine keeps hosted and local Decisions identical and auditable. The
serialization rule comes from a workerd test in which concurrent evaluations
with real network I/O hung until they were serialized. A JSON
interface is language-neutral and testable without a JavaScript runtime, while
the fetch transport is required because Go's WebAssembly port dials sockets
whenever a client configures a dialer, which Workers cannot do.
