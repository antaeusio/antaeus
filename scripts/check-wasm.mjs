// Exercises the built WebAssembly engine through its JavaScript interface with
// a stubbed fetch. No network access. Usage:
//   node scripts/check-wasm.mjs [engine directory]
// The directory defaults to scripts/build-wasm's output and must contain
// antaeus.wasm and wasm_exec.js.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const root = new URL('../', import.meta.url);
const dist = process.argv[2]
  ? pathToFileURL(resolve(process.argv[2]) + '/')
  : new URL('.tmp/dist/antaeus_js_wasm/', root);
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

// One huge chunk is not copied into the engine: reads copy only what the
// adapter asks for, so its response limit bounds Go allocations.
const huge = new Uint8Array(32 << 20).fill(0x20);
let hugeCancelled = false;
const memoryBefore = instance.exports.mem.buffer.byteLength;
r = await evaluate([() => Promise.resolve(new Response(new ReadableStream({
  start(controller) { controller.enqueue(huge); },
  cancel() { hugeCancelled = true; },
}), { status: 200 }))]);
assert.equal(r.decision.outcome, 'failure');
assert(hugeCancelled, 'oversized single-chunk stream was not cancelled');
const growth = instance.exports.mem.buffer.byteLength - memoryBefore;
assert(growth < (8 << 20), `engine memory grew by ${growth} bytes for a 32 MiB chunk`);

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

// No host value stops the engine: odd rejections, synchronous throws, errored
// streams, and non-response values are provider failures, and later calls work.
const erroring = (reason) => () => Promise.resolve(new Response(new ReadableStream({ start(c) { c.error(reason); } }), { status: 200 }));
for (const bad of [
  () => Promise.reject(undefined),
  () => Promise.reject(null),
  () => { throw new Error('synchronous'); },
  erroring(undefined),
  erroring(null),
  () => Promise.resolve(42),
]) {
  r = await evaluate([bad, bad]);
  assert.equal(r.decision.outcome, 'failure');
}
r = await evaluate([reply(answers(0.96, 0.2))]);
assert.equal(r.decision.outcome, 'deny');

// Calls run one at a time in arrival order, never interleaved.
let inFlight = 0;
let maxInFlight = 0;
const order = [];
const slow = (id) => async (init) => {
  inFlight += 1;
  maxInFlight = Math.max(maxInFlight, inFlight);
  order.push(JSON.parse(new TextDecoder().decode(init.body)).state.title);
  await new Promise((done) => setTimeout(done, 30));
  inFlight -= 1;
  return new Response(answers(0.05, 0.9), { status: 200 });
};
calls = [];
script = Array.from({ length: 5 }, (_, i) => slow(i));
const titles = ['a', 'b', 'c', 'd', 'e'];
const results = await Promise.all(titles.map((title) => engine.evaluate(request({ input: { title } }))));
assert(results.every((raw) => JSON.parse(raw).decision.outcome === 'allow'));
assert.equal(maxInFlight, 1, 'evaluations overlapped');
assert.deepEqual(order, titles);

// Credentials for slots off the profile's route are ignored, even if invalid.
r = await evaluate([reply(answers(0.96, 0.2))], {
  credentials: [
    { adapterId: 'io.antaeus.systemone', slot: 'drex-api-key', value: key },
    { adapterId: 'Not Valid', slot: 'BAD SLOT', value: 'x' },
  ],
});
assert.equal(r.decision.outcome, 'deny');

// An oversized request is rejected before it is copied into the engine.
const oversized = JSON.parse(await engine.evaluate('x'.repeat((4 << 20) + 1)));
assert.equal(oversized.error.code, 'host.request_invalid');

// Malformed envelopes are rejected without a provider call.
calls = [];
for (const raw of [request() + '}', request() + ']', request() + '{}', request().replace('"policyFormat":"yaml"', '"policyFormat":"yaml","policyFormat":"yaml"')]) {
  assert.equal(JSON.parse(await engine.evaluate(raw)).error.code, 'host.request_invalid');
}
assert.equal(calls.length, 0);

// Configuration problems are typed errors and make no request.
r = await evaluate([], { credentials: [] });
assert.equal(r.error.code, 'host.credential_missing');
r = await evaluate([], { deadlineUnixMs: Date.now() - 1000 });
assert.equal(r.error.code, 'host.deadline_exceeded');
assert.equal(calls.length, 0);

// Parity: the shared cases produce the same Decisions as the native engine
// (internal/host/parity_test.go), apart from the profile digest, which covers
// each harness's endpoint, and measured latency.
const parity = JSON.parse(read('internal/host/testdata/parity/cases.json'));
const expected = JSON.parse(read('internal/host/testdata/parity/expected.json'));
const normalize = (decision) => {
  delete decision.evaluator?.profileDigest;
  for (const attempt of decision.extensions?.['io.antaeus.execution']?.attempts ?? []) delete attempt.latencyMs;
  return decision;
};
for (const c of parity.cases) {
  calls = [];
  script = [() => Promise.resolve(c.status === 200
    ? new Response(JSON.stringify({ model: 'parity-model', answers: Object.fromEntries(Object.entries(c.answers).map(([id, p]) => [id, { type: 'noul', noul: p }])) }), { status: 200 })
    : new Response('', { status: c.status }))];
  const raw = JSON.parse(await engine.evaluate(JSON.stringify({
    policy: parity.policy, policyFormat: 'yaml',
    profile: JSON.stringify(parity.profile).replace('ENDPOINT', 'https://parity.invalid'), profileFormat: 'json',
    input: parity.input, correlationId: parity.correlationId, credentials: [],
  })));
  assert.equal(raw.ok, true, `${c.name}: ${JSON.stringify(raw.error)}`);
  assert.deepStrictEqual(normalize(raw.decision), expected[c.name], `parity: ${c.name}`);
}
assert.equal(Object.keys(expected).length, parity.cases.length);

console.log('check-wasm: WebAssembly engine interface checks passed');
process.exit(0);
