# WebAssembly engine

The engine is also built as WebAssembly for JavaScript hosts, such as
Cloudflare Workers, browsers, and Node.js. It is the same Go engine as the CLI:
policy validation, evaluator profiles, retries and deadlines, confidence
gating, deterministic reduction, and the Decision record all behave
identically. Remote evaluators reach their providers through the host's
`fetch`.

> **Status: experimental.** Interface version 1 may still change in a minor
> release before v1.0.0.

## Files

Each release includes `antaeus_<version>_js_wasm.tar.gz`, covered by
`SHA256SUMS` and build-provenance attestations. It contains:

| File | Purpose |
| --- | --- |
| `antaeus.wasm` | The engine |
| `wasm_exec.js` | Go's loader for `antaeus.wasm`; use this copy, which matches the Go version that built it |
| `LICENSE-go` | Go's license, which covers `wasm_exec.js` |

To build it yourself, run `scripts/build-wasm`, which writes the same files to
`.tmp/dist/antaeus_js_wasm/`.

## Starting the engine

Load `wasm_exec.js`, which defines `globalThis.Go`, then instantiate and run
the module. The engine installs `globalThis.antaeus` and keeps running until
the host discards it. Start it once per isolate or page and reuse it.

```js
import "./wasm_exec.js";
import module from "./antaeus.wasm"; // a compiled WebAssembly.Module, as in Workers

const go = new Go();
const instance = await WebAssembly.instantiate(module, go.importObject);
go.run(instance); // returns a promise that settles only if the engine exits
const engine = globalThis.antaeus;
```

`engine.interfaceVersion` is `1` and `engine.version` is the build version, for
example `v0.5.0 (abc123def456)`.

## Calls

Both calls take one JSON string and return a promise of a JSON string. The
promise resolves for every well-formed call; it rejects only when the argument
is not a string.

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
| `deadlineUnixMs` | optional absolute deadline in Unix milliseconds. The evaluation ends at the earlier of this and the profile's `totalTimeoutMs`. A host that waited before calling, for example for a concurrency slot, passes its original deadline so the wait counts against it. |
| `credentials` | values for the profile's credential slots, matched by adapter ID and slot. Only slots on the profile's configured route are read; others are ignored. Values are used for this evaluation only and never appear in responses. |

An accepted evaluation always returns a `decision`, including `failure`
Decisions for provider errors and timeouts, exactly as the CLI does.

Installed adapters are `io.antaeus.openai@0.1.0` and
`io.antaeus.systemone@0.3.0`, `0.2.0`, and `0.1.0`. The deterministic fixture
adapter is not installed.

## Errors

These are returned before evaluation starts; no provider is called.

| Code | Meaning |
| --- | --- |
| `host.request_invalid` | Not valid JSON for the call, an unknown field, an unsupported format, a repeated credential, or a request over 4 MiB |
| `host.policy_invalid` | The policy failed to parse or validate |
| `host.profile_invalid` | The evaluator profile failed to parse or validate |
| `host.input_invalid` | The input is not a valid JSON object |
| `host.adapter_not_installed` | The profile names an adapter this build lacks, or an evaluator its adapter rejects |
| `host.credential_missing` | A slot on the configured route has no value |
| `host.deadline_exceeded` | `deadlineUnixMs` passed before evaluation started |
| `host.evaluation_not_started` | Any other configuration error found when starting |

## Network behavior

Requests use the host's global `fetch` with `redirect: "manual"`; a redirect
is an adapter failure and is never followed. The host is responsible for TLS.
When a request's deadline passes, the engine aborts it with an
`AbortController`. Response bodies are read as streams, so an adapter's
response size limit also bounds memory.

## Limits

The module is about 14 MB, or about 3.6 MB compressed. Go's WebAssembly port
runs on one thread; concurrent calls interleave while waiting on `fetch`. The
engine's memory grows with concurrent evaluations and is not returned to the
host. Measure memory against your host's limits, such as 128 MB per Cloudflare
Worker isolate.
