package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
)

func TestMetricsServerUsesDedicatedMuxAndBoundedServerSettings(t *testing.T) {
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("metric 1\n"))
	})
	server := newMetricsServer("127.0.0.1:9090", metricsHandler, config.ServerConfig{
		ReadHeaderTimeoutSeconds: 5,
		IdleTimeoutSeconds:       120,
	})

	if server.Addr != "127.0.0.1:9090" {
		t.Fatalf("metrics server address = %q, want loopback address", server.Addr)
	}
	if server.ReadHeaderTimeout.Seconds() != 5 || server.IdleTimeout.Seconds() != 120 {
		t.Fatalf("metrics server timeouts = %s/%s, want 5s/120s", server.ReadHeaderTimeout, server.IdleTimeout)
	}

	metricsResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || metricsResponse.Body.String() != "metric 1\n" {
		t.Fatalf("GET /metrics = %d %q, want 200 metric payload", metricsResponse.Code, metricsResponse.Body.String())
	}
	otherResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(otherResponse, httptest.NewRequest(http.MethodGet, "/private", nil))
	if otherResponse.Code != http.StatusNotFound {
		t.Fatalf("GET /private = %d, want 404 on metrics listener", otherResponse.Code)
	}
}
