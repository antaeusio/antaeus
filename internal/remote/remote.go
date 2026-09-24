// Package remote holds the transport boundary shared by remote evaluator
// adapters: an isolated HTTP client and typed classification of transport
// failures. It never reads endpoints, proxies, or credentials from the
// environment.
package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
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

// TransportFailure classifies a failed round trip. Deadlines and connection
// failures are retryable; cancellation and TLS verification failures are not.
// The TLS code is prefixed with the adapter's code namespace.
func TransportFailure(ctx context.Context, err error, namespace string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return &evaluator.Error{Code: "evaluator.timeout", Retryable: true, Message: "provider request exceeded its deadline"}
		}
		return &evaluator.Error{Code: "evaluation.cancelled", Retryable: false, Message: "provider request was cancelled"}
	}
	var certificate *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	if errors.As(err, &certificate) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &record) {
		return &evaluator.Error{Code: namespace + ".tls_failed", Retryable: false, Message: "provider TLS verification failed"}
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return &evaluator.Error{Code: "evaluator.timeout", Retryable: true, Message: "provider connection timed out"}
	}
	return &evaluator.Error{Code: "evaluator.unavailable", Retryable: true, Message: "provider connection failed"}
}

// ReadBounded reads at most limit bytes of a response body. It reports whether
// the body was larger than the limit.
func ReadBounded(body io.Reader, limit int64) ([]byte, bool, error) {
	payload, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) > limit {
		return nil, true, nil
	}
	return payload, false, nil
}
