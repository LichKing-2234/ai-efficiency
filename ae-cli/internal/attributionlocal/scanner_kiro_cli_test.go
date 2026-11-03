package attributionlocal

import (
	"path/filepath"
	"testing"
	"time"
)

// Pilot instruments Kiro through a custom agent the developer has to select:
// it writes ~/.kiro/agents/pilot-kiro.json and only a run started with that
// agent reports anything. A plain `kiro-cli` run reports nothing, and Pilot
// being installed used to stop Kiro's own store from being read at all, so that
// consumption was collected by neither reader and disappeared.
func TestScanner_PilotDoesNotSupersedeKiroCLIItDidNotObserve(t *testing.T) {
	fixture := buildKiroCLISQLiteAttributionFixture(t)
	pilotDir := t.TempDir()
	// Pilot saw a Codex turn and no Kiro at all.
	writePilotJSONL(t, filepath.Join(pilotDir, "codex-2026-08-28.jsonl"),
		pilotScannerResponse(fixture.WorkspaceRoot, "resp-1", time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)))

	scanner := NewScanner()
	scanner.PilotOutputDir = pilotDir
	scanner.PilotRunning = func() bool { return true }
	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !containsToolUsage(events, kiroToolName) {
		t.Fatalf("events = %+v, want Kiro's own store read for the sessions Pilot did not observe", events)
	}
}

// When Pilot did observe Kiro, its own store must not be read again. The two
// readers name the same consumption differently — kiro-cli:<conversation>:<event>
// against Pilot's response key — so nothing downstream would recognize the two
// records as one, and the machine would report the usage twice.
func TestScanner_KiroCLIIsNotCountedTwiceWhenPilotObservedIt(t *testing.T) {
	fixture := buildKiroCLISQLiteAttributionFixture(t)
	pilotDir := t.TempDir()
	kiroResponse := pilotResponseEvent(fixture.WorkspaceRoot, pilotAgentKiro, "sess-kiro", "sess-kiro:t1", "resp-kiro", 12, 5)
	kiroResponse["time_unix_nano"] = time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC).UnixNano()
	writePilotJSONL(t, filepath.Join(pilotDir, "kiro-cli-2026-08-28.jsonl"), kiroResponse)

	scanner := NewScanner()
	scanner.PilotOutputDir = pilotDir
	scanner.PilotRunning = func() bool { return true }
	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	kiro := 0
	for _, event := range events {
		if event.Tool == kiroToolName {
			kiro++
		}
	}
	if kiro != 1 {
		t.Fatalf("Kiro events = %d (%+v), want only the one Pilot reported", kiro, events)
	}
}
