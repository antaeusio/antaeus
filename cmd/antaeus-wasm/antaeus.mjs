// Starts the Antaeus WebAssembly engine and exposes its interface. Load Go's
// wasm_exec.js first, which defines globalThis.Go. See docs/webassembly.md.
//
// Every call runs in a fresh engine instance, entered only from its own
// caller's context, and the instance is discarded afterwards. A Go runtime
// keeps one pending timer and re-registers it in whichever JavaScript context
// enters the runtime; in Cloudflare Workers, work and timers registered in a
// request's context are cancelled when that request ends. A runtime shared
// between requests, or handed from one request to another, can therefore lose
// its deadline timer and I/O and hang. Instances share the compiled module, so
// starting one costs little. Concurrent instances are capped; a call that
// finds the cap reached waits by polling with its own timers, and a call that
// stops waiting leaves nothing behind. In Cloudflare Workers, call start()
// inside a request handler, not at module scope.

const POLL_MS = 10;
let scratch;

function failure(code, message) {
  return JSON.stringify({ ok: false, error: { code, message } });
}

function utf8Size(text, limit) {
  if (text.length > limit) return -1;
  scratch ??= new Uint8Array(limit + 1);
  const { read, written } = new TextEncoder().encodeInto(text, scratch);
  return read < text.length || written > limit ? -1 : written;
}

function deadlineOf(text) {
  try {
    const value = JSON.parse(text).deadlineUnixMs;
    return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Compiles the engine from a WebAssembly.Module (as Workers provide) or from
 * the module's bytes, and resolves to its interface:
 * { interfaceVersion, version, validate(request), evaluate(request) }.
 *
 * options.maxInstances caps concurrent calls, and therefore engine memory, per
 * JavaScript isolate (default 2). A call that finds the cap reached waits
 * until its request's deadlineUnixMs, or options.maxWaitMs (default 10 s)
 * without one; at most maxPendingCalls calls totalling maxPendingBytes may
 * wait, and further calls return host.busy at once.
 */
export async function start(module, { maxInstances = 2, maxWaitMs = 10_000 } = {}) {
  if (!Number.isInteger(maxInstances) || maxInstances < 1) throw new RangeError('maxInstances must be a positive integer');
  const compiled = module instanceof WebAssembly.Module ? module : await WebAssembly.compile(module);
  let running = 0;
  let waitingCalls = 0;
  let waitingBytes = 0;

  // An instance's run promise settles only if the instance stops. Both
  // outcomes are handled at once, including for an instance that failed to
  // start, so a stopped instance never leaves an unhandled rejection.
  const stopped = () => failure('host.internal_error', 'the engine stopped during this call');

  async function instantiate() {
    const go = new globalThis.Go();
    const instance = await WebAssembly.instantiate(compiled, go.importObject);
    const exit = Promise.resolve(go.run(instance)).then(stopped, stopped);
    const core = globalThis.__antaeusCore;
    delete globalThis.__antaeusCore;
    if (!core) throw new Error('the Antaeus engine did not start');
    return { core, exit };
  }

  // A probe instance supplies the interface constants and is then discarded
  // without serving a call: it was started in the context that called
  // start(), and an instance is entered only from its own caller's context.
  const probe = (await instantiate()).core;
  const limits = {
    interfaceVersion: probe.interfaceVersion,
    version: probe.version,
    maxRequestBytes: probe.maxRequestBytes,
    maxPendingCalls: probe.maxPendingCalls,
    maxPendingBytes: probe.maxPendingBytes,
  };

  async function claim() {
    running += 1;
    try {
      return await instantiate();
    } catch {
      running -= 1;
      return undefined;
    }
  }

  // run resolves to the call's response; a synchronous throw or a rejection
  // becomes an internal error.
  async function run(instance, kind, text) {
    try {
      return await instance.core.run(kind, text);
    } catch {
      return failure('host.internal_error', 'the engine failed while handling this call');
    }
  }

  async function acquire(size, waitUntil) {
    if (running < maxInstances) return claim();
    if (waitingCalls >= limits.maxPendingCalls || waitingBytes + size > limits.maxPendingBytes) return 'busy';
    waitingCalls += 1;
    waitingBytes += size;
    try {
      while (Date.now() < waitUntil) {
        await new Promise((done) => setTimeout(done, Math.min(POLL_MS, Math.max(0, waitUntil - Date.now()))));
        if (running < maxInstances) return claim();
      }
      return 'expired';
    } finally {
      waitingCalls -= 1;
      waitingBytes -= size;
    }
  }

  async function submit(kind, text) {
    if (typeof text !== 'string') throw new TypeError('expected one JSON string argument');
    const size = utf8Size(text, limits.maxRequestBytes);
    if (size < 0) return failure('host.request_invalid', `request exceeds ${limits.maxRequestBytes} bytes`);
    const deadline = kind === 'evaluate' ? deadlineOf(text) : undefined;
    const instance = await acquire(size, deadline ?? Date.now() + maxWaitMs);
    if (instance === 'busy') return failure('host.busy', 'the engine has too many pending calls; retry later');
    if (instance === 'expired') {
      return deadline === undefined
        ? failure('host.busy', 'no engine instance became free in time; retry later')
        : failure('host.deadline_exceeded', 'the deadline passed before evaluation started');
    }
    if (instance === undefined) return failure('host.internal_error', 'the engine could not start an instance');
    try {
      return await Promise.race([run(instance, kind, text), instance.exit]);
    } finally {
      running -= 1;
    }
  }

  return Object.freeze({
    interfaceVersion: limits.interfaceVersion,
    version: limits.version,
    validate: (request) => submit('validate', request),
    evaluate: (request) => submit('evaluate', request),
  });
}
