package deployment

import (
	"context"
	"errors"
	"fmt"
	"runtime"
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

type restartManagerStub struct {
	result SystemdOperationResult
	err    error
	called bool
}

func (r *restartManagerStub) Restart(context.Context) (SystemdOperationResult, error) {
	r.called = true
	if r.err != nil {
		return SystemdOperationResult{}, r.err
	}
	return r.result, nil
}

type systemdUpdaterStub struct {
	appliedArchiveURL   string
	appliedChecksumsURL string
	rollbackCalled      bool
	applyErr            error
	rollbackErr         error
}

func (s *systemdUpdaterStub) ApplyRelease(_ context.Context, archiveURL, checksumsURL string) (SystemdOperationResult, error) {
	s.appliedArchiveURL = archiveURL
	s.appliedChecksumsURL = checksumsURL
	if s.applyErr != nil {
		return SystemdOperationResult{}, s.applyErr
	}
	return SystemdOperationResult{Message: "update completed", NeedRestart: true}, nil
}

func (s *systemdUpdaterStub) Rollback(context.Context) error {
	s.rollbackCalled = true
	return s.rollbackErr
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
		nil,
		nil,
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
		nil,
		nil,
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

	svcSystemd := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		nil,
		&systemdUpdaterStub{},
		nil,
	)
	status, err = svcSystemd.Status(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status.UpdateStatus.Phase != "idle" {
		t.Fatalf("expected idle phase for systemd mode, got %q", status.UpdateStatus.Phase)
	}
}

func TestDeploymentServiceBundledModeDoesNotDependOnUpdaterClient(t *testing.T) {
	binaryUpdater := &systemdUpdaterStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled: true,
			},
		},
		VersionInfo{Version: "v0.6.0"},
		nil,
		nil,
		binaryUpdater,
		nil,
	)

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status.UpdateStatus.Phase != "idle" {
		t.Fatalf("expected idle phase without updater client in bundled mode, got %q", status.UpdateStatus.Phase)
	}
}

func TestDeploymentServiceBundledModeApplyUsesBinaryUpdater(t *testing.T) {
	archiveName := fmt.Sprintf("ai-efficiency-backend_0.6.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	binaryUpdater := &systemdUpdaterStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.5.0"},
		releaseStub{
			info: ReleaseInfo{
				Version: "v0.6.0",
				Assets: []ReleaseAsset{
					{Name: archiveName, DownloadURL: "https://example.com/archive.tgz"},
					{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
				},
			},
		},
		nil,
		binaryUpdater,
		nil,
	)

	status, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.6.0"})
	if err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if status.Phase != "updated" {
		t.Fatalf("phase = %q, want updated", status.Phase)
	}
	if binaryUpdater.appliedArchiveURL != "https://example.com/archive.tgz" {
		t.Fatalf("archive url = %q", binaryUpdater.appliedArchiveURL)
	}
	if binaryUpdater.appliedChecksumsURL != "https://example.com/checksums.txt" {
		t.Fatalf("checksums url = %q", binaryUpdater.appliedChecksumsURL)
	}
}

func TestDeploymentServiceBundledModeRollbackUsesBinaryUpdater(t *testing.T) {
	binaryUpdater := &systemdUpdaterStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.6.0"},
		nil,
		nil,
		binaryUpdater,
		nil,
	)

	status, err := svc.RollbackUpdate(context.Background())
	if err != nil {
		t.Fatalf("RollbackUpdate: %v", err)
	}
	if status.Phase != "rolled_back" {
		t.Fatalf("phase = %q, want rolled_back", status.Phase)
	}
	if !binaryUpdater.rollbackCalled {
		t.Fatal("expected binary updater rollback to be called")
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
		nil,
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
		nil,
		nil,
	)
	if _, err := disabled.ApplyUpdate(ctx, ApplyRequest{TargetVersion: "v0.5.0"}); err == nil || err.Error() != "deployment updates are disabled" {
		t.Fatalf("expected updates disabled error, got %v", err)
	} else if !IsPolicyError(err) {
		t.Fatalf("expected policy error, got %T", err)
	}
	if _, err := disabled.RollbackUpdate(ctx); err == nil || err.Error() != "deployment updates are disabled" {
		t.Fatalf("expected updates disabled error, got %v", err)
	} else if !IsPolicyError(err) {
		t.Fatalf("expected policy error, got %T", err)
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
		nil,
		nil,
	)
	if _, err := applyDisabled.ApplyUpdate(ctx, ApplyRequest{TargetVersion: "v0.5.0"}); err == nil || err.Error() != "deployment apply is disabled" {
		t.Fatalf("expected apply disabled error, got %v", err)
	} else if !IsPolicyError(err) {
		t.Fatalf("expected policy error, got %T", err)
	}
	if _, err := applyDisabled.RollbackUpdate(ctx); err == nil || err.Error() != "deployment apply is disabled" {
		t.Fatalf("expected apply disabled error, got %v", err)
	} else if !IsPolicyError(err) {
		t.Fatalf("expected policy error, got %T", err)
	}
}

