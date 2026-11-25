package pilot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// InstallerURL is Pilot's published installer. It registers the platform
	// service, deploys the agent hooks, writes the local configuration, and
	// starts the collector.
	InstallerURL = "https://loongcollector-community-edition.oss-cn-shanghai.aliyuncs.com/loongsuite-pilot/installer.sh"

	// InstallerVersion pins what gets installed. An unpinned installer would
	// make every machine's collector a moving target, and would make the
	// output schema this CLI parses change without a release here.
	InstallerVersion = "1.2.0"

	// installerMaxBytes caps the download. The installer is a shell script of a
	// few hundred kilobytes; anything far larger is not what was expected.
	installerMaxBytes = 4 << 20

	// installerTimeout bounds the whole install. It runs npm underneath, so it
	// is not fast, but it must not hang a login indefinitely.
	installerTimeout = 10 * time.Minute
)

// InstallAgents are the agents this CLI sources usage from. Passing them
// explicitly is what makes the install non-interactive: without the flag the
// installer prompts for an agent selection.
var InstallAgents = []string{"claude-code", "codex", "kiro-cli"}

// InstallOptions configures one install.
type InstallOptions struct {
	// DataDir overrides Pilot's data directory. Empty means Pilot's default.
	DataDir string
	// Agents overrides the agent list. Empty means InstallAgents.
	Agents []string
	// Stdout and Stderr receive the installer's output. Nil discards it.
	Stdout io.Writer
	Stderr io.Writer
}

// ErrUnsupportedPlatform is returned where this CLI has no install path.
var ErrUnsupportedPlatform = fmt.Errorf("no LoongSuite Pilot installer for this platform")

// Install runs Pilot's own installer.
//
// The script is downloaded to a file and then executed by an explicit
// interpreter rather than piped into a shell. Piping means the script is
// executed as it arrives, so a download that fails halfway still runs whatever
// arrived — which for an installer is a half-configured machine. Writing it
// down first also leaves the exact script that ran on disk when something goes
// wrong, and lets the size cap apply before anything executes.
//
// This is still remote code from another project, executed on the developer's
// machine. That is a deliberate trade: it is Pilot's own supported installation
// path, and reimplementing service registration, hook deployment, and agent
// detection here would mean owning three platforms' worth of behaviour that
// Pilot already owns and keeps current.
func Install(ctx context.Context, opts InstallOptions) error {
	if runtime.GOOS == "windows" {
		// The Windows installer is a PowerShell script with different
		// arguments. Claiming support without having exercised it would be
		// worse than saying plainly that it is not wired up.
		return ErrUnsupportedPlatform
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return ErrUnsupportedPlatform
	}

	ctx, cancel := context.WithTimeout(ctx, installerTimeout)
	defer cancel()

	scriptPath, err := downloadInstaller(ctx)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	agents := opts.Agents
	if len(agents) == 0 {
		agents = InstallAgents
	}
	args := []string{
		scriptPath, "install",
		"--agents", strings.Join(agents, ","),
		"--version", InstallerVersion,
	}
	if dataDir := strings.TrimSpace(opts.DataDir); dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}

	cmd := exec.CommandContext(ctx, "bash", args...)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	// The installer prompts when it cannot decide something. Every decision it
	// would ask about is supplied above, so a closed stdin turns any remaining
	// prompt into a fast failure rather than a login that hangs forever.
	cmd.Stdin = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run LoongSuite Pilot installer: %w", err)
	}
	return nil
}

func downloadInstaller(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, InstallerURL, nil)
	if err != nil {
		return "", fmt.Errorf("build installer request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download LoongSuite Pilot installer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download LoongSuite Pilot installer: HTTP %d", resp.StatusCode)
	}

	file, err := os.CreateTemp("", "loongsuite-pilot-installer-*.sh")
	if err != nil {
		return "", fmt.Errorf("create installer file: %w", err)
	}
	path := file.Name()
	written, err := io.Copy(file, io.LimitReader(resp.Body, installerMaxBytes+1))
	closeErr := file.Close()
	if err != nil {
		os.Remove(path)
		return "", fmt.Errorf("save installer: %w", err)
	}
	if closeErr != nil {
		os.Remove(path)
		return "", fmt.Errorf("save installer: %w", closeErr)
	}
	if written > installerMaxBytes {
		os.Remove(path)
		return "", fmt.Errorf("save installer: response exceeds %d bytes", installerMaxBytes)
	}
	if written == 0 {
		os.Remove(path)
		return "", fmt.Errorf("save installer: response was empty")
	}
	return path, nil
}

// EnsureInstalled installs Pilot when no service definition exists, and does
// nothing otherwise.
//
// Doing nothing covers two cases that look different but call for the same
// response. A healthy service needs no help. A service someone disabled is an
// instruction, and reinstalling over it would override the only way a person
// has of turning collection off — which matters here, because Pilot's default
// configuration captures conversation content.
func EnsureInstalled(ctx context.Context, checker Checker, opts InstallOptions) (Status, bool, error) {
	status := checker.Check()
	if status.State != StateAbsent {
		return status, false, nil
	}
	if err := Install(ctx, opts); err != nil {
		return status, false, err
	}
	return checker.Check(), true, nil
}
