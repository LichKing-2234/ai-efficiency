package attributionlocal

import (
	"context"
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
