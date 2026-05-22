package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/buildinfo"
	"github.com/ai-efficiency/backend/internal/health"
)

func TestHealthLiveRouteReturns200(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(
			health.FuncPinger(func(context.Context) error { return nil }),
			health.FuncPinger(func(context.Context) error { return nil }),
			health.FuncPinger(func(context.Context) error { return nil }),
			buildinfo.CurrentVersion(),
		),
	))
	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/health/live", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHealthReadyRouteReturnsPayload(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(
			health.FuncPinger(func(context.Context) error { return nil }),
			health.FuncPinger(func(context.Context) error { return nil }),
			health.FuncPinger(func(context.Context) error { return errors.New("raw relay connection details") }),
			buildinfo.CurrentVersion(),
		),
	))
	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/health/ready", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	if _, ok := resp["status"].(string); !ok {
		t.Fatalf("expected string status, got %T", resp["status"])
	}
	if _, ok := resp["version"].(map[string]interface{}); !ok {
		t.Fatalf("expected object version, got %T", resp["version"])
	}
	checks, ok := resp["checks"].([]interface{})
	if !ok || len(checks) < 3 {
		t.Fatalf("expected checks array, got %T", resp["checks"])
	}
	relayCheck, ok := checks[2].(map[string]interface{})
	if !ok {
		t.Fatalf("expected relay check object, got %T", checks[2])
	}
	message, _ := relayCheck["message"].(string)
	if message != "unavailable" {
		t.Fatalf("expected sanitized message unavailable, got %q", message)
	}
	if strings.Contains(w.Body.String(), "raw relay connection details") {
		t.Fatalf("response leaked raw downstream probe error: %s", w.Body.String())
	}
}
