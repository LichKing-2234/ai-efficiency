// Package pilot reports whether LoongSuite Pilot is installed as a supervised
// service and whether it is still producing the output this CLI reads usage
// from.
//
// It deliberately does not manage the process. Pilot ships its own service
// definitions — a launchd agent on macOS, a systemd user unit on Linux — and
// those own liveness: they start it at login and restart it when it exits
// abnormally. Adding a second supervisor here would fight the first one, and
// would override the one way a person has of saying "leave it off".
//
// What the operating system's supervision cannot cover is a graceful stop that
// nobody asked for. Pilot's SIGTERM handler exits zero, and launchd's
// KeepAlive is conditioned on SuccessfulExit, so a plain kill looks exactly
// like an intentional shutdown: the service stays down for the rest of the
// login session without restarting and without reporting anything. That
// blind spot is the whole reason this package exists.
package pilot

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State is what a check found. It is deliberately four values rather than a
// boolean, because "not running" splits into cases that call for opposite
// responses: a service someone disabled must be left alone, while a service
// that is enabled and still not running is a fault worth reporting.
type State string

const (
	// StateAbsent means no service definition is installed. Pilot may still be
	// running from a source checkout, but nothing will restart it.
	StateAbsent State = "absent"
	// StateDisabled means the service is installed and someone turned it off.
	// This is an instruction, not a fault.
	StateDisabled State = "disabled"
	// StateHealthy means the service is installed, enabled, and recently wrote
	// output.
	StateHealthy State = "healthy"
	// StateStalled means the service is installed and enabled but has not
	// written output recently. This is the case the operating system's own
	// supervision cannot see.
	StateStalled State = "stalled"
)

// Severity is how loudly a stall should be reported. It escalates with the gap
// rather than firing one flat warning, because the cost of a stall is not
// constant: a short one costs nothing at all, and a long enough one is
// permanent data loss.
type Severity string

const (
	SeverityNone  Severity = "none"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

const (
	// FreshWindow is how recently Pilot must have written output to count as
	// healthy. Pilot rewrites its runtime record every 30s, but output only
	// appears when an agent actually produced something, so this is generous.
	FreshWindow = 2 * time.Hour

	// RecoverableWindow is the gap past which a stall is reported as an error.
	//
	// A stall is not itself data loss: Pilot resumes from a byte cursor into
	// each agent's own transcript, so restarting it backfills the gap. That
	// only holds while those transcripts still exist. Claude Code prunes its
	// sessions after cleanupPeriodDays, 30 by default, and Codex archives its
	// own. Past that horizon the source is gone and the gap can never be
	// recovered.
	//
	// Erring at three weeks leaves a week of margin to act on the report before
	// the oldest end of the gap starts aging out.
	RecoverableWindow = 21 * 24 * time.Hour

	// UnrecoverableAfter is the horizon RecoverableWindow leaves margin against:
	// the default retention of the agent transcripts a backfill reads from.
	UnrecoverableAfter = 30 * 24 * time.Hour
)

// Status is one check result.
type Status struct {
	State State
	// DataDir is the Pilot data directory the check looked at.
	DataDir string
	// LastOutputAt is when Pilot last wrote to its output directory. Zero when
	// it never has, or when the directory is unreadable.
	LastOutputAt time.Time
	// Gap is how long ago that was. Zero unless State is StateStalled.
	Gap time.Duration
}

// Severity classifies a status for reporting.
func (s Status) Severity() Severity {
	if s.State != StateStalled {
		return SeverityNone
	}
	if s.Gap >= RecoverableWindow {
		return SeverityError
	}
	return SeverityWarn
}

// Unrecoverable reports whether the oldest end of the gap has passed the
// retention of the transcripts a backfill would read from. Past this point
// restarting Pilot no longer recovers the whole gap.
func (s Status) Unrecoverable() bool {
	return s.State == StateStalled && s.Gap >= UnrecoverableAfter
}

// service abstracts the platform's service manager so a check is testable
// without a real launchd or systemd.
type service interface {
	// Installed reports whether a service definition exists.
	Installed() bool
	// Disabled reports whether the service exists and someone turned it off.
	Disabled() bool
	// InstalledAt is when the service definition was written. A service cannot
	// have produced anything before it existed, so this is the floor a silence
	// is measured from — without it, a machine set up a minute ago would report
	// its whole history as an outage.
	InstalledAt() time.Time
}

// Checker resolves Pilot's status. The zero value checks the real machine.
type Checker struct {
	// DataDir overrides Pilot's data directory. Empty means the default.
	DataDir string
	// service overrides the platform service manager. Nil means the real one.
	service service
	// now overrides the clock.
	now func() time.Time
}

// DefaultDataDir is where Pilot keeps its data unless told otherwise.
func DefaultDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("LOONGSUITE_PILOT_DATA_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".loongsuite-pilot")
}

// OutputDir is where Pilot writes the normalized JSONL this CLI reads.
func OutputDir(dataDir string) string {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "logs", "output")
}

