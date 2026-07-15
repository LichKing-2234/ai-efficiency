package middleware

import (
	"time"

	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func RequestTelemetry(logger *zap.Logger, release string) gin.HandlerFunc {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(c *gin.Context) {
		startedAt := time.Now()
		requestID := selectRequestID(c.GetHeader(telemetry.HeaderRequestID))
		c.Request = c.Request.Clone(telemetry.WithRequestID(c.Request.Context(), requestID))
		c.Writer.Header().Set(telemetry.HeaderRequestID, requestID)

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		logger.Info(
			"http_request",
			zap.String("event", "http_request"),
			zap.String("route", route),
			zap.String("method", c.Request.Method),
			zap.String("status_class", telemetry.HTTPStatusClass(c.Writer.Status())),
			zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
			zap.Int("response_bytes", c.Writer.Size()),
			zap.String("release", release),
			zap.String("request_id", requestID),
		)
	}
}

func selectRequestID(incoming string) string {
	if validRequestID(incoming) {
		return incoming
	}
	return uuid.NewString()
}

func validRequestID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		char := id[index]
		if (char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}
