package deployment

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/ai-efficiency/backend/internal/config"
)

type Service struct {
	cfg              config.DeploymentConfig
	version          VersionInfo
	source           ReleaseSource
	updater          Updater
	systemdUpdater   SystemdUpdater
	systemdRestarter RestartManager
}

type DeploymentStatus struct {
	Version         VersionInfo  `json:"version"`
	UpdateAvailable bool         `json:"update_available"`
	LatestRelease   *ReleaseInfo `json:"latest_release,omitempty"`
	UpdateStatus    UpdateStatus `json:"update_status"`
	Mode            string       `json:"mode"`
}

type policyError struct {
	message string
}

type SystemdUpdater interface {
	ApplyRelease(context.Context, string, string) (SystemdOperationResult, error)
	Rollback(context.Context) error
}

type RestartManager interface {
	Restart(context.Context) (SystemdOperationResult, error)
}

func (e *policyError) Error() string {
	return e.message
}

var (
	ErrUpdatesDisabled = &policyError{message: "deployment updates are disabled"}
	ErrApplyDisabled   = &policyError{message: "deployment apply is disabled"}
)

func IsPolicyError(err error) bool {
	var target *policyError
	return errors.As(err, &target)
}

func NewService(
	cfg config.DeploymentConfig,
	version VersionInfo,
	source ReleaseSource,
	updater Updater,
	systemdUpdater SystemdUpdater,
	systemdRestarter RestartManager,
) *Service {
	return &Service{
		cfg:              cfg,
		version:          version,
		source:           source,
		updater:          updater,
		systemdUpdater:   systemdUpdater,
		systemdRestarter: systemdRestarter,
	}
}

func (s *Service) Status(ctx context.Context) (DeploymentStatus, error) {
	status := DeploymentStatus{
		Version: s.version,
		Mode:    s.cfg.Mode,
		UpdateStatus: UpdateStatus{
			Phase: "unavailable",
		},
	}

	if !s.cfg.Update.Enabled {
		status.UpdateStatus = UpdateStatus{Phase: "disabled"}
		return status, nil
	}

	if s.updater == nil {
		return status, nil
	}

	updaterStatus, err := s.updater.Status(ctx)
	if err != nil {
		status.UpdateStatus = UpdateStatus{
			Phase:   "unavailable",
			Message: err.Error(),
		}
		return status, nil
	}
	status.UpdateStatus = updaterStatus

	return status, nil
}

func (s *Service) CheckForUpdate(ctx context.Context) (DeploymentStatus, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return DeploymentStatus{}, err
	}
	if !s.cfg.Update.Enabled {
		return status, nil
	}
	if s.source == nil {
		return status, nil
	}

	latest, err := s.source.Latest(ctx)
	if err != nil {
		return DeploymentStatus{}, fmt.Errorf("fetch latest release: %w", err)
	}
	status.LatestRelease = &latest
	status.UpdateAvailable = latest.Version != "" && latest.Version != s.version.Version

	return status, nil
}

func (s *Service) ApplyUpdate(ctx context.Context, req ApplyRequest) (UpdateStatus, error) {
	if !s.cfg.Update.Enabled {
		return UpdateStatus{}, ErrUpdatesDisabled
	}
	if !s.cfg.Update.ApplyEnabled {
		return UpdateStatus{}, ErrApplyDisabled
	}
	if s.cfg.Mode == "systemd" {
		if s.source == nil || s.systemdUpdater == nil {
			return UpdateStatus{}, fmt.Errorf("systemd updater is not configured")
		}
		release, err := s.source.Latest(ctx)
		if err != nil {
			return UpdateStatus{}, fmt.Errorf("fetch latest release: %w", err)
		}
		archive, checksums, err := SelectSystemdReleaseAssets(release.Assets, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return UpdateStatus{}, err
		}
		result, err := s.systemdUpdater.ApplyRelease(ctx, archive.DownloadURL, checksums.DownloadURL)
		if err != nil {
			return UpdateStatus{}, err
		}
		return UpdateStatus{
			Phase:         "updated",
			TargetVersion: release.Version,
			Message:       result.Message,
		}, nil
	}
	if s.updater == nil {
		return UpdateStatus{}, fmt.Errorf("deployment updater is not configured")
	}
	return s.updater.Apply(ctx, req)
}

func (s *Service) RollbackUpdate(ctx context.Context) (UpdateStatus, error) {
	if !s.cfg.Update.Enabled {
		return UpdateStatus{}, ErrUpdatesDisabled
	}
	if !s.cfg.Update.ApplyEnabled {
		return UpdateStatus{}, ErrApplyDisabled
	}
	if s.cfg.Mode == "systemd" {
		if s.systemdUpdater == nil {
			return UpdateStatus{}, fmt.Errorf("systemd updater is not configured")
		}
		if err := s.systemdUpdater.Rollback(ctx); err != nil {
			return UpdateStatus{}, err
		}
		return UpdateStatus{
			Phase:   "rolled_back",
			Message: "rollback completed",
		}, nil
	}
	if s.updater == nil {
		return UpdateStatus{}, fmt.Errorf("deployment updater is not configured")
	}
	return s.updater.Rollback(ctx)
}

func (s *Service) Restart(ctx context.Context) (UpdateStatus, error) {
	if s.cfg.Mode != "systemd" {
		return UpdateStatus{}, ErrApplyDisabled
	}
	if s.systemdRestarter == nil {
		return UpdateStatus{}, fmt.Errorf("systemd restart is not configured")
	}

	result, err := s.systemdRestarter.Restart(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}

	return UpdateStatus{
		Phase:   "restart_requested",
		Message: result.Message,
	}, nil
}
