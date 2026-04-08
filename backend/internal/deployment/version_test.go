package deployment

import "testing"

func TestCurrentVersionUsesInjectedBuildInfo(t *testing.T) {
	origVersion := BuildVersion
	origCommit := BuildCommit
	origTime := BuildTime
	t.Cleanup(func() {
		BuildVersion = origVersion
		BuildCommit = origCommit
		BuildTime = origTime
	})

	BuildVersion = "1.2.3"
	BuildCommit = "abc123"
	BuildTime = "2026-04-08T12:00:00Z"

	info := CurrentVersion()
	if info.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", info.Version, "1.2.3")
	}
	if info.Commit != "abc123" {
		t.Errorf("commit = %q, want %q", info.Commit, "abc123")
	}
	if info.BuildTime != "2026-04-08T12:00:00Z" {
		t.Errorf("build_time = %q, want %q", info.BuildTime, "2026-04-08T12:00:00Z")
	}
}
