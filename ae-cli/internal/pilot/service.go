package pilot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ServiceLabel is the identity Pilot's own installer registers its service
// under, on every platform that has one.
const ServiceLabel = "com.loongsuite-pilot"

// systemdUnitName is the same service as seen by systemd.
const systemdUnitName = "loongsuite-pilot.service"

// platformService returns the service manager for the running platform, or a
// checker that reports "absent" where this CLI cannot tell.
func platformService() service {
	home, err := os.UserHomeDir()
	if err != nil {
		return unknownService{}
	}
	switch runtime.GOOS {
	case "darwin":
		return launchdService{plistPath: filepath.Join(home, "Library", "LaunchAgents", ServiceLabel+".plist")}
	case "linux":
		return systemdUserService{unitPath: filepath.Join(home, ".config", "systemd", "user", systemdUnitName)}
	default:
		return unknownService{}
	}
}

// unknownService is used where this CLI has no reliable way to read the
// platform's service manager. It reports the service as absent rather than
// guessing, so the caller offers to install rather than reporting a fault that
// may not exist.
type unknownService struct{}

func (unknownService) Installed() bool        { return false }
func (unknownService) Disabled() bool         { return false }
func (unknownService) InstalledAt() time.Time { return time.Time{} }

// launchdService reads macOS launchd.
//
// Installation is read from the agent plist rather than from `launchctl print`,
// which reports a service as not found unless it is currently loaded into the
// caller's domain — that would misread a disabled or crashed service as never
// installed, which is the exact distinction this package exists to make.
type launchdService struct {
	plistPath string
	// disabledOutput overrides the launchctl call in tests.
	disabledOutput func() (string, bool)
}

func (s launchdService) Installed() bool {
	if strings.TrimSpace(s.plistPath) == "" {
		return false
	}
	info, err := os.Stat(s.plistPath)
	return err == nil && !info.IsDir()
}

// InstalledAt is when the agent plist was written.
func (s launchdService) InstalledAt() time.Time { return fileModTime(s.plistPath) }

// Disabled reads launchd's persistent disabled database.
//
// `loongsuite-pilot stop` unloads with -w, which records the service as
// disabled there, and that record survives a reboot. A service stopped any
// other way — a plain kill, for instance — leaves no such record and is not
// reported as disabled, which is correct: nobody asked for it to stay down.
func (s launchdService) Disabled() bool {
	if !s.Installed() {
		return false
	}
	out, ok := s.readDisabled()
	if !ok {
		// launchctl is unavailable or refused. Reporting "not disabled" would
		// risk nagging about a service someone deliberately turned off, so
		// treat an unreadable database as a deliberate stop.
		return true
	}
	return launchdLabelDisabled(out, ServiceLabel)
}

func (s launchdService) readDisabled() (string, bool) {
	if s.disabledOutput != nil {
		return s.disabledOutput()
	}
	uid := os.Getuid()
	if uid < 0 {
		return "", false
	}
	cmd := exec.Command("launchctl", "print-disabled", "gui/"+strconv.Itoa(uid))
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// launchdLabelDisabled finds one label in `launchctl print-disabled` output,
// whose body is a list of `"<label>" => enabled|disabled` lines. A label that
// appears in neither state has never been explicitly set and is not disabled.
func launchdLabelDisabled(out, label string) bool {
	needle := `"` + label + `"`
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, needle) {
			continue
		}
		return strings.HasSuffix(line, "disabled")
	}
	return false
}

// systemdUserService reads a systemd user unit.
type systemdUserService struct {
	unitPath string
	// enabledState overrides the systemctl call in tests.
	enabledState func() (string, bool)
}

func (s systemdUserService) Installed() bool {
	if strings.TrimSpace(s.unitPath) == "" {
		return false
	}
	info, err := os.Stat(s.unitPath)
	return err == nil && !info.IsDir()
}

// InstalledAt is when the unit file was written.
func (s systemdUserService) InstalledAt() time.Time { return fileModTime(s.unitPath) }

// Disabled asks systemd. `is-enabled` exits non-zero for a disabled unit and
// prints the state either way, so the printed word is read rather than the exit
// status.
func (s systemdUserService) Disabled() bool {
	if !s.Installed() {
		return false
	}
	state, ok := s.readEnabled()
	if !ok {
		return true
	}
	switch strings.TrimSpace(state) {
	case "disabled", "masked", "masked-runtime":
		return true
	default:
		return false
	}
}

func (s systemdUserService) readEnabled() (string, bool) {
	if s.enabledState != nil {
		return s.enabledState()
	}
	cmd := exec.Command("systemctl", "--user", "is-enabled", systemdUnitName)
	out, _ := cmd.Output()
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "", false
	}
	return state, true
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}