func TestDeploymentServiceRestartInSystemdMode(t *testing.T) {
	restarter := &restartManagerStub{
		result: SystemdOperationResult{Message: "restart initiated", NeedRestart: true},
	}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		nil,
		nil,
		restarter,
	)

	status, err := svc.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !restarter.called {
		t.Fatalf("expected restarter to be called")
	}
	if status.Phase != "restart_requested" {
		t.Fatalf("expected restart_requested, got %+v", status)
	}
}

func TestDeploymentServiceRestartInBundledModeUsesProcessRestarter(t *testing.T) {
	restarter := &restartManagerStub{
		result: SystemdOperationResult{Message: "restart initiated", NeedRestart: true},
	}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.6.0"},
		nil,
		nil,
		nil,
		restarter,
	)

	status, err := svc.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !restarter.called {
		t.Fatal("expected restarter to be called")
	}
	if status.Phase != "restart_requested" {
		t.Fatalf("phase = %q, want restart_requested", status.Phase)
	}
}

func TestDeploymentServiceApplyUpdateRejectsStaleTargetVersionInSystemdMode(t *testing.T) {
	archiveName := fmt.Sprintf("ai-efficiency-backend_0.5.1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	svc := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		releaseStub{
			info: ReleaseInfo{
				Version: "v0.5.1",
				Assets: []ReleaseAsset{
					{Name: archiveName, DownloadURL: "https://example.com/archive.tgz"},
					{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
				},
			},
		},
		nil,
		&systemdUpdaterStub{},
		nil,
	)

	_, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err == nil {
		t.Fatal("expected target version mismatch error")
	}
	if !IsPolicyError(err) {
		t.Fatalf("expected policy error, got %T", err)
	}
	if got := err.Error(); got != "requested version v0.5.0 no longer matches latest available release v0.5.1" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestDeploymentServiceRestartRequiresConfiguredRestarter(t *testing.T) {
	svc := NewService(
		config.DeploymentConfig{
			Mode: "bundled",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		nil,
		nil,
		nil,
		nil,
	)

	_, err := svc.Restart(context.Background())
	if err == nil {
		t.Fatal("expected restart configuration error")
	}
	if got := err.Error(); got != "deployment restart is not configured" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestDeploymentServiceApplyAndRollbackInSystemdMode(t *testing.T) {
	archiveName := fmt.Sprintf("ai-efficiency-backend_0.5.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	source := releaseStub{
		info: ReleaseInfo{
			Version: "v0.5.0",
			URL:     "https://example.com/release/v0.5.0",
			Assets: []ReleaseAsset{
				{Name: archiveName, DownloadURL: "https://example.com/archive.tgz"},
				{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
			},
		},
	}
	systemdUpdater := &systemdUpdaterStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		source,
		nil,
		systemdUpdater,
		nil,
	)

	result, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if result.Message != "update completed" {
		t.Fatalf("result = %+v", result)
	}
	if systemdUpdater.appliedArchiveURL != "https://example.com/archive.tgz" {
		t.Fatalf("appliedArchiveURL = %q", systemdUpdater.appliedArchiveURL)
	}
	if systemdUpdater.appliedChecksumsURL != "https://example.com/checksums.txt" {
		t.Fatalf("appliedChecksumsURL = %q", systemdUpdater.appliedChecksumsURL)
	}

	if _, err := svc.RollbackUpdate(context.Background()); err != nil {
		t.Fatalf("RollbackUpdate: %v", err)
	}
	if !systemdUpdater.rollbackCalled {
		t.Fatalf("rollbackCalled = false")
	}
}
