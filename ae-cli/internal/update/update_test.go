package update

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckForUpdateReportsAvailableRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example.com/releases/v0.2.0"}`))
	}))
	defer srv.Close()

	result, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	if result.Status != "update_available" {
		t.Fatalf("status = %q, want update_available", result.Status)
	}
	if result.LatestTag != "v0.2.0" {
		t.Fatalf("latest tag = %q, want v0.2.0", result.LatestTag)
	}
}

func TestCheckForUpdateAddsProxyGuidanceWhenGitHubReleaseFetchFails(t *testing.T) {
	oldHTTPDo := httpDo
	defer func() { httpDo = oldHTTPDo }()

	httpDo = func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp timeout")
	}

	_, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest",
	})
	if err == nil {
		t.Fatal("expected release fetch to fail")
	}
	if !strings.Contains(err.Error(), "GitHub Releases") {
		t.Fatalf("error = %q, want GitHub Releases guidance", err)
	}
	if !strings.Contains(err.Error(), "HTTPS_PROXY") {
		t.Fatalf("error = %q, want proxy guidance", err)
	}
}

func TestInstallLatestDoesNotRequireExecutablePathWhenAlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	defer srv.Close()

	oldExecutable := currentExecutable
	defer func() { currentExecutable = oldExecutable }()

	called := false
	currentExecutable = func() (string, error) {
		called = true
		return "", errors.New("should not be called")
	}

	result, err := InstallLatest(context.Background(), InstallOptions{
		CurrentVersion: "v0.2.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("InstallLatest: %v", err)
	}
	if called {
		t.Fatal("expected InstallLatest to skip executable lookup when already up to date")
	}
	if result.Updated {
		t.Fatal("expected no update to be installed")
	}
	if result.Status != "up_to_date" {
		t.Fatalf("status = %q, want up_to_date", result.Status)
	}
}

func TestInstallLatestRunsInstallerScriptForManagedUnixInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix installer test")
	}

	home := t.TempDir()
	officialPath := filepath.Join(home, ".local", "bin", "ae-cli")
	if err := os.MkdirAll(filepath.Dir(officialPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(officialPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	installerArgFile := filepath.Join(t.TempDir(), "installer-arg.txt")
	t.Setenv("AE_TEST_UPDATE_ARG_FILE", installerArgFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
		case "/install.sh":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$1" > "$AE_TEST_UPDATE_ARG_FILE"
echo "installed $1"
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldExecutable := currentExecutable
	oldHome := userHomeDir
	defer func() {
		currentExecutable = oldExecutable
		userHomeDir = oldHome
	}()

	currentExecutable = func() (string, error) { return officialPath, nil }
	userHomeDir = func() (string, error) { return home, nil }

	var out bytes.Buffer
	result, err := InstallLatest(context.Background(), InstallOptions{
		CurrentVersion:   "v0.1.0",
		ReleaseAPIURL:    srv.URL + "/latest",
		InstallScriptURL: srv.URL + "/install.sh",
		Stdout:           &out,
		Stderr:           &out,
	})
	if err != nil {
		t.Fatalf("InstallLatest: %v", err)
	}
	if !result.Updated {
		t.Fatal("expected update to be installed")
	}
	if result.InstalledVersion != "v0.2.0" {
		t.Fatalf("installed version = %q, want v0.2.0", result.InstalledVersion)
	}
	data, err := os.ReadFile(installerArgFile)
	if err != nil {
		t.Fatalf("ReadFile(installer arg): %v", err)
	}
	if string(data) != "v0.2.0" {
		t.Fatalf("installer arg = %q, want v0.2.0", string(data))
	}
	if !strings.Contains(out.String(), "installed v0.2.0") {
		t.Fatalf("output = %q, want installer output", out.String())
	}
}

func TestInstallLatestRejectsUnofficialInstallPath(t *testing.T) {
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	defer srv.Close()

	oldExecutable := currentExecutable
	oldHome := userHomeDir
	defer func() {
		currentExecutable = oldExecutable
		userHomeDir = oldHome
	}()

	currentExecutable = func() (string, error) { return "/usr/local/bin/ae-cli", nil }
	userHomeDir = func() (string, error) { return home, nil }

	_, err := InstallLatest(context.Background(), InstallOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err == nil {
		t.Fatal("expected unofficial install path to be rejected")
	}
	if !strings.Contains(err.Error(), "official install location") {
		t.Fatalf("error = %q, want official-install guidance", err)
	}
}
