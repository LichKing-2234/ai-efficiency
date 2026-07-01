package versioncheck

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/backend/internal/buildinfo"
)

type releaseSourceStub struct {
	info ReleaseInfo
	err  error
}

func (s releaseSourceStub) Latest(context.Context) (ReleaseInfo, error) {
	if s.err != nil {
		return ReleaseInfo{}, s.err
	}
	return s.info, nil
}

func TestServiceStatusReturnsCurrentVersionWithoutNetworkCheck(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "v0.4.0", Commit: "abc123"}, releaseSourceStub{
		info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"},
	})

	status := svc.Status()

	if status.Version.Version != "v0.4.0" {
		t.Fatalf("version = %q, want v0.4.0", status.Version.Version)
	}
	if status.LatestRelease != nil {
		t.Fatalf("latest release = %+v, want nil before explicit check", status.LatestRelease)
	}
	if status.UpdateAvailable {
		t.Fatalf("update_available = true, want false before explicit check")
	}
}

func TestServiceStatusReportsCheckCapability(t *testing.T) {
	enabled := NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, releaseSourceStub{})
	if !enabled.Status().CheckEnabled {
		t.Fatalf("check_enabled = false, want true when release source is configured")
	}

	disabled := NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, nil)
	if disabled.Status().CheckEnabled {
		t.Fatalf("check_enabled = true, want false when release source is not configured")
	}
}

func TestServiceCheckForUpdateReturnsDisabledError(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "v0.4.0"}, nil)

	_, err := svc.CheckForUpdate(context.Background())
	if !errors.Is(err, ErrCheckDisabled) {
		t.Fatalf("CheckForUpdate error = %v, want ErrCheckDisabled", err)
	}
}

func TestServiceCheckForUpdateReportsLatestReleaseWithoutApplyCapability(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "0.4.0", Commit: "abc123"}, releaseSourceStub{
		info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"},
	})

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}

	if status.LatestRelease == nil || status.LatestRelease.Version != "v0.5.0" {
		t.Fatalf("latest release = %+v, want v0.5.0", status.LatestRelease)
	}
	if !status.UpdateAvailable {
		t.Fatalf("update_available = false, want true")
	}
}

func TestServiceCheckForUpdateTreatsVPrefixedSameVersionAsCurrent(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "0.5.0"}, releaseSourceStub{
		info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"},
	})

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}

	if status.UpdateAvailable {
		t.Fatalf("update_available = true, want false for matching v-prefixed release")
	}
}

func TestServiceCheckForUpdateDoesNotReportOlderLatestAsAvailable(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "v0.6.0"}, releaseSourceStub{
		info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"},
	})

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}

	if status.UpdateAvailable {
		t.Fatalf("update_available = true, want false when latest release is older than current")
	}
}

func TestServiceCheckForUpdateReportsCheckErrorForNonSemverCurrentVersion(t *testing.T) {
	svc := NewService(buildinfo.VersionInfo{Version: "dev"}, releaseSourceStub{
		info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/releases/v0.5.0"},
	})

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}

	if !status.Checked {
		t.Fatalf("checked = false, want true after latest-release check")
	}
	if status.UpdateAvailable {
		t.Fatalf("update_available = true, want false when current version is not semver")
	}
	if status.CheckError != "current version is not semver" {
		t.Fatalf("check_error = %q, want current version is not semver", status.CheckError)
	}
	if status.LatestRelease == nil || status.LatestRelease.Version != "v0.5.0" {
		t.Fatalf("latest release = %+v, want v0.5.0", status.LatestRelease)
	}
}
