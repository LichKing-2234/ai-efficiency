package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/buildinfo"
	"github.com/ai-efficiency/backend/internal/health"
	"github.com/ai-efficiency/backend/internal/versioncheck"
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

func TestSystemVersionRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(nil, nil, nil, buildinfo.VersionInfo{Version: "v0.4.0"}),
		versioncheck.NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, nil),
	))
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/system/version", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSystemVersionRouteReturnsCurrentVersion(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(nil, nil, nil, buildinfo.VersionInfo{Version: "v0.4.0"}),
		versioncheck.NewService(buildinfo.VersionInfo{Version: "v0.4.0", Commit: "abc123"}, nil),
	))

	w := doFullRequest(env, http.MethodGet, "/api/v1/system/version", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
	version, ok := data["version"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version object, got %T", data["version"])
	}
	if got, _ := version["version"].(string); got != "v0.4.0" {
		t.Fatalf("version = %q, want v0.4.0", got)
	}
	if _, exists := data["apply_url"]; exists {
		t.Fatalf("version payload exposed apply_url: %+v", data)
	}
}

func TestSystemVersionCheckRouteReturnsLatestRelease(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(nil, nil, nil, buildinfo.VersionInfo{Version: "v0.4.0"}),
		versioncheck.NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, versioncheck.ReleaseSourceFunc(func(context.Context) (versioncheck.ReleaseInfo, error) {
			return versioncheck.ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"}, nil
		})),
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/system/version/check", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
	if got, _ := data["update_available"].(bool); !got {
		t.Fatalf("update_available = %v, want true", data["update_available"])
	}
	latest, ok := data["latest_release"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected latest_release object, got %T", data["latest_release"])
	}
	if got, _ := latest["version"].(string); got != "v0.5.0" {
		t.Fatalf("latest version = %q, want v0.5.0", got)
	}
}

func TestSystemVersionCheckRouteReturnsConflictWhenDisabled(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(nil, nil, nil, buildinfo.VersionInfo{Version: "v0.4.0"}),
		versioncheck.NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, nil),
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/system/version/check", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "version check is not configured" {
		t.Fatalf("expected disabled message, got %q", msg)
	}
}

func TestBinaryUpgradeRoutesRemainRemoved(t *testing.T) {
	env := setupFullTestEnvWithHealth(t, NewHealthHandler(
		health.NewService(nil, nil, nil, buildinfo.VersionInfo{Version: "v0.4.0"}),
		versioncheck.NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, nil),
	))

	for _, path := range []string{
		"/api/v1/settings/deployment/update/apply",
		"/api/v1/settings/deployment/update/rollback",
		"/api/v1/settings/deployment/restart",
	} {
		w := doFullRequest(env, http.MethodPost, path, map[string]string{"target_version": "v0.5.0"})
		if w.Code != http.StatusNotFound {
			t.Fatalf("path %s expected 404, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}
