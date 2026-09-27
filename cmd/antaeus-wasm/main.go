//go:build js && wasm

// Command antaeus-wasm is the engine for JavaScript hosts, such as Cloudflare
// Workers, browsers, and Node.js. Hosts start it through antaeus.mjs, which
// runs every call in a fresh instance of this program, entered only from the
// calling request's own context; see docs/webassembly.md.
//
// This core serves one call at a time and refuses overlapping calls. A Go
// runtime keeps one pending timer and re-registers it, with any work it
// resumes, in whichever JavaScript context enters it, so a runtime shared
// between requests can lose its timers and I/O when another request ends.
package main

import (
	"context"
	"sync"
	"syscall/js"

	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/internal/buildinfo"
	"github.com/antaeusio/antaeus/internal/host"
)

var (
	registry runner.Registry
	mu       sync.Mutex
	running  bool
)

// run starts one call and returns a promise of its JSON response, which
// always resolves.
func run(args []js.Value) (result any) {
	promise := js.Global().Get("Promise")
	resolved := func(response []byte) js.Value { return promise.Call("resolve", string(response)) }
	defer func() {
		if r := recover(); r != nil {
			result = resolved(host.Failure(host.CodeInternalError, "the engine failed while accepting this call"))
		}
	}()
	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeString {
		return promise.Call("reject", js.Global().Get("TypeError").New("expected a call kind and one JSON string"))
	}
	var work func([]byte) []byte
	switch args[0].String() {
	case "validate":
		work = host.Validate
	case "evaluate":
		work = func(request []byte) []byte {
			return host.Evaluate(context.Background(), request, registry)
		}
	default:
		return promise.Call("reject", js.Global().Get("TypeError").New("unknown call kind"))
	}
	// antaeus.mjs checks the size before calling; checking again keeps direct
	// callers from copying an oversized request into Go memory.
	if !withinLimit(args[1]) {
		return resolved(host.Failure(host.CodeRequestInvalid, "request exceeds the size limit"))
	}
	mu.Lock()
	if running {
		mu.Unlock()
		return resolved(host.Failure(host.CodeBusy, "another call is running"))
	}
	running = true
	mu.Unlock()
	request := []byte(args[1].String())

	var executor js.Func
	executor = js.FuncOf(func(_ js.Value, callbacks []js.Value) any {
		executor.Release()
		resolve := callbacks[0]
		go func() {
			response := safely(request, work)
			mu.Lock()
			running = false
			mu.Unlock()
			resolve.Invoke(string(response))
		}()
		return nil
	})
	return promise.New(executor)
}

var scratch js.Value

// withinLimit reports whether text is at most host.MaxRequestBytes of UTF-8,
// measured in JavaScript without copying it into Go memory.
func withinLimit(text js.Value) bool {
	length := js.Global().Get("Object").Invoke(text).Get("length").Int()
	if length > host.MaxRequestBytes {
		return false
	}
	if scratch.IsUndefined() {
		scratch = js.Global().Get("Uint8Array").New(host.MaxRequestBytes + 1)
	}
	result := js.Global().Get("TextEncoder").New().Call("encodeInto", text, scratch)
	return result.Get("read").Int() == length && result.Get("written").Int() <= host.MaxRequestBytes
}

// safely runs work and converts a panic into an internal error response, so
// the engine keeps serving later calls.
func safely(request []byte, work func([]byte) []byte) (response []byte) {
	defer func() {
		if r := recover(); r != nil {
			response = host.Failure(host.CodeInternalError, "the engine failed while handling this call")
		}
	}()
	return work(request)
}

func main() {
	registry = host.Registry()
	core := js.Global().Get("Object").New()
	core.Set("interfaceVersion", host.InterfaceVersion)
	core.Set("version", buildinfo.Current().String())
	core.Set("maxRequestBytes", host.MaxRequestBytes)
	core.Set("maxPendingCalls", host.MaxPendingCalls)
	core.Set("maxPendingBytes", host.MaxPendingBytes)
	core.Set("run", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return run(args)
	}))
	js.Global().Set("__antaeusCore", core)
	select {}
}
