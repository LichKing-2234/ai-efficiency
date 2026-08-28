package pilot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeService struct {
	installed   bool
	disabled    bool
	installedAt time.Time
}

func (s fakeService) Installed() bool        { return s.installed }
func (s fakeService) Disabled() bool         { return s.disabled }
func (s fakeService) InstalledAt() time.Time { return s.installedAt }

var checkNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

// pilotDataDir builds a data directory whose output was last written at the
// given time. A zero time leaves the output directory empty.
func pilotDataDir(t *testing.T, lastWrite time.Time) string {
	t.Helper()
	dataDir := t.TempDir()
	outputDir := OutputDir(dataDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if lastWrite.IsZero() {
		return dataDir
	}
	path := filepath.Join(outputDir, "claude-code-2026-08-28.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, lastWrite, lastWrite); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

func checkerFor(dataDir string, svc fakeService) Checker {
	if svc.installed && svc.installedAt.IsZero() {
		svc.installedAt = checkNow.Add(-365 * 24 * time.Hour)
	}
	return Checker{
		DataDir: dataDir,
		service: svc,
		now:     func() time.Time { return checkNow },
	}
}

func TestCheckReportsAbsentWhenNoServiceIsInstalled(t *testing.T) {
	dataDir := pilotDataDir(t, checkNow.Add(-time.Minute))
	got := checkerFor(dataDir, fakeService{installed: false}).Check()
	if got.State != StateAbsent {
		t.Fatalf("state = %q, want %q", got.State, StateAbsent)
	}
	if got.Severity() != SeverityNone {
		t.Fatalf("severity = %q, want %q: a missing install is fixed by installing, not reported as a fault", got.Severity(), SeverityNone)
	}
}

// A service someone turned off must never be reported as stalled, however long
// it has been silent. Reporting it would turn their decision into a recurring
// complaint, and Pilot's default configuration captures conversation content,
// so turning it off is a choice worth respecting.
func TestCheckStaysSilentForADeliberatelyDisabledService(t *testing.T) {
	dataDir := pilotDataDir(t, checkNow.Add(-90*24*time.Hour))
	got := checkerFor(dataDir, fakeService{installed: true, disabled: true}).Check()
	if got.State != StateDisabled {
		t.Fatalf("state = %q, want %q", got.State, StateDisabled)
	}
	if got.Severity() != SeverityNone {
		t.Fatalf("severity = %q, want %q even after 90 days of silence", got.Severity(), SeverityNone)
	}
}

func TestCheckReportsHealthyWhileOutputIsRecent(t *testing.T) {
	dataDir := pilotDataDir(t, checkNow.Add(-30*time.Minute))
	got := checkerFor(dataDir, fakeService{installed: true}).Check()
	if got.State != StateHealthy {
		t.Fatalf("state = %q, want %q", got.State, StateHealthy)
	}
	if got.Gap != 0 {
		t.Fatalf("gap = %s, want 0 for a healthy service", got.Gap)
	}
}

// This is the case the operating system's own supervision cannot see. Pilot's
// SIGTERM handler exits zero, so launchd's KeepAlive treats a plain kill as an
// intentional shutdown and leaves it down, silently, until the next login.
func TestCheckReportsStalledWhenAnEnabledServiceStopsProducing(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gap           time.Duration
		wantSeverity  Severity
		unrecoverable bool
	}{
		{name: "hours: launchd still recovers this at next login", gap: 5 * time.Hour, wantSeverity: SeverityWarn},
		{name: "days: past what the operating system will fix on its own", gap: 4 * 24 * time.Hour, wantSeverity: SeverityWarn},
		{name: "three weeks: approaching the transcript retention a backfill needs", gap: 22 * 24 * time.Hour, wantSeverity: SeverityError},
		{name: "past retention: the gap can no longer be recovered", gap: 31 * 24 * time.Hour, wantSeverity: SeverityError, unrecoverable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := pilotDataDir(t, checkNow.Add(-tc.gap))
			got := checkerFor(dataDir, fakeService{installed: true}).Check()
			if got.State != StateStalled {
				t.Fatalf("state = %q, want %q", got.State, StateStalled)
			}
			if got.Severity() != tc.wantSeverity {
				t.Fatalf("severity = %q, want %q for a %s gap", got.Severity(), tc.wantSeverity, tc.gap)
			}
			if got.Unrecoverable() != tc.unrecoverable {
				t.Fatalf("unrecoverable = %v, want %v", got.Unrecoverable(), tc.unrecoverable)
			}
		})
	}
}

// A machine set up minutes ago has collected nothing yet. Reporting that as an
// outage would greet every new install with a warning about a gap that never
// happened.
func TestCheckReportsHealthyForAFreshInstallThatHasNotCollectedYet(t *testing.T) {
	dataDir := pilotDataDir(t, time.Time{})
	svc := fakeService{installed: true, installedAt: checkNow.Add(-3 * time.Minute)}
	got := checkerFor(dataDir, svc).Check()
	if got.State != StateHealthy {
		t.Fatalf("state = %q, want %q for an install three minutes old", got.State, StateHealthy)
	}
}

// An install that is old and has still never collected anything is a real
// fault, and the silence is measured from when it was installed.
func TestCheckReportsStalledWhenAnOldInstallNeverWroteOutput(t *testing.T) {
	dataDir := pilotDataDir(t, time.Time{})
	svc := fakeService{installed: true, installedAt: checkNow.Add(-40 * 24 * time.Hour)}
	got := checkerFor(dataDir, svc).Check()
	if got.State != StateStalled {
		t.Fatalf("state = %q, want %q", got.State, StateStalled)
	}
	if got.Severity() != SeverityError {
		t.Fatalf("severity = %q, want %q for forty days of silence", got.Severity(), SeverityError)
	}
}

// Pilot appends to one output file per agent per day. A directory's own
// modification time does not change on append, so the check has to read the
// files.
func TestCheckReadsFileTimesRatherThanTheOutputDirectory(t *testing.T) {
	dataDir := pilotDataDir(t, checkNow.Add(-10*24*time.Hour))
	fresh := filepath.Join(OutputDir(dataDir), "codex-2026-08-28.jsonl")
	if err := os.WriteFile(fresh, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := checkNow.Add(-time.Minute)
	if err := os.Chtimes(fresh, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	// Backdate the directory itself so a directory-mtime check would report a
	// stall that the newest file disproves.
	old := checkNow.Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(OutputDir(dataDir), old, old); err != nil {
		t.Fatal(err)
	}

	got := checkerFor(dataDir, fakeService{installed: true}).Check()
	if got.State != StateHealthy {
		t.Fatalf("state = %q, want %q: the newest file is one minute old", got.State, StateHealthy)
	}
}

func TestLaunchdLabelDisabledReadsOnlyTheMatchingLabel(t *testing.T) {
	out := `	disabled services = {
		"com.docker.helper" => enabled
		"com.loongsuite-pilot" => disabled
		"com.other.thing" => enabled
	}`
	if !launchdLabelDisabled(out, ServiceLabel) {
		t.Fatal("want the disabled label to be found")
	}
	if launchdLabelDisabled(out, "com.docker.helper") {
		t.Fatal("want an enabled label not to read as disabled")
	}
	// A label launchd has never been told about appears in neither state, and
	// must not read as disabled: nobody asked for it to stay down.
	if launchdLabelDisabled(out, "com.never.registered") {
		t.Fatal("want an unlisted label not to read as disabled")
	}
}

// An unreadable disabled database must not be read as "not disabled": that
// would nag about a service someone turned off.
func TestLaunchdTreatsAnUnreadableDisabledDatabaseAsDisabled(t *testing.T) {
	plist := filepath.Join(t.TempDir(), ServiceLabel+".plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := launchdService{
		plistPath:      plist,
		disabledOutput: func() (string, bool) { return "", false },
	}
	if !svc.Installed() {
		t.Fatal("want the plist to count as installed")
	}
	if !svc.Disabled() {
		t.Fatal("want an unreadable launchctl database to read as disabled")
	}
}

func TestSystemdReadsTheEnabledState(t *testing.T) {
	unit := filepath.Join(t.TempDir(), systemdUnitName)
	if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for state, wantDisabled := range map[string]bool{
		"enabled":  false,
		"static":   false,
		"disabled": true,
		"masked":   true,
	} {
		svc := systemdUserService{
			unitPath:     unit,
			enabledState: func() (string, bool) { return state, true },
		}
		if got := svc.Disabled(); got != wantDisabled {
			t.Fatalf("state %q: disabled = %v, want %v", state, got, wantDisabled)
		}
	}
}

// Pilot rewrites its runtime record every thirty seconds while running and
// removes it on a clean shutdown, so the file is a precise liveness signal.
// Output freshness is not: a developer who has not used an agent since lunch
// has stale output and a perfectly healthy collector.
func TestRunningReadsPilotsOwnRuntimeRecord(t *testing.T) {
	for _, tc := range []struct {
		name    string
		write   bool
		age     time.Duration
		running bool
	}{
		{name: "absent means a clean shutdown", write: false, running: false},
		{name: "fresh means alive", write: true, age: 20 * time.Second, running: true},
		{name: "stale means it died without cleaning up", write: true, age: 10 * time.Minute, running: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			logsDir := filepath.Join(dataDir, "logs")
			if err := os.MkdirAll(logsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.write {
				path := filepath.Join(logsDir, runtimeRecordName)
				if err := os.WriteFile(path, []byte(`{"status":"active"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				stamp := checkNow.Add(-tc.age)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			c := Checker{DataDir: dataDir, now: func() time.Time { return checkNow }}
			if got := c.Running(); got != tc.running {
				t.Fatalf("Running() = %v, want %v", got, tc.running)
			}
		})
	}
}

// Stopping Pilot deletes its service definition but leaves launchd's record
// that it was turned off. Requiring an installed service before reading that
// record made a deliberate stop look like a machine that was never set up, and
// login reinstalled over the person's decision.
func TestCheckReportsDisabledEvenAfterTheServiceDefinitionIsRemoved(t *testing.T) {
	svc := launchdService{
		plistPath: filepath.Join(t.TempDir(), "com.loongsuite-pilot.plist"), // never written
		disabledOutput: func() (string, bool) {
			return "\tdisabled services = {\n\t\t\"" + ServiceLabel + "\" => disabled\n\t}", true
		},
	}
	if svc.Installed() {
		t.Fatal("the plist must be absent for this case to mean anything")
	}
	if !svc.Disabled() {
		t.Fatal("want a stopped service to read as disabled with no plist present")
	}

	got := Checker{
		DataDir: pilotDataDir(t, time.Time{}),
		service: svc,
		now:     func() time.Time { return checkNow },
	}.Check()
	if got.State != StateDisabled {
		t.Fatalf("state = %q, want %q: this is what stops login reinstalling over a deliberate stop", got.State, StateDisabled)
	}
}

// With neither a plist nor a disabled record, nothing was ever set up.
func TestCheckReportsAbsentWhenNothingWasEverRegistered(t *testing.T) {
	svc := launchdService{
		plistPath:      filepath.Join(t.TempDir(), "com.loongsuite-pilot.plist"),
		disabledOutput: func() (string, bool) { return "\tdisabled services = {\n\t}", true },
	}
	got := Checker{
		DataDir: pilotDataDir(t, time.Time{}),
		service: svc,
		now:     func() time.Time { return checkNow },
	}.Check()
	if got.State != StateAbsent {
		t.Fatalf("state = %q, want %q", got.State, StateAbsent)
	}
}
