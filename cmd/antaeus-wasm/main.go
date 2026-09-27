//go:build js && wasm

// Command antaeus-wasm is the engine for JavaScript hosts, such as Cloudflare
// Workers, browsers, and Node.js. Started with Go's wasm_exec.js, it installs
// globalThis.antaeus with the interface described in docs/webassembly.md and
// keeps running until the host discards it.
package main

import (
	"context"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/antaeusio/antaeus/internal/buildinfo"
	"github.com/antaeusio/antaeus/internal/host"
)

// Calls run one at a time, in arrival order. All calls share one Go runtime,
// which resumes whichever goroutines are ready whenever any promise settles.
// Interleaved calls would therefore run one request's work, and start its
// fetches, inside another request's JavaScript context; Cloudflare Workers
// cancel such work when that other request ends. The queue is a chain of
// JavaScript promises created in each caller's own context, so a call enters
// Go only when every earlier call has finished.
var (
	queueMu sync.Mutex
	tail    = js.Global().Get("Promise").Call("resolve")
	settle  = js.FuncOf(func(js.Value, []js.Value) any { return nil })
)

// call validates the argument, then queues work and returns a promise of its
// JSON result. The promise always resolves: configuration problems and
// internal failures are typed errors in the response.
func call(args []js.Value, work func([]byte) []byte) (result any) {
	promise := js.Global().Get("Promise")
	defer func() {
		if r := recover(); r != nil {
			result = promise.Call("resolve", string(host.Failure(host.CodeInternalError, "the engine failed while accepting this call")))
		}
	}()
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return promise.Call("reject", js.Global().Get("TypeError").New("expected one JSON string argument"))
	}
	// A JavaScript string's UTF-16 length is a lower bound on its UTF-8 size,
	// so an oversized request is rejected before it is copied into Go memory,
	// which the engine never returns to the host.
	if js.Global().Get("Object").Invoke(args[0]).Get("length").Int() > host.MaxRequestBytes {
		return promise.Call("resolve", string(host.Failure(host.CodeRequestInvalid, fmt.Sprintf("request exceeds %d bytes", host.MaxRequestBytes))))
	}
	request := []byte(args[0].String())

	var run js.Func
	run = js.FuncOf(func(js.Value, []js.Value) any {
		run.Release()
		var executor js.Func
		executor = js.FuncOf(func(_ js.Value, callbacks []js.Value) any {
			executor.Release()
			resolve := callbacks[0]
			go func() {
				resolve.Invoke(string(safely(request, work)))
			}()
			return nil
		})
		return promise.New(executor)
	})

	queueMu.Lock()
	defer queueMu.Unlock()
	queued := tail.Call("then", run)
	tail = queued.Call("then", settle, settle)
	return queued
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
	registry := host.Registry()
	engine := js.Global().Get("Object").New()
	engine.Set("interfaceVersion", host.InterfaceVersion)
	engine.Set("version", buildinfo.Current().String())
	engine.Set("validate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return call(args, host.Validate)
	}))
	engine.Set("evaluate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return call(args, func(request []byte) []byte {
			return host.Evaluate(context.Background(), request, registry)
		})
	}))
	js.Global().Set("antaeus", engine)
	select {}
}
