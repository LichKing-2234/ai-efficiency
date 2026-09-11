package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/pilot"
)

// skipPilotEnv turns off every automatic LoongSuite Pilot action.
//
// Pilot's default configuration records conversation content, so refusing it
// has to be possible without argument. This covers the machines where it should
// never be installed at all — CI images, shared build hosts — where there is no
// interactive command to pass a flag to.
const skipPilotEnv = "AE_CLI_SKIP_PILOT"

// pilotEnsure is the seam tests replace. Production wiring runs Pilot's own
// installer.
var pilotEnsure = pilot.EnsureInstalled

func pilotSkipped() bool {
	value := strings.TrimSpace(os.Getenv(skipPilotEnv))
	if value == "" {
		return false
	}
	skip, err := strconv.ParseBool(value)
	return err == nil && skip
}

// ensurePilot installs LoongSuite Pilot when no service definition exists, and
// otherwise reports whether the installed one is still collecting.
//
// Only login calls this. Installing runs Pilot's own installer, which fetches
// and executes a script, deploys hooks into the developer's agents, and starts
// a background service — too much to happen as a side effect of any command a
// person did not run on purpose. Login is where someone is setting this machine
// up and is watching the output.
//
// It never returns an error. Login must not fail because a collector could not
// be set up: the per-agent readers still work without Pilot, so a failure here
// degrades accounting rather than breaking it. What it must not do is fail
// silently, so every outcome prints.
func ensurePilot(ctx context.Context, out, errOut io.Writer) {
	if pilotSkipped() {
		return
	}

	checker := pilot.Checker{}
	before := checker.Check()

	if before.State == pilot.StateAbsent {
		fmt.Fprintln(out, "Setting up LoongSuite Pilot to collect local AI usage...")
	}

	status, installed, err := pilotEnsure(ctx, checker, pilot.InstallOptions{
		Stdout: out,
		Stderr: errOut,
	})
	if err != nil {
		if errors.Is(err, pilot.ErrUnsupportedPlatform) {
			fmt.Fprintln(errOut, "LoongSuite Pilot: no installer for this platform; falling back to per-agent usage sources.")
			return
		}
		fmt.Fprintf(errOut, "LoongSuite Pilot: setup failed (%v). Falling back to per-agent usage sources.\n", err)
		fmt.Fprintf(errOut, "  Retry later with 'ae-cli login --force', or set %s=1 to stop offering.\n", skipPilotEnv)
		return
	}
	if installed {
		// The installer exiting zero is not proof that a service exists: on the
		// machine this was tested against it exited zero while launchd refused
		// the registration, because a stale one held the label. Reporting what
		// the follow-up check found keeps a claim of success from outliving the
		// thing it claims.
		if status.State == pilot.StateAbsent {
			fmt.Fprintln(errOut, "Warning: the LoongSuite Pilot installer finished but registered no service.")
			fmt.Fprintln(errOut, "  Check it with 'loongsuite-pilot status'. Usage falls back to per-agent sources until it runs.")
			return
		}
		fmt.Fprintln(out, "LoongSuite Pilot installed. It starts collecting as your agents run.")
		return
	}
	reportPilotStatus(status, out, errOut)
}

// notePilotStatus reports the collector's state without changing anything.
//
// This is what every path other than login gets. An upgrade that silently
// downloaded and ran a third-party installer would be a surprising thing for
// `ae-cli update` to do, so it points at login instead of acting.
func notePilotStatus(out, errOut io.Writer) {
	if pilotSkipped() {
		return
	}
	status := pilot.Checker{}.Check()
	if status.State == pilot.StateAbsent {
		fmt.Fprintf(errOut, "LoongSuite Pilot is not installed; usage falls back to per-agent sources.\n")
		fmt.Fprintf(errOut, "  Set it up with 'ae-cli login --force'.\n")
		return
	}
	reportPilotStatus(status, out, errOut)
}

