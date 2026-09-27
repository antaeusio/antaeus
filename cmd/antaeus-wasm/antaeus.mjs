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

  async function instantiate() {
    const go = new globalThis.Go();
    const instance = await WebAssembly.instantiate(compiled, go.importObject);
    const exit = go.run(instance);
    const core = globalThis.__antaeusCore;
    delete globalThis.__antaeusCore;
    if (!core) throw new Error('the Antaeus engine did not start');
    return { core, exit };
  }

  // The first instance supplies the interface constants and serves the first
  // call; like every instance, it is used once.
  let spare = await instantiate();
  const limits = {
    interfaceVersion: spare.core.interfaceVersion,
    version: spare.core.version,
    maxRequestBytes: spare.core.maxRequestBytes,
    maxPendingCalls: spare.core.maxPendingCalls,
    maxPendingBytes: spare.core.maxPendingBytes,
  };

  async function claim() {
    running += 1;
    if (spare) {
      const instance = spare;
      spare = undefined;
      return instance;
    }
    try {
      return await instantiate();
    } catch {
      running -= 1;
      return undefined;
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
      return await Promise.race([
        instance.core.run(kind, text),
        instance.exit.then(() => failure('host.internal_error', 'the engine exited')),
      ]);
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
