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

func TestDependencyTelemetryWaitsForBodyCompletionAndEmitsOnce(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("complete body")),
			Request:    request,
		}, nil
	}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://relay.example.com/data", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := observed.Len(); got != 0 {
		t.Fatalf("dependency events after headers = %d, want 0 before body completion", got)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := observed.Len(); got != 1 {
		t.Fatalf("dependency events after EOF = %d, want 1", got)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := observed.Len(); got != 1 {
		t.Fatalf("dependency events after EOF and Close = %d, want exactly 1", got)
	}
}

func TestDependencyTelemetryCloseWithoutEOFEmitsOnce(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("body is deliberately not read")),
			Request:    request,
		}, nil
	}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://relay.example.com/data", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := observed.Len(); got != 0 {
		t.Fatalf("dependency events after headers = %d, want 0 before Close", got)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := observed.Len(); got != 1 {
		t.Fatalf("dependency events after repeated Close = %d, want exactly 1", got)
	}
}

func TestDependencyTelemetryClassifiesWithheldBodyTimeout(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          &contextBlockingBody{ctx: request.Context()},
			ContentLength: 1,
			Request:       request,
		}, nil
	}}
	client := &http.Client{
		Transport: WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next),
		Timeout:   40 * time.Millisecond,
	}
	request, err := http.NewRequestWithContext(
		WithRequestID(context.Background(), "request-body-timeout"),
		http.MethodGet,
		"https://relay.example.com/private?email=alice@example.com",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() before body read error = %v", err)
	}
	if got := observed.Len(); got != 0 {
		t.Fatalf("dependency events after headers = %d, want 0 before body timeout", got)
	}
	_, readErr := io.ReadAll(response.Body)
	if readErr == nil {
		t.Fatal("ReadAll() error = nil, want body timeout")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	entry := requireSingleDependencyEvent(t, observed)
	fields := entry.ContextMap()
	assertDependencyField(t, fields, "status_class", "error")
	assertDependencyField(t, fields, "error_class", "timeout")
	assertDependencyField(t, fields, "request_id", "request-body-timeout")
	assertDependencyPrivacy(t, entry, fields, "relay.example.com", "alice@example.com")
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

func TestDependencyTelemetryNormalizesUnknownMethod(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}}
	method := strings.Repeat("CUSTOM", 64)
	request, err := http.NewRequestWithContext(context.Background(), method, "https://relay.example.com/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := WrapDependency(zap.New(core), "test-release", "relay", "http_request")(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	response.Body.Close()

	fields := requireSingleDependencyEvent(t, observed).ContextMap()
	assertDependencyField(t, fields, "method", "OTHER")
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

func TestDependencyTelemetryReportsOneNormalizedMetricAfterBodyCompletion(t *testing.T) {
	metrics := &recordingDependencyObserver{}
	next := &fakeDependencyTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    request,
		}, nil
	}}
	request, err := http.NewRequest(http.MethodGet, "https://relay.example.com/private?email=alice@example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	response, err := WrapDependency(zap.NewNop(), "test-release", "relay", "http_request", metrics)(next).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if len(metrics.observations) != 0 {
		t.Fatalf("observations after headers = %#v, want none before body completion", metrics.observations)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if len(metrics.observations) != 1 {
		t.Fatalf("observations = %#v, want one", metrics.observations)
	}
	got := metrics.observations[0]
	if got.dependency != "relay" || got.operation != "http_request" || got.method != "GET" || got.statusClass != "2xx" {
		t.Fatalf("observation = %#v, want fixed dependency fields", got)
	}
	if got.duration < 0 {
		t.Fatalf("duration = %s, want non-negative", got.duration)
	}
}

type recordingDependencyObserver struct {
	observations []dependencyMetricObservation
}

type dependencyMetricObservation struct {
	dependency  string
	operation   string
	method      string
	statusClass string
	duration    time.Duration
}

func (o *recordingDependencyObserver) Observe(dependency, operation, method, statusClass string, duration time.Duration) {
	o.observations = append(o.observations, dependencyMetricObservation{
		dependency:  dependency,
		operation:   operation,
		method:      method,
		statusClass: statusClass,
		duration:    duration,
	})
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

type contextBlockingBody struct {
	ctx context.Context
}

func (b *contextBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (*contextBlockingBody) Close() error { return nil }

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