// reportPilotStatus prints what a check found, escalating with how much is at
// risk rather than emitting one flat warning.
//
// A stall is not itself data loss: Pilot resumes from a byte cursor into each
// agent's own transcript, so restarting it backfills the gap. That only holds
// while those transcripts still exist, which is why a long enough gap is
// reported as a hard problem rather than a nag.
func reportPilotStatus(status pilot.Status, out, errOut io.Writer) {
	switch status.State {
	case pilot.StateHealthy:
		fmt.Fprintln(out, "LoongSuite Pilot is running.")
	case pilot.StateDisabled:
		// Someone turned it off. That is an instruction, not a fault.
	case pilot.StateStalled:
		fmt.Fprintln(errOut, pilotStallMessage(status))
	}
}

func pilotStallMessage(status pilot.Status) string {
	var b strings.Builder
	if status.Severity() == pilot.SeverityError {
		b.WriteString("Error: ")
	} else {
		b.WriteString("Warning: ")
	}
	b.WriteString("LoongSuite Pilot is enabled but has stopped collecting")
	if status.LastOutputAt.IsZero() {
		b.WriteString(" and has never written any usage.")
	} else {
		fmt.Fprintf(&b, " for %s (last usage %s).", humanizePilotGap(status.Gap), status.LastOutputAt.Format(time.RFC3339))
	}
	b.WriteString("\n  Restart it with 'loongsuite-pilot restart'.")
	if status.Unrecoverable() {
		// Past the agents' own transcript retention the source of a backfill is
		// gone, so this gap is not merely late — part of it can never arrive.
		b.WriteString("\n  This gap is longer than the agents keep their session files, so the oldest usage in it can no longer be recovered.")
	}
	return b.String()
}

func humanizePilotGap(gap time.Duration) string {
	switch {
	case gap >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(gap.Hours())/24)
	case gap >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(gap.Hours()))
	default:
		return fmt.Sprintf("%d minutes", int(gap.Minutes()))
	}
}

// printPilotDiagnostic reports the usage collector's state.
//
// Unlike the login and upgrade paths this never installs anything: doctor is
// where someone looks to understand a machine, and a diagnostic that changes
// the machine it is describing is a bad diagnostic.
func printPilotDiagnostic(out io.Writer) {
	fmt.Fprintf(out, "LoongSuite Pilot\n")
	if pilotSkipped() {
		fmt.Fprintf(out, "  Status:       skipped (%s is set)\n", skipPilotEnv)
		return
	}

	status := pilot.Checker{}.Check()
	fmt.Fprintf(out, "  Data Dir:     %s\n", status.DataDir)
	switch status.State {
	case pilot.StateAbsent:
		fmt.Fprintf(out, "  Status:       not installed (usage falls back to per-agent sources)\n")
		fmt.Fprintf(out, "  Recovery:     run 'ae-cli login --force' to set it up\n")
	case pilot.StateDisabled:
		fmt.Fprintf(out, "  Status:       disabled by this machine's owner\n")
	case pilot.StateHealthy:
		fmt.Fprintf(out, "  Status:       collecting\n")
		fmt.Fprintf(out, "  Last Usage:   %s\n", status.LastOutputAt.Format(time.RFC3339))
	case pilot.StateStalled:
		fmt.Fprintf(out, "  Status:       enabled but not collecting\n")
		if status.LastOutputAt.IsZero() {
			fmt.Fprintf(out, "  Last Usage:   never\n")
		} else {
			fmt.Fprintf(out, "  Last Usage:   %s (%s ago)\n", status.LastOutputAt.Format(time.RFC3339), humanizePilotGap(status.Gap))
		}
		fmt.Fprintf(out, "  Recovery:     loongsuite-pilot restart\n")
		if status.Unrecoverable() {
			fmt.Fprintf(out, "  Data Loss:    the oldest usage in this gap is past the agents' session retention and cannot be recovered\n")
		}
	}
}
