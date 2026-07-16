package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const defaultWebVitalsBodyLimit int64 = 4 << 10

type webVitalsRecorder interface {
	ObserveWebVital(telemetry.WebVitalSample) error
}

type WebVitalsOptions struct {
	Limiter   *rate.Limiter
	BodyLimit int64
}

type WebVitalsHandler struct {
	recorder  webVitalsRecorder
	limiter   *rate.Limiter
	bodyLimit int64
}

func NewWebVitalsHandler(recorder webVitalsRecorder, options WebVitalsOptions) *WebVitalsHandler {
	if options.Limiter == nil {
		options.Limiter = rate.NewLimiter(50, 100)
	}
	if options.BodyLimit <= 0 {
		options.BodyLimit = defaultWebVitalsBodyLimit
	}
	return &WebVitalsHandler{recorder: recorder, limiter: options.Limiter, bodyLimit: options.BodyLimit}
}

func (h *WebVitalsHandler) Record(c *gin.Context) {
	if h == nil || h.recorder == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "web vitals telemetry is unavailable")
		return
	}
	if !h.limiter.Allow() {
		pkg.Error(c, http.StatusTooManyRequests, "web vitals rate limit exceeded")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.bodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var sample telemetry.WebVitalSample
	if err := decoder.Decode(&sample); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid web vital sample")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		pkg.Error(c, http.StatusBadRequest, "invalid web vital sample")
		return
	}
	if err := telemetry.ValidateWebVitalSample(sample); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid web vital sample")
		return
	}
	if err := h.recorder.ObserveWebVital(sample); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid web vital sample")
		return
	}
	c.Status(http.StatusAccepted)
}

func RegisterWebVitalsRoutes(group *gin.RouterGroup, handler *WebVitalsHandler) {
	if handler == nil {
		return
	}
	telemetryGroup := group.Group("/telemetry")
	telemetryGroup.POST("/web-vitals", handler.Record)
}
