// Exercises the built WebAssembly engine (scripts/build-wasm) through its
// JavaScript interface with a stubbed fetch. No network access.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';

const root = new URL('../', import.meta.url);
const dist = new URL('.tmp/dist/antaeus_js_wasm/', root);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
createRequire(import.meta.url)(new URL('wasm_exec.js', dist).pathname);

const policy = read('examples/marketplace/listing-policy.yaml');
const profile = read('examples/drex/profile.json');
const key = 'nace_sk_check';
const request = (extra = {}) => JSON.stringify({
  policy, policyFormat: 'yaml', profile, profileFormat: 'json',
  input: { title: 'Luxury watch, 1:1 replica' }, correlationId: 'check-wasm',
  credentials: [{ adapterId: 'io.antaeus.systemone', slot: 'drex-api-key', value: key }],
  ...extra,
});
const answers = (prohibited, complete) => JSON.stringify({
  model: 'drex-latest',
  answers: { 'prohibited-item': { type: 'noul', noul: prohibited }, 'complete-listing': { type: 'noul', noul: complete } },
});

let calls = [];
let script = [];
globalThis.fetch = (url, init) => {
  calls.push({ url, init, at: performance.now() });
  return script.shift()(init);
};
const reply = (body, status = 200, headers = {}) => () => Promise.resolve(new Response(body, { status, headers }));
async function evaluate(steps, extra) {
  calls = [];
  script = steps;
  const response = JSON.parse(await globalThis.antaeus.evaluate(request(extra)));
  assert(!JSON.stringify(response).includes(key), 'response must not contain the credential');
  return response;
}

const go = new Go();
const { instance } = await WebAssembly.instantiate(readFileSync(new URL('antaeus.wasm', dist)), go.importObject);
go.run(instance);
const engine = globalThis.antaeus;
assert.equal(engine.interfaceVersion, 1);
assert.match(engine.version, /\S/);

const validated = JSON.parse(await engine.validate(JSON.stringify({ policy, policyFormat: 'yaml' })));
assert.equal(validated.ok, true);
assert.equal(validated.policy.name, 'marketplace-listing');
assert.match(validated.policy.digest, /^sha256:[0-9a-f]{64}$/);
assert.equal(JSON.parse(await engine.validate('{"policy":"x","policyFormat":"toml"}')).error.code, 'host.request_invalid');
await assert.rejects(engine.validate(42), TypeError);

// A decision through Drex's endpoint, with the key and no redirects.
let r = await evaluate([reply(answers(0.96, 0.2), 200, { 'x-request-id': 'req_1' })]);
assert.equal(r.ok, true);
assert.equal(r.decision.outcome, 'deny');
assert.equal(calls.length, 1);
assert.equal(calls[0].url, 'https://drex.nace.ai/v1/systemone');
assert.equal(new Headers(calls[0].init.headers).get('authorization'), `Bearer ${key}`);
assert.equal(calls[0].init.redirect, 'manual');
const sent = JSON.parse(new TextDecoder().decode(calls[0].init.body));
assert.deepEqual(Object.keys(sent.questions).sort(), ['complete-listing', 'prohibited-item']);

// A provider wait longer than the profile backoff is honored.
r = await evaluate([reply('{}', 429, { 'retry-after-ms': '300' }), reply(answers(0.05, 0.9))]);
assert.equal(r.decision.outcome, 'allow');
assert.equal(calls.length, 2);
assert(calls[1].at - calls[0].at >= 295, `retried after ${calls[1].at - calls[0].at} ms`);

// A redirect is a failure and is not followed.
r = await evaluate([reply('', 302, { location: 'https://elsewhere.example/' })]);
assert.equal(r.decision.outcome, 'failure');
assert.equal(calls.length, 1);

// A body over the adapter limit is not buffered in full: the stream is cancelled.
let pulled = 0;
let cancelled = false;
r = await evaluate([() => Promise.resolve(new Response(new ReadableStream({
  pull(controller) { pulled += 1; controller.enqueue(new Uint8Array(64 << 10)); },
  cancel() { cancelled = true; },
}), { status: 200 }))]);
assert.equal(r.decision.outcome, 'failure');
assert(cancelled, 'oversized stream was not cancelled');
assert(pulled < 64, `pulled ${pulled} chunks`);

// A fetch that never answers is aborted at the host deadline.
let aborted = false;
const hang = (init) => new Promise((_, reject) => init.signal.addEventListener('abort', () => { aborted = true; reject(new DOMException('aborted', 'AbortError')); }));
r = await evaluate([hang, hang], { deadlineUnixMs: Date.now() + 300 });
assert.equal(r.decision.outcome, 'failure');
assert(aborted, 'request was not aborted');

// A network error is a retryable unavailable failure.
r = await evaluate([() => Promise.reject(new TypeError('network down')), () => Promise.reject(new TypeError('network down'))]);
assert.equal(r.decision.outcome, 'failure');
assert.equal(r.decision.failure.retryable, true);

// Configuration problems are typed errors and make no request.
r = await evaluate([], { credentials: [] });
assert.equal(r.error.code, 'host.credential_missing');
r = await evaluate([], { deadlineUnixMs: Date.now() - 1000 });
assert.equal(r.error.code, 'host.deadline_exceeded');
assert.equal(calls.length, 0);

console.log('check-wasm: WebAssembly engine interface checks passed');
process.exit(0);
