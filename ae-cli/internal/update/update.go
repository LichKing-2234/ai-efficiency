package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultReleaseAPIURL          = "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest"
	defaultInstallScriptURLFormat = "https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/%s/ae-cli/%s"
	updateRequestTimeout          = 10 * time.Second
)

var (
	currentExecutable = os.Executable
	userHomeDir       = os.UserHomeDir
	httpDo            = func(req *http.Request) (*http.Response, error) {
		return (&http.Client{Timeout: updateRequestTimeout}).Do(req)
	}
)

type CheckOptions struct {
	CurrentVersion string
	ReleaseAPIURL  string
}

type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	LatestTag       string
	ReleaseURL      string
	UpdateAvailable bool
	Status          string
}

type InstallOptions struct {
	CurrentVersion   string
	ReleaseAPIURL    string
	InstallScriptURL string
	Force            bool
	Stdout           io.Writer
	Stderr           io.Writer
}

type InstallResult struct {
	PreviousVersion  string
	InstalledVersion string
	Updated          bool
	Status           string
}

type latestReleaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func CheckForUpdate(ctx context.Context, opts CheckOptions) (CheckResult, error) {
	currentTag, err := normalizeTag(opts.CurrentVersion)
	if err != nil {
		return CheckResult{}, fmt.Errorf("normalize current version: %w", err)
	}

	latest, err := fetchLatestRelease(ctx, opts.ReleaseAPIURL)
	if err != nil {
		return CheckResult{}, err
	}

	comparison, err := compareVersions(latest.TagName, currentTag)
	if err != nil {
		return CheckResult{}, err
	}

	status := "up_to_date"
	updateAvailable := false
	switch {
	case comparison > 0:
		status = "update_available"
		updateAvailable = true
	case comparison < 0:
		status = "current_newer"
	}

	return CheckResult{
		CurrentVersion:  currentTag,
		LatestVersion:   latest.TagName,
		LatestTag:       latest.TagName,
		ReleaseURL:      latest.HTMLURL,
		UpdateAvailable: updateAvailable,
		Status:          status,
	}, nil
}

func InstallLatest(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	info, err := CheckForUpdate(ctx, CheckOptions{
		CurrentVersion: opts.CurrentVersion,
		ReleaseAPIURL:  opts.ReleaseAPIURL,
	})
	if err != nil {
		return InstallResult{}, err
	}

	if !info.UpdateAvailable && !opts.Force {
		return InstallResult{
			PreviousVersion:  info.CurrentVersion,
			InstalledVersion: info.LatestTag,
			Updated:          false,
			Status:           info.Status,
		}, nil
	}

	executablePath, err := currentExecutable()
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve current executable: %w", err)
	}
	home, err := userHomeDir()
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve user home: %w", err)
	}
	if err := ensureOfficialInstallPath(executablePath, home); err != nil {
		return InstallResult{}, err
	}

	if runtime.GOOS == "windows" {
		return InstallResult{}, fmt.Errorf("ae-cli update install is not supported on Windows yet; rerun ae-cli/install.ps1 from PowerShell")
	}

	scriptURL := opts.InstallScriptURL
	if strings.TrimSpace(scriptURL) == "" {
		scriptURL = installScriptURLForTag(info.LatestTag)
	}
	script, err := downloadInstallScript(ctx, scriptURL)
	if err != nil {
		return InstallResult{}, err
	}

	scriptName := filepath.Base(scriptURL)
	if scriptName == "." || scriptName == "/" || scriptName == "" {
		scriptName = "install.sh"
	}

	tempDir, err := os.MkdirTemp("", "ae-cli-update-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	scriptPath := filepath.Join(tempDir, scriptName)
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("write install script: %w", err)
	}

	cmd := exec.CommandContext(ctx, "bash", scriptPath, info.LatestTag)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return InstallResult{}, fmt.Errorf("run install script: %w", err)
	}

	return InstallResult{
		PreviousVersion:  info.CurrentVersion,
		InstalledVersion: info.LatestTag,
		Updated:          true,
		Status:           "updated",
	}, nil
}

func fetchLatestRelease(ctx context.Context, overrideURL string) (*latestReleaseResponse, error) {
	url := strings.TrimSpace(overrideURL)
	if url == "" {
		url = defaultReleaseAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build latest release request: %w", err)
	}
	resp, err := httpDo(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch latest release: unexpected status %s", resp.Status)
	}

	var payload latestReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	payload.TagName, err = normalizeTag(payload.TagName)
	if err != nil {
		return nil, fmt.Errorf("normalize latest tag: %w", err)
	}
	return &payload, nil
}

