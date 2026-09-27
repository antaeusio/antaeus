//go:build js && wasm

package remote

import (
	"errors"
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
		result <- settled{value: args[0]}
		release()
		return nil
	})
	onReject = js.FuncOf(func(_ js.Value, args []js.Value) any {
		result <- settled{err: errors.New("fetch failed: " + args[0].Call("toString").String())}
		release()
		return nil
	})
	promise.Call("then", onResolve, onReject)
	select {
	case r := <-result:
		return r.value, r.err, true
	case <-done:
		return js.Undefined(), nil, false
	}
}

func (fetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
	if req.Body != nil && req.Body != http.NoBody {
		body, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
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
	var body io.ReadCloser = http.NoBody
	if stream := response.Get("body"); stream.Truthy() {
		body = &streamBody{reader: stream.Call("getReader"), controller: controller, done: ctx.Done(), ctxErr: ctx.Err}
	}
	status := response.Get("status").Int()
	return &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          body,
		ContentLength: contentLength,
		Request:       req,
	}, nil
}

// streamBody reads the response incrementally, so a caller's size limit also
// bounds memory: a body larger than the limit is never fully buffered.
type streamBody struct {
	reader     js.Value
	controller js.Value
	done       <-chan struct{}
	ctxErr     func() error
	pending    []byte
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
	if len(b.pending) == 0 {
		if b.err != nil {
			return 0, b.err
		}
		chunk, err, ok := await(b.reader.Call("read"), b.done)
		switch {
		case !ok:
			b.controller.Call("abort")
			b.err = b.ctxErr()
			return 0, b.err
		case err != nil:
			b.err = err
			return 0, err
		case chunk.Get("done").Bool():
			b.err = io.EOF
			return 0, io.EOF
		}
		value := chunk.Get("value")
		b.pending = make([]byte, value.Get("byteLength").Int())
		js.CopyBytesToGo(b.pending, value)
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

func (b *streamBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		// Stop any unread transfer. A rejected cancel is handled so the host
		// never reports an unhandled promise rejection.
		b.reader.Call("cancel").Call("catch", ignore)
	}
	return nil
}
