package attributionlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestSync_ReplayBackfillsObservedTimesForLegacySpooledEvents(t *testing.T) {
	t.Parallel()

	fixture := setupSyncEngineWithSpool(t)
	sourcePath := writeFile(t, "legacy.jsonl", "{}\n")
	want := time.Date(2026, 5, 19, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(sourcePath, want, want); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	payload := []LocalToolUsageEvent{{
		Tool:          "claude",
		WorkspaceID:   "ws-1",
		ToolSessionID: "claude-1",
		ToolEventID:   "msg-1",
		DedupeKey:     "legacy-zero-time",
		UsageUnit:     UsageUnitToken,
		RequestCount:  1,
		RawSourcePath: sourcePath,
		RawPayload: map[string]any{
			"timestamp": "2026-05-19T12:34:56Z",
		},
	}}
	if err := SaveJSON(fixture.Engine.spoolPath, payload); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	if err := fixture.Engine.Replay(context.Background(), "/tmp/repo"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(fixture.Client.requests) != 1 {
		t.Fatalf("upload count = %d, want 1", len(fixture.Client.requests))
	}
	req := fixture.Client.requests[0]
	if req.ObservedStartAt.IsZero() || req.ObservedEndAt.IsZero() {
		t.Fatalf("expected observed timestamps in upload, got %+v", req)
	}
	if !req.ObservedStartAt.Equal(want) || !req.ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want %s", req.ObservedStartAt, req.ObservedEndAt, want)
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

func TestSync_ReplayPersistsBackfilledObservedTimesOnFailure(t *testing.T) {
	t.Parallel()

	fixture := setupSyncEngineWithSpool(t)
	fixture.Client.failOn = "legacy-zero-time"

	sourcePath := writeFile(t, "legacy.jsonl", "{}\n")
	want := time.Date(2026, 5, 19, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(sourcePath, want, want); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	payload := []LocalToolUsageEvent{{
		Tool:          "claude",
		WorkspaceID:   "ws-1",
		ToolSessionID: "claude-1",
		ToolEventID:   "msg-1",
		DedupeKey:     "legacy-zero-time",
		UsageUnit:     UsageUnitToken,
		RequestCount:  1,
		RawSourcePath: sourcePath,
		RawPayload: map[string]any{
			"timestamp": "2026-05-19T12:34:56Z",
		},
	}}
	if err := SaveJSON(fixture.Engine.spoolPath, payload); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	err := fixture.Engine.Replay(context.Background(), "/tmp/repo")
	if err == nil {
		t.Fatal("expected replay failure")
	}

	remaining, err := loadSpooledEvents(fixture.Engine.spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining count = %d, want 1", len(remaining))
	}
	if remaining[0].ObservedStartAt.IsZero() || remaining[0].ObservedEndAt.IsZero() {
		t.Fatalf("expected observed timestamps to be persisted, got %+v", remaining[0])
	}
	if !remaining[0].ObservedStartAt.Equal(want) || !remaining[0].ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want %s", remaining[0].ObservedStartAt, remaining[0].ObservedEndAt, want)
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

	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	spoolPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "spool.json")
	spooled, err := loadSpooledEvents(spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(spooled) == 0 {
		t.Fatal("expected new events to be spooled when no backend client is configured")
	}
	if _, err := os.Stat(filepath.Join(AttributionRootDir(), "spool.json")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy global spool path to stay unused, stat err=%v", err)
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

	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	spoolPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "spool.json")
	spooled, err := loadSpooledEvents(spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(spooled) != 1 || spooled[0].DedupeKey != "codex-jsonl:sess-1:resp-1" {
		t.Fatalf("spooled = %+v, want the failed scanned event", spooled)
	}
	if _, err := os.Stat(filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "scan-state.json")); err != nil {
		t.Fatalf("expected scan state to be persisted after spooling, stat err=%v", err)
	}
}
