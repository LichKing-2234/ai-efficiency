package middleware

import (
	"io"
	"net/http"

	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery contains panics without allowing Gin's raw request dump or panic value into logs.
func Recovery(logger *zap.Logger, release string) gin.HandlerFunc {
	if logger == nil {
		logger = zap.NewNop()
	}
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		logger.Error(
			"http_recovery",
			zap.String("event", "http_recovery"),
			zap.String("error_class", "panic"),
			zap.String("route", route),
			zap.String("method", telemetry.HTTPMethod(c.Request.Method)),
			zap.String("status_class", "5xx"),
			zap.String("release", release),
			zap.String("request_id", telemetry.RequestID(c.Request.Context())),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
