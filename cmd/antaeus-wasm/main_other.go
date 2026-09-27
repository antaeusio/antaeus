//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "antaeus-wasm runs only as WebAssembly; build it with scripts/build-wasm")
	os.Exit(2)
}
