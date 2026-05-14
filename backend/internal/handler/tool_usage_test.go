package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type httpToolUsageScope struct {
	UserID       int
	RepoConfigID int
	WorkspaceID  string
}

type httpBindingFixture struct {
	WorkspaceID        string
	CheckpointID       int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

func seedToolUsageScopeHTTP(t *testing.T, env *fullTestEnv) httpToolUsageScope {
	t.Helper()

	ctx := context.Background()
	repoID := createFullTestRepo(t, env.client)
	userID := fullAdminUserID(t, env)
	env.client.Session.Create().
		SetRepoConfigID(repoID).
		SetBranch("main").
		SetUserID(userID).
		SetStartedAt(time.Unix(100, 0).UTC()).
		SaveX(ctx)
	env.client.CommitCheckpoint.Create().
		SetEventID("cp-http-seed-scope").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repoID).
		SetCommitSha("seed-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(time.Unix(110, 0).UTC()).
		SaveX(ctx)

	return httpToolUsageScope{
		UserID:       userID,
		RepoConfigID: repoID,
		WorkspaceID:  "ws-1",
	}
}

func seedToolUsageBindingFixtureHTTP(t *testing.T, env *fullTestEnv) httpBindingFixture {
	t.Helper()

	ctx := context.Background()
	scope := seedToolUsageScopeHTTP(t, env)

	prev := time.Unix(150, 0).UTC()
	commitAt := time.Unix(200, 0).UTC()

	checkpoint := env.client.CommitCheckpoint.Create().
		SetEventID("cp-http-1").
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("abc123").
		SetParentShas([]string{"parent-1"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(commitAt).
		SaveX(ctx)

	insert := func(dedupeKey string, endAt time.Time, bound bool) {
		create := env.client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID("codex-sess-1").
			SetToolEventID(dedupeKey).
			SetDedupeKey(dedupeKey).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetInputTokens(10).
			SetOutputTokens(5).
			SetObservedStartAt(endAt.Add(-1 * time.Second)).
			SetObservedEndAt(endAt)
		if bound {
			create.SetCommitCheckpointID(checkpoint.ID)
		}
		create.SaveX(ctx)
	}

	insert("evt-window-1", prev.Add(2*time.Second), false)
	insert("evt-window-2", commitAt.Add(-10*time.Second), false)
	insert("evt-before-window", prev.Add(-10*time.Second), false)
	insert("evt-already-bound", commitAt.Add(-4*time.Second), true)

	return httpBindingFixture{
		WorkspaceID:        scope.WorkspaceID,
		CheckpointID:       checkpoint.ID,
		CommitCapturedAt:   commitAt,
		PreviousCapturedAt: prev,
	}
}

func TestToolUsageHandler_CreateUsageEvent(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	scope := seedToolUsageScopeHTTP(t, env)

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events", map[string]any{
		"tool":               "claude",
		"workspace_id":       scope.WorkspaceID,
		"tool_session_id":    "claude-sess-1",
		"tool_event_id":      "msg-1",
		"dedupe_key":         "claude:claude-sess-1:msg-1",
		"usage_unit":         "token",
		"input_tokens":       11,
		"output_tokens":      7,
		"observed_start_at":  "2026-05-13T10:00:00Z",
		"observed_end_at":    "2026-05-13T10:00:01Z",
		"raw_source_path":    "/Users/admin/.claude/projects/x.jsonl",
		"raw_source_locator": "line:42",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	ev := env.client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ("claude:claude-sess-1:msg-1")).
		OnlyX(context.Background())
	if ev.Tool != "claude" {
		t.Fatalf("tool = %q, want claude", ev.Tool)
	}
}

func TestToolUsageHandler_BindUsageEvents(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedToolUsageBindingFixtureHTTP(t, env)

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events/bind", map[string]any{
		"workspace_id":         fixture.WorkspaceID,
		"commit_checkpoint_id": fixture.CheckpointID,
		"commit_captured_at":   fixture.CommitCapturedAt.Format(time.RFC3339),
		"previous_captured_at": fixture.PreviousCapturedAt.Format(time.RFC3339),
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}
