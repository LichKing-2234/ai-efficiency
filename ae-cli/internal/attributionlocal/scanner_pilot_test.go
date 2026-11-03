package attributionlocal

import (
	"path/filepath"
	"testing"
	"time"
)

func pilotScannerResponse(workspace, responseID string, observedAt time.Time) map[string]any {
	event := pilotResponseEvent(workspace, pilotAgentCodex, "sess-1", "t1", responseID, 12, 5)
	event["time_unix_nano"] = observedAt.UnixNano()
	return event
}

// With Pilot installed it is the only source for the agents it instruments. The
// Codex session reader in the fixture must not also report the same activity.
func TestScanner_PilotReplacesPerAgentSources(t *testing.T) {
	fixture := buildAttributionFixture(t)
	pilotDir := t.TempDir()
	writePilotJSONL(t, filepath.Join(pilotDir, "codex-2026-08-28.jsonl"),
		pilotScannerResponse(fixture.WorkspaceRoot, "resp-1", time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)))

	scanner := NewScanner()
	scanner.PilotOutputDir = pilotDir
	scanner.PilotRunning = func() bool { return true }
	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d (%+v), want only the Pilot event", len(events), events)
	}
	if got, want := events[0].DedupeKey, "pilot:codex:response:resp-1"; got != want {
		t.Fatalf("dedupe key = %q, want %q", got, want)
	}
	if events[0].WorkspaceID == "" {
		t.Fatal("workspace id is empty")
	}
	if got := events[0].InputTokens; got != 12 {
		t.Fatalf("input tokens = %d, want 12", got)
	}
}

// Pilot instruments the Kiro CLI, not the Kiro IDE, so the IDE readers keep
// running alongside it.
func TestScanner_PilotDoesNotSupersedeKiroIDE(t *testing.T) {
	fixture := buildKiroIDEAttributionFixture(t)
	pilotDir := t.TempDir()
	writePilotJSONL(t, filepath.Join(pilotDir, "codex-2026-08-28.jsonl"),
		pilotScannerResponse(fixture.WorkspaceRoot, "resp-1", time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)))

	scanner := NewScanner()
	scanner.PilotOutputDir = pilotDir
	scanner.PilotRunning = func() bool { return true }
	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var sawKiroIDE bool
	for _, ev := range events {
		if ev.DedupeKey == "kiro-ide:chat-sess-1:exec-1" {
			sawKiroIDE = true
		}
	}
	if !sawKiroIDE {
		t.Fatalf("events = %+v, want the Kiro IDE event to survive alongside Pilot", events)
	}
}

func TestScanner_WithoutPilotKeepsPerAgentSources(t *testing.T) {
	fixture := buildAttributionFixture(t)
	scanner := NewScanner()
	scanner.PilotOutputDir = filepath.Join(t.TempDir(), "absent")

	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(events) != 1 || events[0].DedupeKey != "codex-jsonl:sess-1:resp-1" {
		t.Fatalf("events = %+v, want the per-agent Codex event", events)
	}
}

// Turning Pilot off must hand the usage source back to the per-agent readers.
//
// Pilot's output directory survives being stopped, full of everything it
// recorded before. Deciding on the directory alone would keep the per-agent
// readers suppressed while nothing replaced them, and every agent turn from
// then on would go uncounted — silently, which is the worst way for an
// accounting tool to fail.
func TestScanner_FallsBackToPerAgentSourcesWhenPilotIsNotCollecting(t *testing.T) {
	fixture := buildAttributionFixture(t)
	pilotDir := t.TempDir()
	writePilotJSONL(t, filepath.Join(pilotDir, "codex-2026-08-28.jsonl"),
		pilotScannerResponse(fixture.WorkspaceRoot, "resp-1", time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)))

	scanner := NewScanner()
	scanner.PilotOutputDir = pilotDir
	scanner.PilotRunning = func() bool { return false }

	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(events) != 1 || events[0].DedupeKey != "codex-jsonl:sess-1:resp-1" {
		t.Fatalf("events = %+v, want the per-agent Codex event once Pilot stopped collecting", events)
	}
}
