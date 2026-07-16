package telemetry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/httpclient"
	"go.uber.org/zap"
)

func WrapDependency(logger *zap.Logger, release, dependency, operation string, observers ...DependencyObserver) httpclient.TransportWrapper {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(next http.RoundTripper) http.RoundTripper {
		return &dependencyTransport{
			next:       next,
			logger:     logger,
			release:    release,
			dependency: dependency,
			operation:  operation,
			observers:  observers,
		}
	}
}

type dependencyTransport struct {
	next       http.RoundTripper
	logger     *zap.Logger
	release    string
	dependency string
	operation  string
	observers  []DependencyObserver
}

func (t *dependencyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	requestID := RequestID(request.Context())
	outbound := request
	if requestID != "" {
		outbound = request.Clone(request.Context())
		outbound.Header = request.Header.Clone()
		if outbound.Header == nil {
			outbound.Header = make(http.Header)
		}
		outbound.Header.Set(HeaderRequestID, requestID)
	}

	startedAt := time.Now()
	response, err := t.next.RoundTrip(outbound)
	if err != nil {
		t.logEvent(request.Context(), requestID, request.Method, "error", startedAt, err)
		return response, err
	}
	if response == nil {
		t.logEvent(request.Context(), requestID, request.Method, "unknown", startedAt, nil)
		return nil, nil
	}
	statusClass := HTTPStatusClass(response.StatusCode)
	if response.Body == nil {
		t.logEvent(request.Context(), requestID, request.Method, statusClass, startedAt, nil)
		return response, nil
	}

	response.Body = &dependencyBody{
		body: response.Body,
		finish: func(bodyErr error) {
			terminalStatus := statusClass
			if bodyErr != nil {
				terminalStatus = "error"
			}
			t.logEvent(request.Context(), requestID, request.Method, terminalStatus, startedAt, bodyErr)
		},
	}
	return response, nil
}

func (t *dependencyTransport) logEvent(ctx context.Context, requestID, method, statusClass string, startedAt time.Time, err error) {
	duration := time.Since(startedAt)
	method = HTTPMethod(method)
	for _, observer := range t.observers {
		if observer != nil {
			observer.Observe(t.dependency, t.operation, method, statusClass, duration)
		}
	}
	fields := []zap.Field{
		zap.String("event", "dependency_request"),
		zap.String("dependency", t.dependency),
		zap.String("operation", t.operation),
		zap.String("method", method),
		zap.String("status_class", statusClass),
		zap.Int64("duration_ms", duration.Milliseconds()),
		zap.String("release", t.release),
	}
	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}
	if err != nil {
		fields = append(fields, zap.String("error_class", classifyDependencyError(ctx, err)))
	}
	t.logger.Info("dependency_request", fields...)
}

type dependencyBody struct {
	body   io.ReadCloser
	finish func(error)
	once   sync.Once
}

func (b *dependencyBody) Read(buffer []byte) (int, error) {
	read, err := b.body.Read(buffer)
	if err != nil {
		if errors.Is(err, io.EOF) {
			b.complete(nil)
		} else {
			b.complete(err)
		}
	}
	return read, err
}

func (b *dependencyBody) Close() error {
	err := b.body.Close()
	b.complete(err)
	return err
}

func (b *dependencyBody) complete(err error) {
	b.once.Do(func() {
		b.finish(err)
	})
}

func (t *dependencyTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func classifyDependencyError(ctx context.Context, err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return "timeout"
	}
	return "transport_error"
}
