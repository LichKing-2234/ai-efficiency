package telemetry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/internal/httpclient"
	"go.uber.org/zap"
)

func WrapDependency(logger *zap.Logger, release, dependency, operation string) httpclient.TransportWrapper {
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
		}
	}
}

type dependencyTransport struct {
	next       http.RoundTripper
	logger     *zap.Logger
	release    string
	dependency string
	operation  string
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
	statusClass := "unknown"
	if err != nil {
		statusClass = "error"
	} else if response != nil {
		statusClass = HTTPStatusClass(response.StatusCode)
	}

	fields := []zap.Field{
		zap.String("event", "dependency_request"),
		zap.String("dependency", t.dependency),
		zap.String("operation", t.operation),
		zap.String("method", request.Method),
		zap.String("status_class", statusClass),
		zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		zap.String("release", t.release),
	}
	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}
	if err != nil {
		fields = append(fields, zap.String("error_class", classifyDependencyError(request.Context(), err)))
	}
	t.logger.Info("dependency_request", fields...)

	return response, err
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
