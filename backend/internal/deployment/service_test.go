package deployment

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
)

type updaterStub struct {
	status         UpdateStatus
	applyReq       ApplyRequest
	applyResp      UpdateStatus
	rollbackResp   UpdateStatus
	statusErr      error
	applyErr       error
	rollbackErr    error
	statusCalled   bool
	applyCalled    bool
	rollbackCalled bool
}

func (u *updaterStub) Status(context.Context) (UpdateStatus, error) {
	u.statusCalled = true
	if u.statusErr != nil {
		return UpdateStatus{}, u.statusErr
	}
	return u.status, nil
}

func (u *updaterStub) Apply(_ context.Context, req ApplyRequest) (UpdateStatus, error) {
	u.applyCalled = true
	u.applyReq = req
	if u.applyErr != nil {
		return UpdateStatus{}, u.applyErr
	}
	return u.applyResp, nil
}

func (u *updaterStub) Rollback(context.Context) (UpdateStatus, error) {
	u.rollbackCalled = true
	if u.rollbackErr != nil {
		return UpdateStatus{}, u.rollbackErr
	}
	return u.rollbackResp, nil
}

type releaseStub struct {
	info ReleaseInfo
	err  error
}

func (s releaseStub) Latest(context.Context) (ReleaseInfo, error) {
	if s.err != nil {
		return ReleaseInfo{}, s.err
	}
	return s.info, nil
}

func TestDeploymentServiceCheckAndApplyUpdate(t *testing.T) {
	updater := &updaterStub{
		status:    UpdateStatus{Phase: "idle"},
		applyResp: UpdateStatus{Phase: "applying", TargetVersion: "v0.5.0"},
	}
	source := releaseStub{
		info: ReleaseInfo{
			Version: "v0.5.0",
			URL:     "https://example.com/release/v0.5.0",
		},
	}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		source,
		updater,
	)

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !status.UpdateAvailable {
		t.Fatalf("expected update available")
	}
	if status.LatestRelease == nil || status.LatestRelease.Version != "v0.5.0" {
		t.Fatalf("expected latest release v0.5.0, got %+v", status.LatestRelease)
	}

	resp, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updater.applyCalled {
		t.Fatalf("expected updater apply to be called")
	}
	if updater.applyReq.TargetVersion != "v0.5.0" {
		t.Fatalf("expected target version v0.5.0, got %q", updater.applyReq.TargetVersion)
	}
	if resp.TargetVersion != "v0.5.0" {
		t.Fatalf("expected response target version v0.5.0, got %q", resp.TargetVersion)
	}
}

type countingReleaseStub struct {
	called int
}

func (s *countingReleaseStub) Latest(context.Context) (ReleaseInfo, error) {
	s.called++
	return ReleaseInfo{Version: "v0.9.0"}, nil
}

func TestDeploymentServiceStatusResilientWhenUpdaterUnavailable(t *testing.T) {
	svcNoUpdater := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		nil,
	)
	status, err := svcNoUpdater.Status(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status.Version.Version != "v0.4.0" || status.Mode != "bundled" {
		t.Fatalf("expected basic deployment state, got %+v", status)
	}
	if status.UpdateStatus.Phase != "unavailable" {
		t.Fatalf("expected unavailable phase, got %q", status.UpdateStatus.Phase)
	}

	svcUpdaterErr := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		&updaterStub{statusErr: errors.New("updater down")},
	)
	status, err = svcUpdaterErr.Status(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status.UpdateStatus.Phase != "unavailable" {
		t.Fatalf("expected unavailable phase, got %q", status.UpdateStatus.Phase)
	}
	if status.UpdateStatus.Message != "updater down" {
		t.Fatalf("expected updater error message, got %q", status.UpdateStatus.Message)
	}
}

func TestDeploymentServiceCheckForUpdateSkipsSourceWhenDisabled(t *testing.T) {
	source := &countingReleaseStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled: false,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		source,
		nil,
	)
	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if source.called != 0 {
		t.Fatalf("expected release source not called, got %d", source.called)
	}
	if status.UpdateStatus.Phase != "disabled" {
		t.Fatalf("expected disabled phase, got %q", status.UpdateStatus.Phase)
	}
	if status.UpdateAvailable {
		t.Fatalf("expected update unavailable when disabled")
	}
}

func TestDeploymentServiceApplyAndRollbackEnforceUpdateFlags(t *testing.T) {
	ctx := context.Background()

	disabled := NewService(
		config.DeploymentConfig{
			Update: config.UpdateConfig{
				Enabled: false,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		&updaterStub{},
	)
	if _, err := disabled.ApplyUpdate(ctx, ApplyRequest{TargetVersion: "v0.5.0"}); err == nil || err.Error() != "deployment updates are disabled" {
		t.Fatalf("expected updates disabled error, got %v", err)
	}
	if _, err := disabled.RollbackUpdate(ctx); err == nil || err.Error() != "deployment updates are disabled" {
		t.Fatalf("expected updates disabled error, got %v", err)
	}

	applyDisabled := NewService(
		config.DeploymentConfig{
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: false,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		&updaterStub{},
	)
	if _, err := applyDisabled.ApplyUpdate(ctx, ApplyRequest{TargetVersion: "v0.5.0"}); err == nil || err.Error() != "deployment apply is disabled" {
		t.Fatalf("expected apply disabled error, got %v", err)
	}
	if _, err := applyDisabled.RollbackUpdate(ctx); err == nil || err.Error() != "deployment apply is disabled" {
		t.Fatalf("expected apply disabled error, got %v", err)
	}
}
