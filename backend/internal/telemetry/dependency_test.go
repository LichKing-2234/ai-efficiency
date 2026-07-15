package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDependencyTelemetryForwardsRequestIDAndUsesFixedLabels(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	var forwarded *http.Request
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		forwarded = request
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("private downstream response")),
			Request:    request,
		}, nil
	}}
	ctx := WithRequestID(context.Background(), "request-alpha")
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api-user:api-password@relay.example.com/users/7?email=alice@example.com",
		strings.NewReader("private request body"),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer private-credential")

	response, err := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	response.Body.Close()

	if forwarded == nil {
		t.Fatal("wrapped transport did not receive a request")
	}
	if got := forwarded.Header.Get(HeaderRequestID); got != "request-alpha" {
		t.Fatalf("forwarded %s = %q, want %q", HeaderRequestID, got, "request-alpha")
	}
	if got := request.Header.Get(HeaderRequestID); got != "" {
		t.Fatalf("original request %s = %q, want unchanged", HeaderRequestID, got)
	}
	if forwarded.Context() != ctx {
		t.Fatal("forwarded request detached from the incoming context")
	}

	entry := requireSingleDependencyEvent(t, observed)
	fields := entry.ContextMap()
	if len(fields) != 8 {
		t.Fatalf("fields = %#v, want exactly 8 dependency fields", fields)
	}
	assertDependencyField(t, fields, "event", "dependency_request")
	assertDependencyField(t, fields, "dependency", "relay")
	assertDependencyField(t, fields, "operation", "http_request")
	assertDependencyField(t, fields, "method", http.MethodGet)
	assertDependencyField(t, fields, "status_class", "2xx")
	assertDependencyField(t, fields, "release", "test-release")
	assertDependencyField(t, fields, "request_id", "request-alpha")
	assertNonNegativeDependencyDuration(t, fields)
	assertDependencyPrivacy(t, entry, fields,
		"relay.example.com",
		"/users/7",
		"alice@example.com",
		"private request body",
		"private downstream response",
		"api-user",
		"api-password",
		"private-credential",
	)
}

func TestDependencyTelemetryClassifiesErrorsWithoutSensitiveDetails(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantClass string
	}{
		{name: "timeout", err: dependencyTimeoutError{}, wantClass: "timeout"},
		{name: "transport", err: errors.New("private response from bob@example.org"), wantClass: "transport_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, observed := observer.New(zap.InfoLevel)
			next := &fakeDependencyTransport{roundTrip: func(*http.Request) (*http.Response, error) {
				return nil, tt.err
			}}
			request, err := http.NewRequestWithContext(
				WithRequestID(context.Background(), "request-beta"),
				http.MethodPost,
				"https://relay.example.com/private?email=bob@example.org",
				strings.NewReader("private body"),
			)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}

			_, gotErr := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
			if !errors.Is(gotErr, tt.err) {
				t.Fatalf("RoundTrip() error = %v, want %v", gotErr, tt.err)
			}

			entry := requireSingleDependencyEvent(t, observed)
			fields := entry.ContextMap()
			assertDependencyField(t, fields, "status_class", "error")
			assertDependencyField(t, fields, "error_class", tt.wantClass)
			assertDependencyField(t, fields, "request_id", "request-beta")
			assertNonNegativeDependencyDuration(t, fields)
			assertDependencyPrivacy(t, entry, fields,
				"relay.example.com",
				"/private",
				"bob@example.org",
				"private body",
				"private response",
			)
		})
	}
}

func TestDependencyTelemetryDoesNotFabricateRequestID(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get(HeaderRequestID); got != "" {
			t.Fatalf("forwarded %s = %q, want empty", HeaderRequestID, got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://relay.example.com/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	response.Body.Close()

	fields := requireSingleDependencyEvent(t, observed).ContextMap()
	if requestID, ok := fields["request_id"]; ok {
		t.Fatalf("request_id field = %v, want omitted without correlation context", requestID)
	}
}

func TestDependencyTelemetryPreservesCancellation(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	started := make(chan struct{})
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	}}
	ctx, cancel := context.WithCancel(WithRequestID(context.Background(), "request-cancel"))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://relay.example.com/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, roundTripErr := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
		done <- roundTripErr
	}()
	<-started
	cancel()

	select {
	case gotErr := <-done:
		if !errors.Is(gotErr, context.Canceled) {
			t.Fatalf("RoundTrip() error = %v, want context canceled", gotErr)
		}
	case <-time.After(time.Second):
		t.Fatal("wrapped transport did not preserve request cancellation")
	}

	fields := requireSingleDependencyEvent(t, observed).ContextMap()
	assertDependencyField(t, fields, "error_class", "canceled")
}

func TestDependencyTelemetryDelegatesCloseIdleConnections(t *testing.T) {
	next := &fakeDependencyTransport{}
	wrapped := WrapDependency(zap.NewNop(), "test-release", "relay", "http_request")(next)
	closer, ok := wrapped.(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatalf("wrapped transport type = %T, want CloseIdleConnections capability", wrapped)
	}
	closer.CloseIdleConnections()
	if !next.closedIdleConnections {
		t.Fatal("CloseIdleConnections() was not delegated to the Relay transport")
	}
}

type fakeDependencyTransport struct {
	roundTrip             func(*http.Request) (*http.Response, error)
	closedIdleConnections bool
}

func (t *fakeDependencyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.roundTrip(request)
}

func (t *fakeDependencyTransport) CloseIdleConnections() {
	t.closedIdleConnections = true
}

type dependencyTimeoutError struct{}

func (dependencyTimeoutError) Error() string   { return "private timeout detail" }
func (dependencyTimeoutError) Timeout() bool   { return true }
func (dependencyTimeoutError) Temporary() bool { return true }

func requireSingleDependencyEvent(t *testing.T, observed *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("dependency events = %d, want 1", len(entries))
	}
	if entries[0].Message != "dependency_request" {
		t.Fatalf("event message = %q, want %q", entries[0].Message, "dependency_request")
	}
	return entries[0]
}

func assertDependencyField(t *testing.T, fields map[string]interface{}, key, want string) {
	t.Helper()
	if got, ok := fields[key].(string); !ok || got != want {
		t.Fatalf("field %s = %#v, want %q", key, fields[key], want)
	}
}

func assertNonNegativeDependencyDuration(t *testing.T, fields map[string]interface{}) {
	t.Helper()
	duration, ok := fields["duration_ms"].(int64)
	if !ok {
		t.Fatalf("duration_ms = %#v, want int64", fields["duration_ms"])
	}
	if duration < 0 {
		t.Fatalf("duration_ms = %d, want non-negative", duration)
	}
}

func assertDependencyPrivacy(t *testing.T, entry observer.LoggedEntry, fields map[string]interface{}, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(entry.Message, secret) {
			t.Fatalf("event message contains sensitive value %q", secret)
		}
		for key, value := range fields {
			text, ok := value.(string)
			if ok && strings.Contains(text, secret) {
				t.Fatalf("field %s contains sensitive value %q", key, secret)
			}
		}
	}
}
