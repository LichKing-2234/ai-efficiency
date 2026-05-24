package attributionlocal

import (
	"context"
	"encoding/json"
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

func TestSync_ManagedUploadSendsRepoConfigIDAndOmitsRawFields(t *testing.T) {
	fixture := buildAttributionFixture(t)
	client := &syncBackendClientStub{}
	engine := &SyncEngine{
		Scanner: NewScanner(),
		Client:  client,
	}

	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	if err := engine.Run(context.Background(), RunOptions{
		WorkspaceRoot: fixture.WorkspaceRoot,
		WorkspaceID:   workspaceID,
		ServerURL:     "https://ae.example.com",
		AuthSubject:   "user:123",
		RepoConfigID:  123,
		RepoKey:       "github.com/acme/repo",
		DurableReplay: true,
		ManagedUpload: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	req := client.requests[0]
	if req.RepoConfigID != 123 {
		t.Fatalf("repo_config_id = %d, want 123", req.RepoConfigID)
	}
	if req.RawSourcePath != "" || req.RawSourceLocator != "" || req.RawPayload != nil {
		t.Fatalf("managed upload leaked raw fields: %+v", req)
	}
}

func TestSync_RunSkipsSpooledEventsFromDifferentBinding(t *testing.T) {
	fixture := buildAttributionFixture(t)
	client := &syncBackendClientStub{}
	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	spoolPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "spool.json")
	if err := SaveJSON(spoolPath, []LocalToolUsageEvent{
		{
			Tool:            "codex",
			WorkspaceID:     workspaceID,
			ServerURL:       "https://ae.example.com",
			AuthSubject:     "user:123",
			RepoConfigID:    123,
			RepoKey:         "github.com/acme/repo",
			ManagedUpload:   true,
			ToolSessionID:   "conv-1",
			ToolEventID:     "stale",
			DedupeKey:       "stale-binding",
			UsageUnit:       UsageUnitToken,
			RequestCount:    1,
			ObservedStartAt: jsonTime("2026-05-13T10:00:00Z"),
			ObservedEndAt:   jsonTime("2026-05-13T10:00:01Z"),
		},
	}); err != nil {
		t.Fatalf("SaveJSON(spool): %v", err)
	}

	engine := &SyncEngine{
		Scanner: NewScanner(),
		Client:  client,
	}
	if err := engine.Run(context.Background(), RunOptions{
		WorkspaceRoot: fixture.WorkspaceRoot,
		WorkspaceID:   workspaceID,
		ServerURL:     "https://ae.example.com",
		AuthSubject:   "user:456",
		RepoConfigID:  456,
		RepoKey:       "github.com/acme/other",
		DurableReplay: true,
		ManagedUpload: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.SawUpload("stale-binding") {
		t.Fatalf("stale binding was uploaded: %+v", client.requests)
	}
	remaining, err := loadSpooledEvents(spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining spool = %+v, want stale mismatched event dropped", remaining)
	}
}

func TestSync_RunWritesSkippedLedgerForMismatchedSpooledEvents(t *testing.T) {
	fixture := buildAttributionFixture(t)
	client := &syncBackendClientStub{}
	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	spoolPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "spool.json")
	if err := SaveJSON(spoolPath, []LocalToolUsageEvent{
		{
			Tool:            "codex",
			WorkspaceID:     workspaceID,
			ServerURL:       "https://ae.example.com",
			AuthSubject:     "user:123",
			RepoConfigID:    123,
			RepoKey:         "github.com/acme/repo",
			ManagedUpload:   true,
			ToolSessionID:   "conv-1",
			ToolEventID:     "stale",
			DedupeKey:       "stale-binding",
			UsageUnit:       UsageUnitToken,
			RequestCount:    1,
			ObservedStartAt: jsonTime("2026-05-13T10:00:00Z"),
			ObservedEndAt:   jsonTime("2026-05-13T10:00:01Z"),
		},
	}); err != nil {
		t.Fatalf("SaveJSON(spool): %v", err)
	}

	engine := &SyncEngine{
		Scanner: NewScanner(),
		Client:  client,
	}
	if err := engine.Run(context.Background(), RunOptions{
		WorkspaceRoot: fixture.WorkspaceRoot,
		WorkspaceID:   workspaceID,
		ServerURL:     "https://ae.example.com",
		AuthSubject:   "user:456",
		RepoConfigID:  456,
		RepoKey:       "github.com/acme/other",
		DurableReplay: true,
		ManagedUpload: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	ledgerPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "upload-ledger.jsonl")
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read upload ledger: %v", err)
	}
	var rec struct {
		Kind      string `json:"kind"`
		DedupeKey string `json:"dedupe_key"`
		Status    string `json:"status"`
		LastError string `json:"last_error"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse ledger: %v", err)
	}
	if rec.Kind != "tool_usage" || rec.DedupeKey != "stale-binding" || rec.Status != "skipped" || rec.LastError != "context mismatch" {
		t.Fatalf("ledger = %+v, want skipped tool_usage context mismatch", rec)
	}
}