func downloadInstallScript(ctx context.Context, scriptURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build install script request: %w", err)
	}
	resp, err := httpDo(req)
	if err != nil {
		return nil, fmt.Errorf("download install script: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download install script: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read install script: %w", err)
	}
	return data, nil
}

func installScriptURLForTag(tag string) string {
	scriptName := "install.sh"
	if runtime.GOOS == "windows" {
		scriptName = "install.ps1"
	}
	return fmt.Sprintf(defaultInstallScriptURLFormat, tag, scriptName)
}

func ensureOfficialInstallPath(currentPath, home string) error {
	currentResolved, err := resolvePath(currentPath)
	if err != nil {
		return fmt.Errorf("resolve current executable path: %w", err)
	}
	officialResolved, err := resolvePath(officialInstallPath(home))
	if err != nil {
		return fmt.Errorf("resolve official install path: %w", err)
	}
	if currentResolved == officialResolved {
		return nil
	}

	command := "curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash"
	if runtime.GOOS == "windows" {
		command = "iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex"
	}
	return fmt.Errorf("ae-cli update install only supports the official install location at %s; current executable is %s. Reinstall with `%s`", officialResolved, currentResolved, command)
}

func officialInstallPath(home string) string {
	name := "ae-cli"
	if runtime.GOOS == "windows" {
		name = "ae-cli.exe"
	}
	return filepath.Join(home, ".local", "bin", name)
}

func resolvePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		return "", absErr
	}
	return filepath.Clean(abs), nil
}

type parsedVersion struct {
	major      int
	minor      int
	patch      int
	prerelease []versionIdentifier
}

type versionIdentifier struct {
	raw       string
	numeric   bool
	numericID int
}

func normalizeTag(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("version must not be empty")
	}
	if !strings.HasPrefix(trimmed, "v") {
		trimmed = "v" + trimmed
	}
	if _, err := parseVersion(trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}

func compareVersions(left, right string) (int, error) {
	leftParsed, err := parseVersion(left)
	if err != nil {
		return 0, fmt.Errorf("parse version %q: %w", left, err)
	}
	rightParsed, err := parseVersion(right)
	if err != nil {
		return 0, fmt.Errorf("parse version %q: %w", right, err)
	}

	if leftParsed.major != rightParsed.major {
		if leftParsed.major > rightParsed.major {
			return 1, nil
		}
		return -1, nil
	}
	if leftParsed.minor != rightParsed.minor {
		if leftParsed.minor > rightParsed.minor {
			return 1, nil
		}
		return -1, nil
	}
	if leftParsed.patch != rightParsed.patch {
		if leftParsed.patch > rightParsed.patch {
			return 1, nil
		}
		return -1, nil
	}
	return comparePrerelease(leftParsed.prerelease, rightParsed.prerelease), nil
}

func comparePrerelease(left, right []versionIdentifier) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}

	for i := 0; i < len(left) && i < len(right); i++ {
		comparison := compareIdentifier(left[i], right[i])
		if comparison != 0 {
			return comparison
		}
	}
	switch {
	case len(left) > len(right):
		return 1
	case len(left) < len(right):
		return -1
	default:
		return 0
	}
}

func compareIdentifier(left, right versionIdentifier) int {
	switch {
	case left.numeric && right.numeric:
		if left.numericID > right.numericID {
			return 1
		}
		if left.numericID < right.numericID {
			return -1
		}
		return 0
	case left.numeric && !right.numeric:
		return -1
	case !left.numeric && right.numeric:
		return 1
	default:
		if left.raw > right.raw {
			return 1
		}
		if left.raw < right.raw {
			return -1
		}
		return 0
	}
}

func parseVersion(raw string) (parsedVersion, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "v")
	withoutBuild, _, _ := strings.Cut(trimmed, "+")
	core, prerelease, _ := strings.Cut(withoutBuild, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return parsedVersion{}, fmt.Errorf("unsupported version format")
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return parsedVersion{}, fmt.Errorf("unsupported version format")
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return parsedVersion{}, fmt.Errorf("unsupported version format")
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return parsedVersion{}, fmt.Errorf("unsupported version format")
	}

	parsed := parsedVersion{major: major, minor: minor, patch: patch}
	if prerelease == "" {
		return parsed, nil
	}
	segments := strings.Split(prerelease, ".")
	for _, segment := range segments {
		if segment == "" {
			return parsedVersion{}, fmt.Errorf("unsupported version format")
		}
		id := versionIdentifier{raw: segment}
		if numeric, err := strconv.Atoi(segment); err == nil {
			id.numeric = true
			id.numericID = numeric
		}
		parsed.prerelease = append(parsed.prerelease, id)
	}
	return parsed, nil
}
