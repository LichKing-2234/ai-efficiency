package versioncheck

import (
	"context"
	"errors"
	"strings"

	"github.com/ai-efficiency/backend/internal/buildinfo"
	"golang.org/x/mod/semver"
)

var ErrCheckDisabled = errors.New("version check is not configured")

type ReleaseInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type Status struct {
	Version         buildinfo.VersionInfo `json:"version"`
	CheckEnabled    bool                  `json:"check_enabled"`
	Checked         bool                  `json:"checked,omitempty"`
	UpdateAvailable bool                  `json:"update_available"`
	LatestRelease   *ReleaseInfo          `json:"latest_release,omitempty"`
}

type ReleaseSource interface {
	Latest(context.Context) (ReleaseInfo, error)
}

type ReleaseSourceFunc func(context.Context) (ReleaseInfo, error)

func (f ReleaseSourceFunc) Latest(ctx context.Context) (ReleaseInfo, error) {
	return f(ctx)
}

type Service struct {
	version buildinfo.VersionInfo
	source  ReleaseSource
}

func NewService(version buildinfo.VersionInfo, source ReleaseSource) *Service {
	return &Service{
		version: version,
		source:  source,
	}
}

func (s *Service) Status() Status {
	return Status{
		Version:      s.version,
		CheckEnabled: s.source != nil,
	}
}

func (s *Service) CheckForUpdate(ctx context.Context) (Status, error) {
	status := s.Status()
	if s.source == nil {
		return status, ErrCheckDisabled
	}

	latest, err := s.source.Latest(ctx)
	if err != nil {
		return Status{}, err
	}

	status.Checked = true
	status.LatestRelease = &latest
	status.UpdateAvailable = isNewerRelease(latest.Version, s.version.Version)
	return status, nil
}

func isNewerRelease(latestVersion, currentVersion string) bool {
	latest, ok := normalizeSemver(latestVersion)
	if !ok {
		return false
	}
	current, ok := normalizeSemver(currentVersion)
	if !ok {
		return false
	}
	return semver.Compare(latest, current) > 0
}

func normalizeSemver(version string) (string, bool) {
	normalized := strings.TrimSpace(version)
	if normalized == "" {
		return "", false
	}
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	if !semver.IsValid(normalized) {
		return "", false
	}
	return semver.Canonical(normalized), true
}
