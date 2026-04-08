package deployment

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/internal/config"
)

type Service struct {
	cfg     config.DeploymentConfig
	version VersionInfo
	source  ReleaseSource
	updater Updater
}

type DeploymentStatus struct {
	Version         VersionInfo  `json:"version"`
	UpdateAvailable bool         `json:"update_available"`
	LatestRelease   *ReleaseInfo `json:"latest_release,omitempty"`
	UpdateStatus    UpdateStatus `json:"update_status"`
	Mode            string       `json:"mode"`
}

func NewService(cfg config.DeploymentConfig, version VersionInfo, source ReleaseSource, updater Updater) *Service {
	return &Service{
		cfg:     cfg,
		version: version,
		source:  source,
		updater: updater,
	}
}

func (s *Service) Status(ctx context.Context) (DeploymentStatus, error) {
	status := DeploymentStatus{
		Version: s.version,
		Mode:    s.cfg.Mode,
		UpdateStatus: UpdateStatus{
			Phase: "disabled",
		},
	}

	if s.updater != nil {
		updaterStatus, err := s.updater.Status(ctx)
		if err != nil {
			return DeploymentStatus{}, fmt.Errorf("read updater status: %w", err)
		}
		status.UpdateStatus = updaterStatus
	}

	return status, nil
}

func (s *Service) CheckForUpdate(ctx context.Context) (DeploymentStatus, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return DeploymentStatus{}, err
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
	if s.updater == nil {
		return UpdateStatus{}, fmt.Errorf("deployment updater is not configured")
	}
	return s.updater.Apply(ctx, req)
}

func (s *Service) RollbackUpdate(ctx context.Context) (UpdateStatus, error) {
	if s.updater == nil {
		return UpdateStatus{}, fmt.Errorf("deployment updater is not configured")
	}
	return s.updater.Rollback(ctx)
}
