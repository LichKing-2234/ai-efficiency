package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/pilot"
)

// The skip switch has to stop the install before anything is downloaded or run,
// not merely quiet the output. Machines that set it — CI images, shared build
// hosts — must never have another project's installer executed on them.
func TestEnsurePilotRunsNothingWhenSkipped(t *testing.T) {
	t.Setenv(skipPilotEnv, "1")
	called := false
	restore := pilotEnsure
	pilotEnsure = func(context.Context, pilot.Checker, pilot.InstallOptions) (pilot.Status, bool, error) {
		called = true
		return pilot.Status{}, false, nil
	}
	t.Cleanup(func() { pilotEnsure = restore })

	var out, errOut bytes.Buffer
	ensurePilot(context.Background(), &out, &errOut)

	if called {
		t.Fatal("the installer path ran while the skip switch was set")
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("out=%q errOut=%q, want silence", out.String(), errOut.String())
	}
}

// A machine that cannot set Pilot up must still finish logging in. The
// per-agent readers keep working without it, so this degrades accounting rather
// than breaking the command.
func TestEnsurePilotReportsFailureWithoutBreakingTheCommand(t *testing.T) {
	t.Setenv(skipPilotEnv, "")
	restore := pilotEnsure
	pilotEnsure = func(context.Context, pilot.Checker, pilot.InstallOptions) (pilot.Status, bool, error) {
		return pilot.Status{}, false, pilot.ErrUnsupportedPlatform
	}
	t.Cleanup(func() { pilotEnsure = restore })

	var out, errOut bytes.Buffer
	ensurePilot(context.Background(), &out, &errOut)

	if !strings.Contains(errOut.String(), "per-agent") {
		t.Fatalf("errOut = %q, want it to say accounting falls back", errOut.String())
	}
}

func TestPilotStallMessageEscalatesWithTheGap(t *testing.T) {
	lastUsage := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		gap        time.Duration
		wantPrefix string
		wantLoss   bool
	}{
		{name: "days", gap: 3 * 24 * time.Hour, wantPrefix: "Warning: "},
		{name: "three weeks", gap: 22 * 24 * time.Hour, wantPrefix: "Error: "},
		{name: "past transcript retention", gap: 31 * 24 * time.Hour, wantPrefix: "Error: ", wantLoss: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := pilotStallMessage(pilot.Status{
				State: pilot.StateStalled, Gap: tc.gap, LastOutputAt: lastUsage,
			})
			if !strings.HasPrefix(msg, tc.wantPrefix) {
				t.Fatalf("message = %q, want prefix %q", msg, tc.wantPrefix)
			}
			if !strings.Contains(msg, "loongsuite-pilot restart") {
				t.Fatalf("message = %q, want the repair command", msg)
			}
			if got := strings.Contains(msg, "cannot be recovered") || strings.Contains(msg, "no longer be recovered"); got != tc.wantLoss {
				t.Fatalf("message = %q, data-loss note = %v, want %v", msg, got, tc.wantLoss)
			}
		})
	}
}

// A service someone turned off must produce no output at all, however long it
// has been quiet.
func TestReportPilotStatusStaysSilentForADisabledService(t *testing.T) {
	var out, errOut bytes.Buffer
	reportPilotStatus(pilot.Status{State: pilot.StateDisabled, Gap: 90 * 24 * time.Hour}, &out, &errOut)
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("out=%q errOut=%q, want silence for a deliberately disabled service", out.String(), errOut.String())
	}
}

// The installer exiting zero is not proof that a service exists. On the machine
// this was first tested against it exited zero while launchd refused the
// registration, because a stale one from an earlier run held the label — and
// login reported success anyway.
func TestEnsurePilotDoesNotClaimSuccessWhenNoServiceWasRegistered(t *testing.T) {
	t.Setenv(skipPilotEnv, "")
	restore := pilotEnsure
	pilotEnsure = func(context.Context, pilot.Checker, pilot.InstallOptions) (pilot.Status, bool, error) {
		return pilot.Status{State: pilot.StateAbsent}, true, nil
	}
	t.Cleanup(func() { pilotEnsure = restore })

	var out, errOut bytes.Buffer
	ensurePilot(context.Background(), &out, &errOut)

	if strings.Contains(out.String(), "installed") && !strings.Contains(errOut.String(), "registered no service") {
		t.Fatalf("out = %q, errOut = %q; want the missing registration reported, not success", out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "registered no service") {
		t.Fatalf("errOut = %q, want it to say no service was registered", errOut.String())
	}
}
