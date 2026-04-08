package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/internal/deployment"
)

type stubDeploymentStatusReader struct{}

func (stubDeploymentStatusReader) Status(context.Context) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestHealthReadyRouteReturnsDeploymentPayload(t *testing.T) {
	SetDeploymentHandler(NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	t.Cleanup(func() {
		SetDeploymentHandler(nil)
	})

	env := setupFullTestEnv(t)
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
}

func TestDeploymentStatusRequiresAdmin(t *testing.T) {
	SetDeploymentHandler(NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	t.Cleanup(func() {
		SetDeploymentHandler(nil)
	})

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/settings/deployment", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
