//go:build !(js && wasm)

package remote

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// NewClient builds an isolated client. It ignores proxy environment variables
// and any process-wide http.DefaultTransport customization, requires TLS 1.2 or
// newer with certificate verification, and never follows redirects, so
// authorization is never forwarded to another location.
func NewClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          4,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
