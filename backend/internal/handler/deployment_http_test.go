package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/deployment"
)

type stubDeploymentStatusReader struct {
	err         error
	applyErr    error
	rollbackErr error
	restartErr  error
	status      deployment.DeploymentStatus
}

func (s stubDeploymentStatusReader) Status(context.Context) (deployment.DeploymentStatus, error) {
	if s.err != nil {
		return deployment.DeploymentStatus{}, s.err
	}
	if s.status != (deployment.DeploymentStatus{}) {
		return s.status, nil
	}
	return deployment.DeploymentStatus{Mode: "bundled"}, nil
}

func (s stubDeploymentStatusReader) CheckForUpdate(ctx context.Context) (deployment.DeploymentStatus, error) {
	return s.Status(ctx)
}

func (s stubDeploymentStatusReader) ApplyUpdate(context.Context, deployment.ApplyRequest) (deployment.UpdateStatus, error) {
	if s.applyErr != nil {
		return deployment.UpdateStatus{}, s.applyErr
	}
	if s.err != nil {
		return deployment.UpdateStatus{}, s.err
	}
	return deployment.UpdateStatus{Phase: "applying"}, nil
}

func (s stubDeploymentStatusReader) RollbackUpdate(context.Context) (deployment.UpdateStatus, error) {
	if s.rollbackErr != nil {
		return deployment.UpdateStatus{}, s.rollbackErr
	}
	if s.err != nil {
		return deployment.UpdateStatus{}, s.err
	}
	return deployment.UpdateStatus{Phase: "rollback_started"}, nil
}

func (s stubDeploymentStatusReader) Restart(context.Context) (deployment.UpdateStatus, error) {
	if s.restartErr != nil {
		return deployment.UpdateStatus{}, s.restartErr
	}
	if s.err != nil {
		return deployment.UpdateStatus{}, s.err
	}
	return deployment.UpdateStatus{Phase: "restart_requested"}, nil
}

func TestHealthLiveRouteReturns200(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/health/live", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHealthReadyRouteReturnsDeploymentPayload(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return errors.New("raw relay connection details") }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
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

func TestDeploymentStatusRequiresAdmin(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/settings/deployment", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeploymentStatusAdminSuccess(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	w := doFullRequest(env, http.MethodGet, "/api/v1/settings/deployment", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if code, ok := resp["code"].(float64); !ok || int(code) != 200 {
		t.Fatalf("expected response code 200, got %+v", resp["code"])
	}
	if _, ok := resp["data"].(map[string]interface{}); !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
}

func TestDeploymentStatusReturns502WhenReaderFails(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{err: errors.New("boom")},
	))
	w := doFullRequest(env, http.MethodGet, "/api/v1/settings/deployment", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "boom" {
		t.Fatalf("expected message boom, got %q", msg)
	}
}

func TestDeploymentUpdateRoutesRequireAdmin(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))
	nonAdminToken := createFullNonAdminToken(t, env)

	cases := []struct {
		path string
		body []byte
	}{
		{path: "/api/v1/settings/deployment/update/check"},
		{path: "/api/v1/settings/deployment/update/apply", body: []byte(`{"target_version":"v0.5.0"}`)},
		{path: "/api/v1/settings/deployment/update/rollback"},
	}

	for _, tc := range cases {
		w := doFullRequestWithToken(env, http.MethodPost, tc.path, bytes.NewReader(tc.body), nonAdminToken)
		if w.Code != http.StatusForbidden {
			t.Fatalf("path %s expected 403, got %d: %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestDeploymentApplyUpdateSuccessEnvelope(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/update/apply", map[string]string{"target_version": "v0.5.0"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if code, ok := resp["code"].(float64); !ok || int(code) != 200 {
		t.Fatalf("expected response code 200, got %+v", resp["code"])
	}
	if _, ok := resp["data"].(map[string]interface{}); !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
}

func TestDeploymentApplyUpdateRejectsEmptyTargetVersion(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/update/apply", map[string]string{"target_version": "   "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "target_version is required" {
		t.Fatalf("expected target_version validation message, got %q", msg)
	}
}

func TestDeploymentRestartReturnsConflictWhenUnsupported(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{restartErr: deployment.ErrApplyDisabled},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/restart", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeploymentStatusAdminSuccessWhenUpdaterUnavailablePayload(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{
			status: deployment.DeploymentStatus{
				Mode: "bundled",
				UpdateStatus: deployment.UpdateStatus{
					Phase:   "unavailable",
					Message: "updater down",
				},
			},
		},
	))

	w := doFullRequest(env, http.MethodGet, "/api/v1/settings/deployment", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
	updateStatus, ok := data["update_status"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected update_status object, got %T", data["update_status"])
	}
	if phase, _ := updateStatus["phase"].(string); phase != "unavailable" {
		t.Fatalf("expected unavailable phase, got %q", phase)
	}
}

func TestDeploymentApplyUpdateReturns409ForPolicyDeny(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{
			applyErr: deployment.ErrUpdatesDisabled,
		},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/update/apply", map[string]string{"target_version": "v0.5.0"})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != deployment.ErrUpdatesDisabled.Error() {
		t.Fatalf("expected policy message %q, got %q", deployment.ErrUpdatesDisabled.Error(), msg)
	}
}

func TestDeploymentRollbackUpdateReturns502ForDownstreamFailure(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{
			rollbackErr: errors.New("updater transport failed"),
		},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/update/rollback", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "updater transport failed" {
		t.Fatalf("expected downstream message, got %q", msg)
	}
}
