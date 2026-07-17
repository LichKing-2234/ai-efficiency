package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

func TestWebVitalsHandlerAcceptsStrictBoundedSamplesWithoutEcho(t *testing.T) {
	recorder := &recordingWebVitalsRecorder{}
	handler := NewWebVitalsHandler(recorder, WebVitalsOptions{})
	router := gin.New()
	router.POST("/web-vitals", handler.Record)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/web-vitals", strings.NewReader(
		`{"metric":"LCP","value":2500,"route":"/repos/:id","navigation_type":"navigate"}`,
	)))
	if response.Code != http.StatusAccepted || response.Body.Len() != 0 {
		t.Fatalf("response = %d %q, want 202 empty body", response.Code, response.Body.String())
	}
	if len(recorder.samples) != 1 || recorder.samples[0].Metric != "LCP" || recorder.samples[0].Route != "/repos/:id" {
		t.Fatalf("recorded samples = %#v, want one LCP sample", recorder.samples)
	}
}

func TestWebVitalsHandlerRejectsInvalidUnknownTrailingAndOversizedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid sample", body: `{"metric":"FID","value":1,"route":"/usage","navigation_type":"navigate"}`},
		{name: "unknown field", body: `{"metric":"LCP","value":1,"route":"/usage","navigation_type":"navigate","email":"alice@example.com"}`},
		{name: "trailing JSON", body: `{"metric":"LCP","value":1,"route":"/usage","navigation_type":"navigate"}{}`},
		{name: "oversized", body: `{"metric":"LCP","value":1,"route":"/usage","navigation_type":"navigate","padding":"` + strings.Repeat("x", 5000) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingWebVitalsRecorder{}
			handler := NewWebVitalsHandler(recorder, WebVitalsOptions{})
			router := gin.New()
			router.POST("/web-vitals", handler.Record)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/web-vitals", strings.NewReader(tt.body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("response status = %d, want 400", response.Code)
			}
			if len(recorder.samples) != 0 {
				t.Fatalf("recorded samples = %#v, want none", recorder.samples)
			}
		})
	}
}

func TestWebVitalsHandlerRateLimitsBeforeRecording(t *testing.T) {
	recorder := &recordingWebVitalsRecorder{}
	handler := NewWebVitalsHandler(recorder, WebVitalsOptions{Limiter: rate.NewLimiter(0, 1)})
	router := gin.New()
	router.POST("/web-vitals", handler.Record)
	body := `{"metric":"CLS","value":0.1,"route":"/usage","navigation_type":"reload"}`

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/web-vitals", strings.NewReader(body)))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/web-vitals", strings.NewReader(body)))

	if first.Code != http.StatusAccepted || second.Code != http.StatusTooManyRequests {
		t.Fatalf("statuses = %d/%d, want 202/429", first.Code, second.Code)
	}
	if len(recorder.samples) != 1 {
		t.Fatalf("recorded samples = %d, want 1", len(recorder.samples))
	}
}

func TestRouterRequiresAuthenticationForWebVitals(t *testing.T) {
	client := testdb.Open(t)
	logger := zap.NewNop()
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoService := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	webVitalsHandler := NewWebVitalsHandler(&recordingWebVitalsRecorder{}, WebVitalsOptions{})
	router := setupRouterForTest(t,
		client,
		nil,
		authService,
		repoService,
		webhookHandler,
		nil,
		nil,
		"0000000000000000000000000000000000000000000000000000000000000000",
		"",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterOptions{WebVitalsHandler: webVitalsHandler},
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/web-vitals", strings.NewReader(
		`{"metric":"LCP","value":1,"route":"/usage","navigation_type":"navigate"}`,
	))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.Code)
	}
}

type recordingWebVitalsRecorder struct {
	samples []telemetry.WebVitalSample
	err     error
}

func (r *recordingWebVitalsRecorder) ObserveWebVital(sample telemetry.WebVitalSample) error {
	if r.err != nil {
		return r.err
	}
	r.samples = append(r.samples, sample)
	return nil
}