func (c Checker) clock() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func (c Checker) dataDir() string {
	if dir := strings.TrimSpace(c.DataDir); dir != "" {
		return dir
	}
	return DefaultDataDir()
}

func (c Checker) svc() service {
	if c.service != nil {
		return c.service
	}
	return platformService()
}

// Check resolves the current status.
//
// The order matters. Whether a service definition exists, and whether someone
// disabled it, are both decided before output is looked at: a disabled service
// has no output by design, and reporting that as a stall would turn a person's
// own decision into a recurring complaint.
func (c Checker) Check() Status {
	status := Status{DataDir: c.dataDir()}

	// Disabled is asked first, and without requiring an installed service.
	// Stopping Pilot deletes its service definition but leaves the platform's
	// record that it was turned off, so asking "is it installed" first would
	// read a deliberate stop as a machine that was never set up — and login
	// would helpfully undo the person's decision.
	svc := c.svc()
	if svc.Disabled() {
		status.State = StateDisabled
		return status
	}
	if !svc.Installed() {
		status.State = StateAbsent
		return status
	}

	status.LastOutputAt = latestOutputWrite(OutputDir(status.DataDir))

	// Silence is measured from the later of the last output and the moment the
	// service was installed. A fresh install has produced nothing yet, and
	// reporting that as an outage would greet every new machine with a warning
	// about a gap that never happened. Measured this way, a machine installed
	// minutes ago is quiet, and one installed months ago that never collected
	// anything reports the full silence.
	since := status.LastOutputAt
	if installedAt := svc.InstalledAt(); installedAt.After(since) {
		since = installedAt
	}
	if since.IsZero() {
		// Neither the service definition nor the output could be dated. There is
		// nothing to measure, so claiming a fault would be an invention.
		status.State = StateHealthy
		return status
	}

	gap := c.clock().Sub(since)
	if gap < FreshWindow {
		status.State = StateHealthy
		return status
	}
	status.State = StateStalled
	status.Gap = gap
	return status
}

// latestOutputWrite is the most recent write to Pilot's output directory.
//
// Only Pilot's service writes there, so the staleness of that directory is the
// length of the outage, with no additional state to keep. The directory's own
// modification time is not enough: it changes when a file is created, not when
// an existing file is appended to, and Pilot appends to one file per agent per
// day.
func latestOutputWrite(outputDir string) time.Time {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return time.Time{}
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if modTime := info.ModTime().UTC(); modTime.After(latest) {
			latest = modTime
		}
	}
	return latest
}

// runtimeRecordName is the file Pilot rewrites while it is running, and removes
// when it shuts down cleanly.
const runtimeRecordName = "runtime.json"

// runtimeFreshWindow is how stale Pilot's runtime record may be and still mean
// "running". Pilot rewrites it every 30 seconds, so a few multiples of that
// absorbs a slow machine without calling a dead collector alive.
const runtimeFreshWindow = 3 * time.Minute

// Running reports whether Pilot's collector is alive right now.
//
// This is separate from Check, and answers a different question. Check asks
// whether a machine is set up to collect and reports when it has stopped
// producing; this asks whether reading Pilot's output is currently the right
// way to account for usage at all.
//
// The signal is Pilot's own runtime record: it rewrites the file every thirty
// seconds while running, and deletes it on a clean shutdown. So a missing file
// means stopped, a stale one means it died without cleaning up, and a fresh one
// means alive. Output freshness cannot answer this — a developer who has not
// used an agent since lunch has stale output and a perfectly healthy collector.
func (c Checker) Running() bool {
	path := filepath.Join(c.dataDir(), "logs", runtimeRecordName)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return c.clock().Sub(info.ModTime().UTC()) < runtimeFreshWindow
}
