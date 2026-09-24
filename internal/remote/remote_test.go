package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
)

func code(t *testing.T, err error) (string, bool) {
	t.Helper()
	var failure *evaluator.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error %v is not an evaluator.Error", err)
	}
	return failure.Code, failure.Retryable
}

func TestNewClientIgnoresAmbientStateAndRedirects(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	client := NewClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == http.DefaultTransport || transport.Proxy != nil {
		t.Fatalf("unexpected transport %#v", client.Transport)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("redirect was followed: %d", response.StatusCode)
	}
}

func TestTransportFailureClassification(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if c, retry := code(t, TransportFailure(expired, errors.New("x"), "demo")); c != "evaluator.timeout" || !retry {
		t.Fatalf("deadline: %s %v", c, retry)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if c, retry := code(t, TransportFailure(cancelled, errors.New("x"), "demo")); c != "evaluation.cancelled" || retry {
		t.Fatalf("cancelled: %s %v", c, retry)
	}
	if c, retry := code(t, TransportFailure(context.Background(), errors.New("connection refused"), "demo")); c != "evaluator.unavailable" || !retry {
		t.Fatalf("connection: %s %v", c, retry)
	}
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	_, err := NewClient().Get(server.URL)
	if c, retry := code(t, TransportFailure(context.Background(), err, "demo")); c != "demo.tls_failed" || retry {
		t.Fatalf("tls: %s %v", c, retry)
	}
}

func TestReadBounded(t *testing.T) {
	payload, tooLarge, err := ReadBounded(strings.NewReader("12345"), 5)
	if err != nil || tooLarge || string(payload) != "12345" {
		t.Fatalf("at limit: %q %v %v", payload, tooLarge, err)
	}
	payload, tooLarge, err = ReadBounded(strings.NewReader("123456"), 5)
	if err != nil || !tooLarge || payload != nil {
		t.Fatalf("over limit: %q %v %v", payload, tooLarge, err)
	}
}
