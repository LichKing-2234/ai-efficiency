package main

import (
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
)

func newMetricsServer(addr string, metricsHandler http.Handler, cfg config.ServerConfig) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler)
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: time.Duration(cfg.ReadHeaderTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.IdleTimeoutSeconds) * time.Second,
	}
}
