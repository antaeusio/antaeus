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
reimplementation. The `js/wasm` command only adapts one call to
`internal/host`, which owns the whole request and response contract as JSON
documents and is tested natively. Keep policy, profile, credential, and
reduction behavior in the shared engine packages; the host package may only
parse requests, reject configuration before evaluation with `host.*` codes,
bind supplied credentials through `localbinding.Preflight`, cap the evaluation
at the host's absolute deadline, and return the runner's Decision unchanged.
Bump the interface version for any incompatible change.

Run every call in a fresh engine instance, entered only from the calling
request's own JavaScript context, and discard the instance afterwards.
`antaeus.mjs` owns this, together with admission and waiting; the Go core
refuses overlapping calls. A Go runtime keeps one pending timer and
re-registers it, with any work it resumes, in whichever context enters it, and
Cloudflare Workers cancel a request's timers and I/O when it ends. Local
workerd tests hung with a runtime shared between concurrent requests, with
calls handed from one request to another through promises, and with instances
reused after late callbacks from an earlier request. Cap concurrent instances,
make other calls wait by polling with their own timers, bound waiting calls
with `host.MaxPendingCalls` and `host.MaxPendingBytes` (refusing with
`host.busy`), measure UTF-8 size before a request reaches an instance, and
settle a waiting call at its own deadline. Never let JavaScript interop panic:
guard every host call, use fixed messages for rejection values, and turn engine
panics into `host.internal_error`.

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
per-call instance rule comes from workerd tests with real network I/O, in which
every design that let one request's context enter a runtime used by another
request eventually hung. A JSON
interface is language-neutral and testable without a JavaScript runtime, while
the fetch transport is required because Go's WebAssembly port dials sockets
whenever a client configures a dialer, which Workers cannot do.
