package deployment

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
)

type updaterStub struct {
	status         UpdateStatus
	applyReq       ApplyRequest
	applyResp      UpdateStatus
	rollbackResp   UpdateStatus
	statusCalled   bool
	applyCalled    bool
	rollbackCalled bool
}

func (u *updaterStub) Status(context.Context) (UpdateStatus, error) {
	u.statusCalled = true
	return u.status, nil
}

func (u *updaterStub) Apply(_ context.Context, req ApplyRequest) (UpdateStatus, error) {
	u.applyCalled = true
	u.applyReq = req
	return u.applyResp, nil
}

func (u *updaterStub) Rollback(context.Context) (UpdateStatus, error) {
	u.rollbackCalled = true
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
		config.DeploymentConfig{Mode: "bundled"},
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
