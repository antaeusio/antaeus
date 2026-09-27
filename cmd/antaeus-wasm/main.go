//go:build js && wasm

// Command antaeus-wasm is the engine for JavaScript hosts, such as Cloudflare
// Workers, browsers, and Node.js. Started with Go's wasm_exec.js, it installs
// globalThis.antaeus with the interface described in docs/webassembly.md and
// keeps running until the host discards it.
package main

import (
	"context"
	"syscall/js"

	"github.com/antaeusio/antaeus/internal/buildinfo"
	"github.com/antaeusio/antaeus/internal/host"
)

// call runs work on its own goroutine and returns a promise of its JSON
// result, so the host's event loop keeps running while the engine waits on
// fetch. Responses always resolve; configuration problems are typed errors in
// the response, not rejections.
func call(args []js.Value, work func([]byte) []byte) any {
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return js.Global().Get("Promise").Call("reject", js.Global().Get("TypeError").New("expected one JSON string argument"))
	}
	request := []byte(args[0].String())
	var executor js.Func
	executor = js.FuncOf(func(_ js.Value, callbacks []js.Value) any {
		resolve := callbacks[0]
		go func() {
			resolve.Invoke(string(work(request)))
		}()
		executor.Release()
		return nil
	})
	return js.Global().Get("Promise").New(executor)
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
