package update

import (
	"bytes"
	"context"
	"errors"
	"io"
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
		_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"}]`))
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
	if result.LatestTag != "ae-cli/v0.2.0" {
		t.Fatalf("latest tag = %q, want ae-cli/v0.2.0", result.LatestTag)
	}
	if result.LatestVersion != "v0.2.0" {
		t.Fatalf("latest version = %q, want v0.2.0", result.LatestVersion)
	}
}

func TestCheckForUpdateSelectsLatestIndependentCLIRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.1.0-preview.42","html_url":"https://example.com/releases/v0.1.0-preview.42"},
			{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"},
			{"tag_name":"ae-cli/v0.1.9","html_url":"https://example.com/releases/ae-cli/v0.1.9"}
		]`))
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
	if result.LatestVersion != "v0.2.0" {
		t.Fatalf("latest version = %q, want v0.2.0", result.LatestVersion)
	}
	if result.LatestTag != "ae-cli/v0.2.0" {
		t.Fatalf("latest tag = %q, want ae-cli/v0.2.0", result.LatestTag)
	}
	if result.ReleaseURL != "https://example.com/releases/ae-cli/v0.2.0" {
		t.Fatalf("release URL = %q", result.ReleaseURL)
	}
}

func TestCheckForUpdateRejectsReleaseListWithoutCLIRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.1.0-preview.42","html_url":"https://example.com/releases/v0.1.0-preview.42"}
		]`))
	}))
	defer srv.Close()

	_, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err == nil {
		t.Fatal("expected missing CLI release to fail")
	}
	if !strings.Contains(err.Error(), "no ae-cli release found") {
		t.Fatalf("error = %q, want no ae-cli release found", err)
	}
}

func TestCheckForUpdateRejectsBareReleaseTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"0.2.0","html_url":"https://example.com/releases/0.2.0"},
			{"tag_name":"v0.2.0","html_url":"https://example.com/releases/v0.2.0"},
			{"tag_name":"ae-cli/0.2.0","html_url":"https://example.com/releases/ae-cli/0.2.0"}
		]`))
	}))
	defer srv.Close()

	_, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err == nil {
		t.Fatal("expected bare release tags to be ignored")
	}
	if !strings.Contains(err.Error(), "no ae-cli release found") {
		t.Fatalf("error = %q, want no ae-cli release found", err)
	}
}

func TestCheckForUpdateFollowsReleasePaginationUntilCLIRelease(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/page1":
			w.Header().Set("Link", "<"+srv.URL+"/page2>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"tag_name":"v0.1.0-preview.42"}]`))
		case "/page2":
			_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL + "/page1",
	})
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.LatestTag != "ae-cli/v0.2.0" {
		t.Fatalf("latest tag = %q, want ae-cli/v0.2.0", result.LatestTag)
	}
}

func TestCheckForUpdateUsesFirstPublishedCLIRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"ae-cli/v0.1.9","html_url":"https://example.com/releases/ae-cli/v0.1.9"},
			{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"}
		]`))
	}))
	defer srv.Close()

	result, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.LatestTag != "ae-cli/v0.1.9" {
		t.Fatalf("latest tag = %q, want first published CLI release ae-cli/v0.1.9", result.LatestTag)
	}
}

func TestCheckForUpdateKeepsPlainReleaseFetchError(t *testing.T) {
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
	if !strings.Contains(err.Error(), "fetch latest release: dial tcp timeout") {
		t.Fatalf("error = %q, want plain fetch wrapper", err)
	}
	if strings.Contains(err.Error(), "GitHub Releases") || strings.Contains(err.Error(), "HTTPS_PROXY") {
		t.Fatalf("error = %q, want no onboarding proxy guidance in update package", err)
	}
}

func TestInstallLatestDownloadsInstallerFromReleaseTagByDefault(t *testing.T) {
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

	oldExecutable := currentExecutable
	oldHome := userHomeDir
	oldHTTPDo := httpDo
	defer func() {
		currentExecutable = oldExecutable
		userHomeDir = oldHome
		httpDo = oldHTTPDo
	}()

	currentExecutable = func() (string, error) { return officialPath, nil }
	userHomeDir = func() (string, error) { return home, nil }

	var scriptURL string
	httpDo = func(req *http.Request) (*http.Response, error) {
		body := `#!/usr/bin/env bash
set -euo pipefail
exit 0
`
		if req.URL.String() == "https://example.com/releases" {
			body = `[{"tag_name":"ae-cli/v0.2.0"}]`
		} else {
			scriptURL = req.URL.String()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}

	_, err := InstallLatest(context.Background(), InstallOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  "https://example.com/releases",
	})
	if err != nil {
		t.Fatalf("InstallLatest: %v", err)
	}
	wantURL := "https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/ae-cli/v0.2.0/ae-cli/install.sh"
	if scriptURL != wantURL {
		t.Fatalf("installer URL = %q, want %q", scriptURL, wantURL)
	}
}

func TestInstallLatestDoesNotRequireExecutablePathWhenAlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
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
			_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
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
	if string(data) != "ae-cli/v0.2.0" {
		t.Fatalf("installer arg = %q, want ae-cli/v0.2.0", string(data))
	}
	if !strings.Contains(out.String(), "installed ae-cli/v0.2.0") {
		t.Fatalf("output = %q, want installer output", out.String())
	}
}

func TestInstallLatestRejectsUnofficialInstallPath(t *testing.T) {
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
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
