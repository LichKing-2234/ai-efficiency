package attributionlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSync_ReplaysSpooledEventsBeforeNewScan(t *testing.T) {
	t.Parallel()

	fixture := setupSyncEngineWithSpool(t)
	if err := fixture.Engine.Replay(context.Background(), "/tmp/repo"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !fixture.Client.SawUpload("spooled-dedupe-key") {
		t.Fatal("expected spooled event upload")
	}
}

func TestSync_ReplayDropsAlreadyUploadedPrefixOnFailure(t *testing.T) {
	t.Parallel()

	fixture := setupSyncEngineWithSpool(t)
	fixture.Client.failOn = "second-dedupe-key"

	payload := []LocalToolUsageEvent{
		{DedupeKey: "first-dedupe-key", Tool: "codex", UsageUnit: UsageUnitToken},
		{DedupeKey: "second-dedupe-key", Tool: "codex", UsageUnit: UsageUnitToken},
	}
	if err := SaveJSON(fixture.Engine.spoolPath, payload); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	err := fixture.Engine.Replay(context.Background(), "/tmp/repo")
	if err == nil {
		t.Fatal("expected replay failure")
	}
	if !fixture.Client.SawUpload("first-dedupe-key") {
		t.Fatal("expected first upload before failure")
	}

	remaining, err := loadSpooledEvents(fixture.Engine.spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(remaining) != 1 || remaining[0].DedupeKey != "second-dedupe-key" {
		t.Fatalf("remaining = %+v, want only second item", remaining)
	}
}

func TestSync_RunForWorkspaceWithoutClientSpoolsNewEvents(t *testing.T) {
	fixture := buildAttributionFixture(t)
	engine := &SyncEngine{
		Scanner: NewScanner(),
	}

	if err := engine.RunForWorkspace(context.Background(), fixture.WorkspaceRoot); err != nil {
		t.Fatalf("RunForWorkspace: %v", err)
	}

	spooled, err := loadSpooledEvents(filepath.Join(AttributionRootDir(), "spool.json"))
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(spooled) == 0 {
		t.Fatal("expected new events to be spooled when no backend client is configured")
	}
}

func TestSync_RunForWorkspaceSpoolsNewEventsWhenUploadFails(t *testing.T) {
	fixture := buildAttributionFixture(t)

	engine := &SyncEngine{
		Scanner: NewScanner(),
		Client: &syncBackendClientStub{
			failOn: "codex-jsonl:sess-1:resp-1",
		},
	}

	if err := engine.RunForWorkspace(context.Background(), fixture.WorkspaceRoot); err != nil {
		t.Fatalf("RunForWorkspace: %v", err)
	}

	spoolPath := filepath.Join(AttributionRootDir(), "spool.json")
	spooled, err := loadSpooledEvents(spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(spooled) != 1 || spooled[0].DedupeKey != "codex-jsonl:sess-1:resp-1" {
		t.Fatalf("spooled = %+v, want the failed scanned event", spooled)
	}
	if _, err := os.Stat(filepath.Join(AttributionRootDir(), "scan-state.json")); err != nil {
		t.Fatalf("expected scan state to be persisted after spooling, stat err=%v", err)
	}
}
