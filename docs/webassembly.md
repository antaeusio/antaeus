# WebAssembly engine

The engine is also built as WebAssembly for JavaScript hosts, such as
Cloudflare Workers, browsers, and Node.js. It is the same Go engine as the CLI:
policy validation, evaluator profiles, retries and deadlines, confidence
gating, deterministic reduction, and the Decision record are shared code.
Remote evaluators reach their providers through the host's `fetch`; see
[Network behavior](#network-behavior) for the few differences that follow.

> **Status: experimental.** Interface version 1 may still change in a minor
> release before v1.0.0.

## Files

Each release includes `antaeus_<version>_js_wasm.tar.gz`, covered by
`SHA256SUMS` and build-provenance attestations. It contains:

| File | Purpose |
| --- | --- |
| `antaeus.wasm` | The engine |
| `antaeus.mjs` | The interface module: starts engine instances and exposes `validate` and `evaluate` |
| `wasm_exec.js` | Go's loader for `antaeus.wasm`; use this copy, which matches the Go version that built it |
| `LICENSE-go` | Go's license, which covers `wasm_exec.js` |

To build it yourself, run `scripts/build-wasm`, which writes the same files to
`.tmp/dist/antaeus_js_wasm/`.

## Starting the engine

Load `wasm_exec.js`, which defines `globalThis.Go`, then call `start` from
`antaeus.mjs` with the compiled module or its bytes. Start it once and reuse
the result.

In Cloudflare Workers, call `start` inside a request handler, not at module
scope: Go's runtime reads random values and sets timers while starting, which
Workers do not allow during global-scope execution.

```js
import "./wasm_exec.js";
import { start } from "./antaeus.mjs";
import module from "./antaeus.wasm"; // a compiled WebAssembly.Module, as in Workers

let engine;

export default {
  async fetch(request) {
    engine ??= start(module).catch((error) => { engine = undefined; throw error; });
    const antaeus = await engine;
    const response = JSON.parse(await antaeus.evaluate(JSON.stringify({ /* ... */ })));
    // ...
  },
};
```

`start(module, options)` resolves to `{ interfaceVersion, version, validate,
evaluate }`. `interfaceVersion` is `1` and `version` is the build version, for
example `v0.5.0 (abc123def456)`. Options:

| Option | Default | Meaning |
| --- | --- | --- |
| `maxInstances` | 2 | Calls that may run at the same time in this JavaScript isolate or page |
| `maxWaitMs` | 10000 | How long a call without `deadlineUnixMs` waits for a free instance before returning `host.busy` |

## How calls run

Every call runs in a fresh engine instance that is entered only from the
calling request's own JavaScript context and discarded afterwards; instances
share the compiled module, so starting one takes milliseconds. A Go runtime
keeps one pending timer and re-registers it, along with any work it resumes,
in whichever context enters it. In Cloudflare Workers, timers and I/O
registered in a request's context are cancelled when that request ends, so a
runtime shared between requests, or reused after one, can lose its deadline
timer and its provider requests and hang.

When `maxInstances` calls are running, a new call waits by polling with its own
timers. At most 16 calls, totalling at most 16 MiB of request text, may wait;
further calls return `host.busy` at once. Waiting requests stay JavaScript
strings. An `evaluate` call with `deadlineUnixMs` that is still waiting at its
deadline returns `host.deadline_exceeded` then; a call that has started ends
within its deadline as usual. A call that stops waiting leaves nothing behind.
If an instance exits during a call, for example on a fatal error such as
running out of memory, that call returns `host.internal_error`.

## Calls

Both calls take one JSON string and return a promise of a JSON string. The
promise resolves for every call with a string argument, including internal
failures; it rejects only when the argument is not a string.

Every response has this shape; exactly one of `policy`, `decision`, or `error`
is present:

```json
{"ok": true, "decision": { "apiVersion": "decision.antaeus.io/v0alpha1", "...": "..." }}
{"ok": false, "error": {"code": "host.credential_missing", "message": "..."}}
```

### `engine.validate(request)`

```json
{"policy": "<policy source>", "policyFormat": "yaml"}
```

Returns `policy` with the policy's `name`, `digest`, and number of `rules`.

### `engine.evaluate(request)`

```json
{
  "policy": "<policy source>",
  "policyFormat": "yaml",
  "profile": "<evaluator profile source>",
  "profileFormat": "json",
  "input": {"title": "Canon AE-1 in good condition"},
  "correlationId": "req-123",
  "deadlineUnixMs": 1790510000000,
  "credentials": [
    {"adapterId": "io.antaeus.systemone", "slot": "drex-api-key", "value": "<key>"}
  ]
}
```

| Field | Requirement |
| --- | --- |
| `policyFormat`, `profileFormat` | `yaml` or `json` |
| `input` | a JSON object |
| `correlationId` | the Decision's correlation ID |
| `deadlineUnixMs` | optional absolute deadline in Unix milliseconds. The evaluation ends at the earlier of this and the profile's `totalTimeoutMs`, and a call still waiting for an instance ends there too. A host that waited before calling, for example for a concurrency slot, passes its original deadline so the wait counts against it. |
| `credentials` | values for the profile's credential slots, matched by adapter ID and slot. Entries for slots the profile's configured route does not use are ignored without validation. Values are used for this evaluation only and never appear in responses. |

An accepted evaluation always returns a `decision`, including `failure`
Decisions for provider errors and timeouts, exactly as the CLI does.

Installed adapters are `io.antaeus.openai@0.1.0` and
`io.antaeus.systemone@0.3.0`, `0.2.0`, and `0.1.0`. The deterministic fixture
adapter is not installed.

## Errors

These are returned before evaluation starts; no provider is called.

| Code | Meaning |
| --- | --- |
| `host.request_invalid` | Not exactly one valid JSON object for the call (duplicate keys at any depth, trailing data, and a second document are rejected), an unknown field, an unsupported format, a repeated credential, or a request over 4 MiB of UTF-8 |
| `host.policy_invalid` | The policy failed to parse or validate |
| `host.profile_invalid` | The evaluator profile failed to parse or validate |
| `host.input_invalid` | The input is not a valid JSON object |
| `host.adapter_not_installed` | The profile names an adapter this build lacks, or an evaluator its adapter rejects |
| `host.credential_missing` | A slot on the configured route has no value |
| `host.deadline_exceeded` | `deadlineUnixMs` passed before evaluation started, including while the call was waiting |
| `host.evaluation_not_started` | Any other configuration error found when starting |
| `host.busy` | Too many calls are waiting, or none became free within `maxWaitMs`; the call did not start and may be retried |
| `host.internal_error` | An internal failure prevented completion. No Decision is available. Provider requests may already have occurred, so retrying is not necessarily safe. Later calls are unaffected. |

## Network behavior

Requests use the host's global `fetch` with `redirect: "manual"`; a redirect
is an adapter failure and is never followed. The host is responsible for TLS.
When a request's deadline passes, the engine aborts it with an
`AbortController`. Response bodies are read as streams, and each read copies
only the bytes the adapter asks for, so an adapter's response size limit also
bounds the engine's own allocations, however large the host's chunks are.
Memory the host's `fetch` implementation allocates for those chunks is outside
the engine's control. A `fetch` that throws, rejects, or
returns something other than a response is a provider failure, never a crash.

Two results differ from native builds:

- `fetch` does not say why a connection failed, so a TLS verification failure
  is the retryable `evaluator.unavailable` rather than the adapter's
  non-retryable TLS code.
- Browsers answer `redirect: "manual"` with status 0, which adapters report
  as an unexpected status rather than a redirect. Workers and Node.js return
  the real redirect status.

## Limits

The module is about 14 MB, or about 3.6 MB compressed: within the Workers
Paid plan's 10 MB compressed limit but over the Free plan's 3 MB. Requests
over 4 MiB of UTF-8 are rejected before they reach an engine instance. Each
running instance holds its own memory until it is discarded, so peak engine
memory grows with `maxInstances`; measure it against your host's limits, such
as 128 MB per Cloudflare Worker isolate.
