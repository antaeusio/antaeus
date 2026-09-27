//go:build js && wasm

package remote

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"syscall/js"
)

// NewClient builds a client whose requests go through the host's fetch, such
// as a Cloudflare Worker's or a browser's. Go's own js/wasm transport would
// dial sockets instead whenever a dialer is configured, which a Worker cannot
// do. The host verifies TLS. Redirects are never followed, so authorization is
// never forwarded to another location.
func NewClient() *http.Client {
	return &http.Client{
		Transport:     fetchTransport{},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type fetchTransport struct{}

// ignore is a permanent no-op callback for promises whose outcome is unused.
var ignore = js.FuncOf(func(js.Value, []js.Value) any { return nil })

// guard runs JavaScript interop and converts a panic, such as a host function
// that throws or returns an unexpected value, into an error, so no host value
// can stop the engine.
func guard(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("fetch failed: host returned an unexpected value: %v", r)
		}
	}()
	return f()
}

var errFetchRejected = errors.New("fetch failed: the host rejected the request or response stream")

type settled struct {
	value js.Value
	err   error
}

// await waits for a JavaScript promise or for done. Both callbacks stay
// registered until the promise settles, even after done, because releasing a
// callback that JavaScript later invokes would crash the program. Callers abort
// the underlying operation on done so the promise settles promptly.
func await(promise js.Value, done <-chan struct{}) (js.Value, error, bool) {
	result := make(chan settled, 1)
	var onResolve, onReject js.Func
	release := func() {
		onResolve.Release()
		onReject.Release()
	}
	onResolve = js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := js.Undefined()
		if len(args) > 0 {
			value = args[0]
		}
		result <- settled{value: value}
		release()
		return nil
	})
	// The rejection reason can be any value, including undefined, and may
	// carry host details; a fixed message is enough to classify the failure.
	onReject = js.FuncOf(func(js.Value, []js.Value) any {
		result <- settled{err: errFetchRejected}
		release()
		return nil
	})
	if err := guard(func() error {
		promise.Call("then", onResolve, onReject)
		return nil
	}); err != nil {
		release()
		return js.Undefined(), err, true
	}
	select {
	case r := <-result:
		return r.value, r.err, true
	case <-done:
		return js.Undefined(), nil, false
	}
}

func (fetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	var response *http.Response
	err := guard(func() error {
		var err error
		response, err = roundTrip(req, body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func roundTrip(req *http.Request, body []byte) (*http.Response, error) {
	global := js.Global()
	fetch := global.Get("fetch")
	if fetch.Type() != js.TypeFunction {
		return nil, errors.New("the host provides no fetch function")
	}
	ctx := req.Context()
	controller := global.Get("AbortController").New()
	init := global.Get("Object").New()
	init.Set("method", req.Method)
	init.Set("redirect", "manual")
	init.Set("signal", controller.Get("signal"))
	headers := global.Get("Headers").New()
	for name, values := range req.Header {
		for _, value := range values {
			headers.Call("append", name, value)
		}
	}
	init.Set("headers", headers)
	if body != nil {
		array := global.Get("Uint8Array").New(len(body))
		js.CopyBytesToJS(array, body)
		init.Set("body", array)
	}

	response, err, ok := await(fetch.Invoke(req.URL.String(), init), ctx.Done())
	if !ok {
		controller.Call("abort")
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if response.Type() != js.TypeObject {
		return nil, errors.New("fetch failed: the host resolved fetch without a response")
	}

	header := http.Header{}
	entries := response.Get("headers").Call("entries")
	for {
		next := entries.Call("next")
		if next.Get("done").Bool() {
			break
		}
		pair := next.Get("value")
		header.Add(pair.Index(0).String(), pair.Index(1).String())
	}
	contentLength := int64(-1)
	if value, err := strconv.ParseInt(header.Get("Content-Length"), 10, 64); err == nil && value >= 0 {
		contentLength = value
	}
	var reader io.ReadCloser = http.NoBody
	if stream := response.Get("body"); stream.Truthy() {
		reader = &streamBody{reader: stream.Call("getReader"), controller: controller, done: ctx.Done(), ctxErr: ctx.Err}
	}
	status := response.Get("status").Int()
	return &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          reader,
		ContentLength: contentLength,
		Request:       req,
	}, nil
}

// streamBody reads the response incrementally and copies into Go memory only
// the bytes each Read asks for, so a caller's size limit also bounds the
// engine's allocations, however large the host's chunks are. Memory the host's
// fetch implementation allocates for a chunk is outside the engine's control.
type streamBody struct {
	reader     js.Value
	controller js.Value
	done       <-chan struct{}
	ctxErr     func() error
	chunk      js.Value // the current Uint8Array chunk, or undefined
	offset     int      // bytes of chunk already read
	length     int      // byte length of chunk
	mu         sync.Mutex
	closed     bool
	err        error
}

func (b *streamBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, errors.New("read on closed response body")
	}
	if len(p) == 0 {
		return 0, nil
	}
	for b.offset >= b.length {
		if b.err != nil {
			return 0, b.err
		}
		if err := guard(b.next); err != nil {
			b.err = err
			return 0, err
		}
	}
	n := min(len(p), b.length-b.offset)
	var copied int
	if err := guard(func() error {
		copied = js.CopyBytesToGo(p[:n], b.chunk.Call("subarray", b.offset, b.offset+n))
		return nil
	}); err != nil {
		b.err = err
		return 0, err
	}
	b.offset += copied
	return copied, nil
}

// next makes the next chunk current, or sets err at the end of the stream.
func (b *streamBody) next() error {
	chunk, err, ok := await(b.reader.Call("read"), b.done)
	switch {
	case !ok:
		b.controller.Call("abort")
		b.err = b.ctxErr()
		return nil
	case err != nil:
		return err
	case chunk.Type() != js.TypeObject:
		return errors.New("fetch failed: the host stream returned an unexpected chunk")
	case chunk.Get("done").Bool():
		b.err = io.EOF
		return nil
	}
	value := chunk.Get("value")
	if !value.InstanceOf(js.Global().Get("Uint8Array")) {
		return errors.New("fetch failed: the host stream returned an unexpected chunk")
	}
	b.chunk, b.offset, b.length = value, 0, value.Get("byteLength").Int()
	return nil
}

func (b *streamBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		// Stop any unread transfer. A rejected cancel is handled so the host
		// never reports an unhandled promise rejection.
		_ = guard(func() error {
			b.reader.Call("cancel").Call("catch", ignore)
			return nil
		})
	}
	return nil
}
