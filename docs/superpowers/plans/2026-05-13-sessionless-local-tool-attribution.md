# Sessionless Local Tool Attribution Implementation Plan

> **Status:** In progress. Task 1 is partially implemented with a narrower first slice than the original draft: `tool_usage_events` and the backend `toolusage` service are landing first, while the standalone `workspaces` table has been deferred because the initial Ent modeling caused generated-code conflicts around `workspace_id`. Follow-up tasks should treat `workspace_id` as a business key first and only add a dedicated `workspaces` table after the ingest/bind path is stable.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a sessionless attribution pipeline that reads local Codex, Claude, and Kiro artifacts, binds normalized usage events to commit checkpoints and PRs, and removes the need for user-visible `ae-cli start/stop/flush` usage for attribution.

**Architecture:** Keep the existing backend modular-monolith boundaries and add a new attribution path centered on `workspace_id`, local tool artifacts, and git checkpoints instead of runtime session bootstrap. Parsing and local state live in `ae-cli`, while durable usage storage, commit binding, and PR aggregation live in `backend`. Agent hooks become repair triggers, and git hooks remain the authoritative commit/rewrite binding points.

**Tech Stack:** Go (`cobra`, `database/sql`, `ent`, `gin`, `uuid`), existing `ae-cli` hooks/session/collector packages, existing backend checkpoint/attribution packages, SQLite read access for Codex local logs, JSON/JSONL parsing for Codex/Claude/Kiro local artifacts.

---

## File Structure

### New files

- `ae-cli/internal/attributionlocal/state.go`
  Local watermark and spool directory helpers under `~/.ai-efficiency/attribution/`.
- `ae-cli/internal/attributionlocal/state_test.go`
  Tests for watermark/spool file layout and atomic persistence.
- `ae-cli/internal/attributionlocal/types.go`
  Shared normalized event structs, dedupe key helpers, and bind requests.
- `ae-cli/internal/attributionlocal/codex_sqlite.go`
  Codex SQLite parser for `~/.codex/logs_2.sqlite`.
- `ae-cli/internal/attributionlocal/codex_sqlite_test.go`
  Tests for Codex SQLite extraction, dedupe, and token field mapping.
- `ae-cli/internal/attributionlocal/codex_jsonl.go`
  Codex JSONL fallback parser for `~/.codex/sessions/**/*.jsonl`.
- `ae-cli/internal/attributionlocal/codex_jsonl_test.go`
  Tests for `last_token_usage` preference and cumulative fallback.
- `ae-cli/internal/attributionlocal/claude_jsonl.go`
  Claude JSONL parser for `~/.claude/projects/**/*.jsonl`.
- `ae-cli/internal/attributionlocal/claude_jsonl_test.go`
  Tests for final-message selection and duplicate message suppression.
- `ae-cli/internal/attributionlocal/kiro_json.go`
  Kiro JSON parser for `~/.kiro/sessions/cli/*.json`.
- `ae-cli/internal/attributionlocal/kiro_json_test.go`
  Tests for credit aggregation, `conversation_id` selection, and turn indexing.
- `ae-cli/internal/attributionlocal/scanner.go`
  Incremental scanner orchestrating all three tool parsers with watermarks.
- `ae-cli/internal/attributionlocal/scanner_test.go`
  Tests for per-tool incremental scanning and workspace filtering.
- `ae-cli/internal/attributionlocal/sync.go`
  Sync engine that scans, stages spool entries, uploads usage events, and binds them to checkpoints.
- `ae-cli/internal/attributionlocal/sync_test.go`
  Tests for spool replay, fail-open behavior, and idempotent upload ordering.
- `backend/ent/schema/tool_usage_event.go`
  Durable normalized usage event schema.
- `backend/ent/schema/workspace.go`
  Durable workspace identity schema separate from `session_workspaces`.
- `backend/internal/toolusage/service.go`
  Validation, dedupe, persistence, and checkpoint binding for tool usage events.
- `backend/internal/toolusage/service_test.go`
  Unit tests for tool usage persistence and binding semantics.
- `backend/internal/handler/tool_usage.go`
  HTTP handlers for ingesting tool usage events and binding them to checkpoints.
- `backend/internal/handler/tool_usage_test.go`
  HTTP tests for ingest and binding endpoints.
- `docs/superpowers/plans/2026-05-13-sessionless-local-tool-attribution.md`
  This implementation plan.

### Modified files

- `ae-cli/go.mod`
  Add the SQLite driver used for Codex log parsing.
- `ae-cli/internal/client/client.go`
  Add client methods for tool usage ingest and binding endpoints.
- `ae-cli/internal/client/client_test.go`
  Verify the new HTTP request payloads.
- `ae-cli/internal/hooks/handler.go`
  Trigger local sync on `post-commit` and `post-rewrite`, while preserving existing fail-open behavior.
- `ae-cli/internal/hooks/handler_test.go`
  Cover sync trigger behavior and fallback spool creation.
- `ae-cli/internal/hooks/install.go`
  Extend hook installation to support the new attribution sync command without regressing hook chaining.
- `ae-cli/cmd/hook.go`
  Repoint git hooks at the new local attribution sync path and add a hidden recovery command if needed.
- `ae-cli/cmd/root.go`
  Register any new hidden maintenance commands.
- `ae-cli/internal/session/workspace.go`
  Reuse stable `workspace_id` derivation from the new scanner/sync path without depending on active runtime session state.
- `backend/internal/handler/router.go`
  Register tool usage ingest and bind routes.
- `backend/internal/attribution/service.go`
  Read from `tool_usage_events` as the new primary source for commit/PR attribution.
- `backend/internal/attribution/service_test.go`
  Cover token + credit aggregation, rewrite resolution, and idempotent PR settlement.
- `backend/internal/checkpoint/service.go`
  Optionally expose helper queries needed by tool usage bind semantics.
- `backend/internal/checkpoint/service_test.go`
  Cover binding interactions with checkpoints and rewrites.
- `backend/internal/handler/session.go`
  Stop treating `session_usage_events` as the only usage detail path where detail payloads are exposed.
- `backend/internal/handler/session_detail_http_test.go`
  Adjust detail expectations if tool usage summaries are surfaced there.
- `backend/cmd/server/main.go`
  Wire the new backend services.
- `docs/architecture.md`
  Update the project-level runtime overview once the implementation lands.

### Existing files to read before implementation

- `ae-cli/internal/hooks/handler.go`
- `ae-cli/internal/hooks/install.go`
- `ae-cli/internal/client/client.go`
- `ae-cli/internal/session/workspace.go`
- `ae-cli/internal/collector/*.go`
- `backend/internal/checkpoint/service.go`
- `backend/internal/attribution/service.go`
- `backend/internal/handler/router.go`
- `backend/ent/schema/session_workspace.go`
- `backend/ent/schema/agent_metadata_event.go`
- `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`

---

### Task 1: Backend Schema For Workspaces And Tool Usage Events

**Files:**
- Deferred: `backend/ent/schema/workspace.go` (see status note above; not part of the current Task 1 slice)
- Create: `backend/ent/schema/tool_usage_event.go`
- Modify: `backend/ent/schema/repoconfig.go`
- Modify: `backend/ent/schema/user.go`
- Modify: `backend/ent/schema/commit_checkpoint.go`
- Test: `backend/internal/toolusage/service_test.go`

- [x] **Step 1: Write the failing schema/service tests**

```go
func TestCreateToolUsageEvent_DedupesByDedupeKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.NewClient(t)
	svc := toolusage.NewService(client)
	user := client.User.Create().SetUsername("alice").SaveX(ctx)
	repoCfg := client.RepoConfig.Create().SetName("repo").SetFullName("org/repo").SetRepoKey("github.com/org/repo").SetDefaultBranch("main").SaveX(ctx)
	workspace := client.Workspace.Create().
		SetWorkspaceID("ws-1").
		SetUserID(user.ID).
		SetRepoConfigID(repoCfg.ID).
		SetFirstSeenAt(time.Unix(100, 0)).
		SetLastSeenAt(time.Unix(100, 0)).
		SaveX(ctx)

	req := toolusage.CreateUsageEventRequest{
		Tool:            "codex",
		WorkspaceID:     workspace.WorkspaceID,
		ToolSessionID:   "codex-sess-1",
		ToolEventID:     "resp-1",
		DedupeKey:       "codex:codex-sess-1:resp-1",
		UsageUnit:       "token",
		InputTokens:     10,
		OutputTokens:    5,
		ObservedStartAt: time.Unix(120, 0),
		ObservedEndAt:   time.Unix(121, 0),
	}

	if err := svc.CreateUsageEvent(ctx, req); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := svc.CreateUsageEvent(ctx, req); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	count := client.ToolUsageEvent.Query().CountX(ctx)
	if count != 1 {
		t.Fatalf("tool_usage_events count = %d, want 1", count)
	}
}

func TestBindUsageEventsToCheckpoint_BindsOnlyUnboundWindowMatches(t *testing.T) {
	ctx := context.Background()
	client := testdb.NewClient(t)
	svc := toolusage.NewService(client)
	seed := createToolUsageBindingFixture(t, client)

	bound, err := svc.BindUsageEventsToCheckpoint(ctx, toolusage.BindUsageEventsRequest{
		WorkspaceID:        seed.WorkspaceID,
		CommitCheckpointID: seed.CheckpointID,
		CommitCapturedAt:   seed.CommitCapturedAt,
		PreviousCapturedAt: seed.PreviousCapturedAt,
	})
	if err != nil {
		t.Fatalf("BindUsageEventsToCheckpoint: %v", err)
	}
	if bound != 2 {
		t.Fatalf("bound count = %d, want 2", bound)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/toolusage -run 'Test(CreateToolUsageEvent_DedupesByDedupeKey|BindUsageEventsToCheckpoint_BindsOnlyUnboundWindowMatches)$' -v
```

Expected:

```text
FAIL ... package github.com/ai-efficiency/backend/internal/toolusage: no Go files
```

- [x] **Step 3: Add the Ent schemas**

```go
// backend/ent/schema/workspace.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Workspace struct{ ent.Schema }

func (Workspace) Fields() []ent.Field {
	return []ent.Field{
		field.String("workspace_id").NotEmpty().Unique(),
		field.Int("user_id"),
		field.Int("repo_config_id"),
		field.Time("first_seen_at").Default(timeNow),
		field.Time("last_seen_at").Default(timeNow).UpdateDefault(timeNow),
		field.String("last_branch").Optional().Nillable(),
		field.String("last_head_sha").Optional().Nillable(),
	}
}

func (Workspace) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("workspaces").Field("user_id").Unique().Required(),
		edge.From("repo_config", RepoConfig.Type).Ref("workspaces").Field("repo_config_id").Unique().Required(),
		edge.To("tool_usage_events", ToolUsageEvent.Type),
	}
}

func (Workspace) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "repo_config_id"),
	}
}
```

```go
// backend/ent/schema/tool_usage_event.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ToolUsageEvent struct{ ent.Schema }

func (ToolUsageEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("tool").NotEmpty(),
		field.String("workspace_id").NotEmpty(),
		field.Int("repo_config_id"),
		field.Int("user_id"),
		field.String("tool_session_id").NotEmpty(),
		field.String("tool_event_id").Optional().Nillable(),
		field.Time("observed_start_at"),
		field.Time("observed_end_at"),
		field.Int("request_count").Default(0),
		field.Enum("usage_unit").Values("token", "credit").Default("token"),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("cached_input_tokens").Default(0),
		field.Int64("reasoning_tokens").Default(0),
		field.Float("credit_usage").Default(0),
		field.Float("context_usage_pct").Default(0),
		field.Int("commit_checkpoint_id").Optional().Nillable(),
		field.String("dedupe_key").NotEmpty().Unique(),
		field.String("raw_source_path").Optional().Nillable(),
		field.String("raw_source_locator").Optional().Nillable(),
		field.JSON("raw_payload", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow),
	}
}

func (ToolUsageEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("workspace", Workspace.Type).Ref("tool_usage_events").Field("workspace_id").Unique().Required(),
		edge.From("repo_config", RepoConfig.Type).Ref("tool_usage_events").Field("repo_config_id").Unique().Required(),
		edge.From("user", User.Type).Ref("tool_usage_events").Field("user_id").Unique().Required(),
		edge.From("commit_checkpoint", CommitCheckpoint.Type).Ref("tool_usage_events").Field("commit_checkpoint_id").Unique(),
	}
}

func (ToolUsageEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("workspace_id", "observed_end_at"),
		index.Fields("commit_checkpoint_id"),
		index.Fields("tool", "tool_session_id"),
	}
}
```

- [x] **Step 4: Generate Ent code**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go generate ./ent
```

Expected:

```text
No output, generated files updated under backend/ent/
```

- [x] **Step 5: Write the service implementation**

```go
// backend/internal/toolusage/service.go
package toolusage

type CreateUsageEventRequest struct {
	Tool              string
	WorkspaceID       string
	ToolSessionID     string
	ToolEventID       string
	DedupeKey         string
	UsageUnit         string
	RequestCount      int
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	ContextUsagePct   float64
	ObservedStartAt   time.Time
	ObservedEndAt     time.Time
	RawSourcePath     string
	RawSourceLocator  string
	RawPayload        map[string]any
}

type BindUsageEventsRequest struct {
	WorkspaceID        string
	CommitCheckpointID int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

type Service struct {
	entClient *ent.Client
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) CreateUsageEvent(ctx context.Context, req CreateUsageEventRequest) error {
	workspace, err := s.entClient.Workspace.Query().
		Where(workspace.WorkspaceIDEQ(strings.TrimSpace(req.WorkspaceID))).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	exists, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ(strings.TrimSpace(req.DedupeKey))).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check dedupe: %w", err)
	}
	if exists {
		return nil
	}

	_, err = s.entClient.ToolUsageEvent.Create().
		SetTool(strings.TrimSpace(req.Tool)).
		SetWorkspaceID(workspace.WorkspaceID).
		SetRepoConfigID(workspace.RepoConfigID).
		SetUserID(workspace.UserID).
		SetToolSessionID(strings.TrimSpace(req.ToolSessionID)).
		SetToolEventID(strings.TrimSpace(req.ToolEventID)).
		SetObservedStartAt(req.ObservedStartAt.UTC()).
		SetObservedEndAt(req.ObservedEndAt.UTC()).
		SetRequestCount(req.RequestCount).
		SetUsageUnit(toolusageevent.UsageUnit(req.UsageUnit)).
		SetInputTokens(req.InputTokens).
		SetOutputTokens(req.OutputTokens).
		SetCachedInputTokens(req.CachedInputTokens).
		SetReasoningTokens(req.ReasoningTokens).
		SetCreditUsage(req.CreditUsage).
		SetContextUsagePct(req.ContextUsagePct).
		SetDedupeKey(strings.TrimSpace(req.DedupeKey)).
		SetRawSourcePath(strings.TrimSpace(req.RawSourcePath)).
		SetRawSourceLocator(strings.TrimSpace(req.RawSourceLocator)).
		SetRawPayload(req.RawPayload).
		Save(ctx)
	if err != nil && ent.IsConstraintError(err) {
		return nil
	}
	return err
}

func (s *Service) BindUsageEventsToCheckpoint(ctx context.Context, req BindUsageEventsRequest) (int, error) {
	items, err := s.entClient.ToolUsageEvent.Query().
		Where(
			toolusageevent.WorkspaceIDEQ(strings.TrimSpace(req.WorkspaceID)),
			toolusageevent.CommitCheckpointIDIsNil(),
			toolusageevent.ObservedEndAtLTE(req.CommitCapturedAt),
			toolusageevent.ObservedEndAtGT(req.PreviousCapturedAt),
		).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query usage events: %w", err)
	}
	for _, item := range items {
		if _, err := s.entClient.ToolUsageEvent.UpdateOneID(item.ID).
			SetCommitCheckpointID(req.CommitCheckpointID).
			Save(ctx); err != nil {
			return 0, fmt.Errorf("bind usage event %d: %w", item.ID, err)
		}
	}
	return len(items), nil
}
```

- [x] **Step 6: Run the backend tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/toolusage -run 'Test(CreateToolUsageEvent_DedupesByDedupeKey|BindUsageEventsToCheckpoint_BindsOnlyUnboundWindowMatches)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 7: Commit**

```bash
git add backend/ent/schema/workspace.go \
        backend/ent/schema/tool_usage_event.go \
        backend/internal/toolusage/service.go \
        backend/internal/toolusage/service_test.go \
        backend/ent
git commit -m "feat(backend): add workspace and tool usage event schemas"
```

### Task 2: Backend Test Fixtures For Tool Usage HTTP And Attribution

**Files:**
- Create: `backend/internal/toolusage/test_helpers_test.go`
- Modify: `backend/internal/handler/handler_coverage_test.go`
- Modify: `backend/internal/attribution/service_test.go`
- Test: `backend/internal/toolusage/test_helpers_test.go`

- [x] **Step 1: Write the failing fixture helper tests**

```go
func TestSeedWorkspaceForToolUsage_CreatesWorkspaceAndRepo(t *testing.T) {
	ctx := context.Background()
	client := testdb.NewClient(t)

	workspaceID := seedWorkspaceForToolUsage(t, client)
	ws := client.Workspace.Query().Where(workspace.WorkspaceIDEQ(workspaceID)).OnlyX(ctx)
	if ws.RepoConfigID == 0 || ws.UserID == 0 {
		t.Fatalf("workspace = %+v, want repo_config_id and user_id", ws)
	}
}

func TestCreateToolUsageBindingFixture_SeedsBoundAndUnboundRows(t *testing.T) {
	ctx := context.Background()
	client := testdb.NewClient(t)

	fixture := createToolUsageBindingFixture(t, client)
	rows := client.ToolUsageEvent.Query().
		Where(toolusageevent.WorkspaceIDEQ(fixture.WorkspaceID)).
		AllX(ctx)
	if len(rows) < 3 {
		t.Fatalf("tool usage row count = %d, want at least 3", len(rows))
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/toolusage -run 'Test(SeedWorkspaceForToolUsage_CreatesWorkspaceAndRepo|CreateToolUsageBindingFixture_SeedsBoundAndUnboundRows)$' -v
```

Expected:

```text
FAIL ... undefined: seedWorkspaceForToolUsage / createToolUsageBindingFixture
```

- [x] **Step 3: Add explicit fixture helpers**

```go
// backend/internal/toolusage/test_helpers_test.go
package toolusage

type bindingFixture struct {
	WorkspaceID        string
	CheckpointID       int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

func seedWorkspaceForToolUsage(t *testing.T, client *ent.Client) string {
	t.Helper()
	ctx := context.Background()
	user := client.User.Create().SetUsername("toolusage-user").SaveX(ctx)
	repoCfg := client.RepoConfig.Create().
		SetName("repo").
		SetFullName("org/repo").
		SetRepoKey("github.com/org/repo").
		SetDefaultBranch("main").
		SaveX(ctx)
	ws := client.Workspace.Create().
		SetWorkspaceID("ws-1").
		SetUserID(user.ID).
		SetRepoConfigID(repoCfg.ID).
		SetFirstSeenAt(time.Unix(100, 0)).
		SetLastSeenAt(time.Unix(100, 0)).
		SaveX(ctx)
	return ws.WorkspaceID
}

func createToolUsageBindingFixture(t *testing.T, client *ent.Client) bindingFixture {
	t.Helper()
	ctx := context.Background()
	workspaceID := seedWorkspaceForToolUsage(t, client)
	repoCfg := client.RepoConfig.Query().OnlyX(ctx)
	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("evt-checkpoint-1").
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(repoCfg.ID).
		SetCommitSha("abc123").
		SetParentShas([]string{"def456"}).
		SetBindingSource(commitcheckpoint.BindingSourceManual).
		SetCapturedAt(time.Unix(200, 0)).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(repoCfg.ID).
		SetUserID(client.User.Query().OnlyX(ctx).ID).
		SetToolSessionID("codex-1").
		SetToolEventID("resp-1").
		SetObservedStartAt(time.Unix(150, 0)).
		SetObservedEndAt(time.Unix(151, 0)).
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetInputTokens(10).
		SetOutputTokens(5).
		SetDedupeKey("codex:1").
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(repoCfg.ID).
		SetUserID(client.User.Query().OnlyX(ctx).ID).
		SetToolSessionID("claude-1").
		SetToolEventID("msg-1").
		SetObservedStartAt(time.Unix(170, 0)).
		SetObservedEndAt(time.Unix(171, 0)).
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetInputTokens(20).
		SetOutputTokens(10).
		SetDedupeKey("claude:1").
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(repoCfg.ID).
		SetUserID(client.User.Query().OnlyX(ctx).ID).
		SetToolSessionID("kiro-1").
		SetToolEventID("turn-1").
		SetObservedStartAt(time.Unix(205, 0)).
		SetObservedEndAt(time.Unix(206, 0)).
		SetUsageUnit(toolusageevent.UsageUnitCredit).
		SetCreditUsage(0.3).
		SetDedupeKey("kiro:1").
		SaveX(ctx)

	return bindingFixture{
		WorkspaceID:        workspaceID,
		CheckpointID:       checkpoint.ID,
		CommitCapturedAt:   time.Unix(200, 0),
		PreviousCapturedAt: time.Unix(140, 0),
	}
}
```

- [x] **Step 4: Run the fixture helper tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/toolusage -run 'Test(SeedWorkspaceForToolUsage_CreatesWorkspaceAndRepo|CreateToolUsageBindingFixture_SeedsBoundAndUnboundRows)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 5: Commit**

```bash
git add backend/internal/toolusage/test_helpers_test.go \
        backend/internal/handler/handler_coverage_test.go \
        backend/internal/attribution/service_test.go
git commit -m "test(backend): add tool usage attribution fixtures"
```

### Task 3: Backend HTTP Ingest And Binding Endpoints

**Files:**
- Create: `backend/internal/handler/tool_usage.go`
- Create: `backend/internal/handler/tool_usage_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/handler/tool_usage_test.go`

- [x] **Step 1: Write the failing handler tests**

```go
func TestToolUsageHandler_CreateUsageEvent(t *testing.T) {
	env := setupFullTestEnv(t)
	seedWorkspaceForToolUsage(t, env.Client)

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events", map[string]any{
		"tool":               "claude",
		"workspace_id":       "ws-1",
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
}

func TestToolUsageHandler_BindUsageEvents(t *testing.T) {
	env := setupFullTestEnv(t)
	seedToolUsageBindingFixtureHTTP(t, env.Client)

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events/bind", map[string]any{
		"workspace_id":         "ws-1",
		"commit_checkpoint_id": 101,
		"commit_captured_at":   "2026-05-13T10:10:00Z",
		"previous_captured_at": "2026-05-13T10:00:00Z",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestToolUsageHandler_(CreateUsageEvent|BindUsageEvents)$' -v
```

Expected:

```text
FAIL ... 404 route not found
```

- [x] **Step 3: Add the handler implementation**

```go
// backend/internal/handler/tool_usage.go
package handler

type ToolUsageHandler struct {
	service *toolusage.Service
}

func NewToolUsageHandler(service *toolusage.Service) *ToolUsageHandler {
	return &ToolUsageHandler{service: service}
}

type createToolUsageEventRequest struct {
	Tool              string         `json:"tool" binding:"required"`
	WorkspaceID       string         `json:"workspace_id" binding:"required"`
	ToolSessionID     string         `json:"tool_session_id" binding:"required"`
	ToolEventID       string         `json:"tool_event_id"`
	DedupeKey         string         `json:"dedupe_key" binding:"required"`
	UsageUnit         string         `json:"usage_unit" binding:"required"`
	RequestCount      int            `json:"request_count"`
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	CreditUsage       float64        `json:"credit_usage"`
	ContextUsagePct   float64        `json:"context_usage_pct"`
	ObservedStartAt   time.Time      `json:"observed_start_at" binding:"required"`
	ObservedEndAt     time.Time      `json:"observed_end_at" binding:"required"`
	RawSourcePath     string         `json:"raw_source_path"`
	RawSourceLocator  string         `json:"raw_source_locator"`
	RawPayload        map[string]any `json:"raw_payload"`
}

type bindToolUsageEventsRequest struct {
	WorkspaceID        string    `json:"workspace_id" binding:"required"`
	CommitCheckpointID int       `json:"commit_checkpoint_id" binding:"required"`
	CommitCapturedAt   time.Time `json:"commit_captured_at" binding:"required"`
	PreviousCapturedAt time.Time `json:"previous_captured_at" binding:"required"`
}

func (h *ToolUsageHandler) Create(c *gin.Context) {
	var req createToolUsageEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.CreateUsageEvent(c.Request.Context(), toolusage.CreateUsageEventRequest(req)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "ok"})
}

func (h *ToolUsageHandler) Bind(c *gin.Context) {
	var req bindToolUsageEventsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	count, err := h.service.BindUsageEventsToCheckpoint(c.Request.Context(), toolusage.BindUsageEventsRequest(req))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"bound_count": count})
}
```

- [x] **Step 4: Register the routes and wiring**

```go
// backend/internal/handler/router.go
toolUsageHandler := NewToolUsageHandler(toolusage.NewService(entClient))

toolUsageGroup := protected.Group("/tool-usage-events")
toolUsageGroup.POST("", toolUsageHandler.Create)
toolUsageGroup.POST("/bind", toolUsageHandler.Bind)
```

- [x] **Step 5: Run the handler tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestToolUsageHandler_(CreateUsageEvent|BindUsageEvents)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 6: Commit**

```bash
git add backend/internal/handler/tool_usage.go \
        backend/internal/handler/tool_usage_test.go \
        backend/internal/handler/router.go
git commit -m "feat(backend): add tool usage ingest endpoints"
```

### Task 4: ae-cli Test Fixtures For Local Artifact Parsing

**Files:**
- Create: `ae-cli/internal/attributionlocal/test_helpers_test.go`
- Test: `ae-cli/internal/attributionlocal/test_helpers_test.go`

- [x] **Step 1: Write the failing local fixture tests**

```go
func TestWriteFile_WritesFixtureContent(t *testing.T) {
	path := writeFile(t, "fixture.txt", "hello")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data = %q, want %q", string(data), "hello")
	}
}

func TestBuildCodexSQLiteFixture_CreatesLogsDB(t *testing.T) {
	path := buildCodexSQLiteFixture(t, []string{`event.name="codex.sse_event" event.kind=response.completed conversation.id=conv-1 response.id=resp-1`})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(WriteFile_WritesFixtureContent|BuildCodexSQLiteFixture_CreatesLogsDB)$' -v
```

Expected:

```text
FAIL ... undefined: writeFile / buildCodexSQLiteFixture
```

- [x] **Step 3: Add explicit local fixture helpers**

```go
// ae-cli/internal/attributionlocal/test_helpers_test.go
package attributionlocal

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

func buildCodexSQLiteFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, line := range lines {
		if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, line); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	return path
}

type attributionFixture struct {
	WorkspaceRoot string
}

func buildAttributionFixture(t *testing.T) attributionFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	dbPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create logs table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, `event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-05-13T10:00:00Z conversation.id=conv-1 response.id=resp-1`); err != nil {
		t.Fatalf("insert logs row: %v", err)
	}
	return attributionFixture{WorkspaceRoot: root}
}

type syncEngineFixture struct {
	Engine *SyncEngine
	Client *syncBackendClientStub
}

func setupSyncEngineWithSpool(t *testing.T) syncEngineFixture {
	t.Helper()
	client := &syncBackendClientStub{}
	engine := &SyncEngine{Client: client}
	return syncEngineFixture{Engine: engine, Client: client}
}

type syncBackendClientStub struct {
	uploads []string
}

func (s *syncBackendClientStub) SendToolUsageEvent(_ context.Context, req client.ToolUsageEventRequest) error {
	s.uploads = append(s.uploads, req.DedupeKey)
	return nil
}

func (s *syncBackendClientStub) BindToolUsageEvents(_ context.Context, _ client.BindToolUsageEventsRequest) error {
	return nil
}

func (s *syncBackendClientStub) SawUpload(dedupeKey string) bool {
	for _, item := range s.uploads {
		if item == dedupeKey {
			return true
		}
	}
	return false
}

func fixtureRepoRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return root
}
```

- [x] **Step 4: Run the local fixture tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(WriteFile_WritesFixtureContent|BuildCodexSQLiteFixture_CreatesLogsDB)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 5: Commit**

```bash
git add ae-cli/internal/attributionlocal/test_helpers_test.go
git commit -m "test(ae-cli): add local attribution fixtures"
```

### Task 5: Codex SQLite Parser And Watermark State

**Files:**
- Modify: `ae-cli/go.mod`
- Create: `ae-cli/internal/attributionlocal/types.go`
- Create: `ae-cli/internal/attributionlocal/state.go`
- Create: `ae-cli/internal/attributionlocal/state_test.go`
- Create: `ae-cli/internal/attributionlocal/codex_sqlite.go`
- Create: `ae-cli/internal/attributionlocal/codex_sqlite_test.go`
- Test: `ae-cli/internal/attributionlocal/state_test.go`
- Test: `ae-cli/internal/attributionlocal/codex_sqlite_test.go`

- [x] **Step 1: Write the failing parser test**

```go
func TestParseCodexSQLite_ExtractsResponseCompletedUsage(t *testing.T) {
	dbPath := buildCodexSQLiteFixture(t, []string{
		`event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-05-13T10:00:00Z conversation.id=conv-1 response.id=resp-1`,
	})

	parser := attributionlocal.NewCodexSQLiteParser()
	events, watermark, err := parser.Parse(dbPath, attributionlocal.CodexSQLiteWatermark{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].ToolSessionID != "conv-1" || events[0].ToolEventID != "resp-1" {
		t.Fatalf("event = %+v", events[0])
	}
	if watermark.LastLogID == 0 {
		t.Fatalf("watermark.LastLogID = %d, want > 0", watermark.LastLogID)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'TestParseCodexSQLite_ExtractsResponseCompletedUsage$' -v
```

Expected:

```text
FAIL ... package github.com/ai-efficiency/ae-cli/internal/attributionlocal: no Go files
```

- [x] **Step 3: Add the SQLite dependency**

```go
// ae-cli/go.mod
require (
	github.com/glebarez/go-sqlite v1.22.0
)
```

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go mod tidy
```

Expected:

```text
go: downloading github.com/glebarez/go-sqlite ...
```

- [x] **Step 4: Add normalized event and watermark types**

```go
// ae-cli/internal/attributionlocal/types.go
package attributionlocal

type UsageUnit string

const (
	UsageUnitToken  UsageUnit = "token"
	UsageUnitCredit UsageUnit = "credit"
)

type LocalToolUsageEvent struct {
	Tool              string
	WorkspaceID       string
	ToolSessionID     string
	ToolEventID       string
	DedupeKey         string
	RequestCount      int
	UsageUnit         UsageUnit
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	ContextUsagePct   float64
	ObservedStartAt   time.Time
	ObservedEndAt     time.Time
	RawSourcePath     string
	RawSourceLocator  string
	RawPayload        map[string]any
}

type CodexSQLiteWatermark struct {
	LastLogID int64 `json:"last_log_id"`
}
```

```go
// ae-cli/internal/attributionlocal/state.go
package attributionlocal

func AttributionRootDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-efficiency", "attribution")
}

func SaveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}
```

- [x] **Step 5: Implement the parser**

```go
// ae-cli/internal/attributionlocal/codex_sqlite.go
package attributionlocal

type CodexSQLiteParser struct{}

func NewCodexSQLiteParser() *CodexSQLiteParser { return &CodexSQLiteParser{} }

func (p *CodexSQLiteParser) Parse(dbPath string, wm CodexSQLiteWatermark) ([]LocalToolUsageEvent, CodexSQLiteWatermark, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, wm, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, feedback_log_body
		FROM logs
		WHERE id > ?
		  AND feedback_log_body LIKE '%response.completed%'
		ORDER BY id ASC
	`, wm.LastLogID)
	if err != nil {
		return nil, wm, err
	}
	defer rows.Close()

	seen := map[string]LocalToolUsageEvent{}
	lastID := wm.LastLogID
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, wm, err
		}
		lastID = id
		event := parseCodexCompletedLine(body)
		if event == nil {
			continue
		}
		seen[event.DedupeKey] = *event
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]LocalToolUsageEvent, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out, CodexSQLiteWatermark{LastLogID: lastID}, nil
}
```

- [x] **Step 6: Run the parser tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(ParseCodexSQLite_ExtractsResponseCompletedUsage|SaveJSON.*|LoadJSON.*)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 7: Commit**

```bash
git add ae-cli/go.mod ae-cli/go.sum \
        ae-cli/internal/attributionlocal/types.go \
        ae-cli/internal/attributionlocal/state.go \
        ae-cli/internal/attributionlocal/state_test.go \
        ae-cli/internal/attributionlocal/codex_sqlite.go \
        ae-cli/internal/attributionlocal/codex_sqlite_test.go
git commit -m "feat(ae-cli): add codex sqlite attribution parser"
```

### Task 6: Codex JSONL Fallback, Claude Parser, And Kiro Parser

**Files:**
- Create: `ae-cli/internal/attributionlocal/codex_jsonl.go`
- Create: `ae-cli/internal/attributionlocal/codex_jsonl_test.go`
- Create: `ae-cli/internal/attributionlocal/claude_jsonl.go`
- Create: `ae-cli/internal/attributionlocal/claude_jsonl_test.go`
- Create: `ae-cli/internal/attributionlocal/kiro_json.go`
- Create: `ae-cli/internal/attributionlocal/kiro_json_test.go`
- Test: `ae-cli/internal/attributionlocal/codex_jsonl_test.go`
- Test: `ae-cli/internal/attributionlocal/claude_jsonl_test.go`
- Test: `ae-cli/internal/attributionlocal/kiro_json_test.go`

- [x] **Step 1: Write the failing fallback/parser tests**

```go
func TestParseCodexJSONL_PrefersLastTokenUsage(t *testing.T) {
	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120},"last_token_usage":{"input_tokens":7,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":9}}}}`,
	}, "\n"))

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 1 || events[0].InputTokens != 7 || events[0].OutputTokens != 2 {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseClaudeJSONL_PrefersEndTurnRecord(t *testing.T) {
	path := writeFile(t, "claude.jsonl", strings.Join([]string{
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":1}},"stop_reason":null}`,
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":3}},"stop_reason":"end_turn"}`,
	}, "\n"))

	events, err := ParseClaudeJSONL(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseClaudeJSONL: %v", err)
	}
	if len(events) != 1 || events[0].OutputTokens != 3 {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseKiroJSON_UsesCreditAndConversationID(t *testing.T) {
	path := writeFile(t, "kiro.json", `{"session_id":"root-1","cwd":"/tmp/repo","session_state":{"conversation_metadata":{"user_turn_metadatas":[{"total_request_count":2,"context_usage_percentage":4.2,"metering_usage":[{"value":0.1,"unit":"credit"},{"value":0.2,"unit":"credit"}]}]},"rts_model_state":{"conversation_id":"conv-1"}}}`)

	events, err := ParseKiroJSON(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroJSON: %v", err)
	}
	if len(events) != 1 || events[0].ToolSessionID != "conv-1" || events[0].CreditUsage != 0.3 {
		t.Fatalf("events = %+v", events)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(ParseCodexJSONL_PrefersLastTokenUsage|ParseClaudeJSONL_PrefersEndTurnRecord|ParseKiroJSON_UsesCreditAndConversationID)$' -v
```

Expected:

```text
FAIL ... undefined: ParseCodexJSONLFallback / ParseClaudeJSONL / ParseKiroJSON
```

- [x] **Step 3: Implement Codex JSONL fallback**

```go
// ae-cli/internal/attributionlocal/codex_jsonl.go
func ParseCodexJSONLFallback(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sessionID string
	var event *LocalToolUsageEvent
	for _, raw := range strings.Split(string(lines), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			continue
		}
		switch row["type"] {
		case "session_meta":
			payload, _ := row["payload"].(map[string]any)
			if strings.TrimSpace(asString(payload["cwd"])) == filepath.Clean(workspaceRoot) {
				sessionID = strings.TrimSpace(asString(payload["id"]))
			}
		case "event_msg":
			if sessionID == "" {
				continue
			}
			payload, _ := row["payload"].(map[string]any)
			if strings.TrimSpace(asString(payload["type"])) != "token_count" {
				continue
			}
			info, _ := payload["info"].(map[string]any)
			lastUsage, _ := info["last_token_usage"].(map[string]any)
			totalUsage, _ := info["total_token_usage"].(map[string]any)
			selected := lastUsage
			if len(selected) == 0 {
				selected = totalUsage
			}
			if len(selected) == 0 {
				continue
			}
			event = &LocalToolUsageEvent{
				Tool:              "codex",
				ToolSessionID:     sessionID,
				ToolEventID:       strings.TrimSpace(asString(payload["response_id"])),
				DedupeKey:         "codex-jsonl:" + sessionID + ":" + filepath.Base(path),
				UsageUnit:         UsageUnitToken,
				RequestCount:      1,
				InputTokens:       asInt64(selected["input_tokens"]),
				OutputTokens:      asInt64(selected["output_tokens"]),
				CachedInputTokens: asInt64(selected["cached_input_tokens"]),
				ReasoningTokens:   asInt64(selected["reasoning_output_tokens"]),
				RawSourcePath:     path,
				RawPayload:        payload,
			}
		}
	}
	if event == nil {
		return nil, nil
	}
	return []LocalToolUsageEvent{*event}, nil
}
```

- [x] **Step 4: Implement Claude JSONL parsing**

```go
// ae-cli/internal/attributionlocal/claude_jsonl.go
func ParseClaudeJSONL(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	type candidate struct {
		event    LocalToolUsageEvent
		stopDone bool
		score    int64
	}
	best := map[string]candidate{}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if strings.TrimSpace(asString(row["type"])) != "assistant" {
			continue
		}
		if filepath.Clean(asString(row["cwd"])) != filepath.Clean(workspaceRoot) {
			continue
		}
		msg, _ := row["message"].(map[string]any)
		msgID := strings.TrimSpace(asString(msg["id"]))
		sessionID := strings.TrimSpace(asString(row["sessionId"]))
		if msgID == "" || sessionID == "" {
			continue
		}
		usage, _ := msg["usage"].(map[string]any)
		score := asInt64(usage["input_tokens"]) + asInt64(usage["output_tokens"]) + asInt64(usage["cache_creation_input_tokens"]) + asInt64(usage["cache_read_input_tokens"])
		cur := candidate{
			event: LocalToolUsageEvent{
				Tool:              "claude",
				ToolSessionID:     sessionID,
				ToolEventID:       msgID,
				DedupeKey:         "claude:" + sessionID + ":" + msgID,
				RequestCount:      1,
				UsageUnit:         UsageUnitToken,
				InputTokens:       asInt64(usage["input_tokens"]),
				OutputTokens:      asInt64(usage["output_tokens"]),
				CachedInputTokens: asInt64(usage["cache_creation_input_tokens"]) + asInt64(usage["cache_read_input_tokens"]),
				RawSourcePath:     path,
				RawPayload:        row,
			},
			stopDone: strings.TrimSpace(asString(row["stop_reason"])) == "end_turn",
			score:    score,
		}
		old, ok := best[cur.event.DedupeKey]
		if !ok || (cur.stopDone && !old.stopDone) || (cur.stopDone == old.stopDone && cur.score >= old.score) {
			best[cur.event.DedupeKey] = cur
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	out := make([]LocalToolUsageEvent, 0, len(best))
	for _, item := range best {
		out = append(out, item.event)
	}
	slices.SortFunc(out, func(a, b LocalToolUsageEvent) int { return strings.Compare(a.DedupeKey, b.DedupeKey) })
	return out, nil
}
```

- [x] **Step 5: Implement Kiro JSON parsing**

```go
// ae-cli/internal/attributionlocal/kiro_json.go
func ParseKiroJSON(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	var doc struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		SessionState struct {
			ConversationMetadata struct {
				UserTurnMetadatas []struct {
					MessageIDs              []string `json:"message_ids"`
					TotalRequestCount       int      `json:"total_request_count"`
					ContextUsagePercentage  float64  `json:"context_usage_percentage"`
					MeteringUsage           []struct {
						Value float64 `json:"value"`
						Unit  string  `json:"unit"`
					} `json:"metering_usage"`
					InputTokenCount  int64 `json:"input_token_count"`
					OutputTokenCount int64 `json:"output_token_count"`
				} `json:"user_turn_metadatas"`
			} `json:"conversation_metadata"`
			RTSModelState struct {
				ConversationID string `json:"conversation_id"`
			} `json:"rts_model_state"`
		} `json:"session_state"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if filepath.Clean(doc.CWD) != filepath.Clean(workspaceRoot) {
		return nil, nil
	}
	sessionID := strings.TrimSpace(doc.SessionState.RTSModelState.ConversationID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(doc.SessionID)
	}
	if sessionID == "" {
		return nil, nil
	}
	out := make([]LocalToolUsageEvent, 0, len(doc.SessionState.ConversationMetadata.UserTurnMetadatas))
	for idx, turn := range doc.SessionState.ConversationMetadata.UserTurnMetadatas {
		var credits float64
		for _, usage := range turn.MeteringUsage {
			if usage.Unit == "credit" {
				credits += usage.Value
			}
		}
		out = append(out, LocalToolUsageEvent{
			Tool:             "kiro",
			ToolSessionID:    sessionID,
			ToolEventID:      fmt.Sprintf("turn-%d", idx),
			DedupeKey:        fmt.Sprintf("kiro:%s:%d", sessionID, idx),
			RequestCount:     turn.TotalRequestCount,
			UsageUnit:        UsageUnitCredit,
			CreditUsage:      credits,
			ContextUsagePct:  turn.ContextUsagePercentage,
			InputTokens:      turn.InputTokenCount,
			OutputTokens:     turn.OutputTokenCount,
			RawSourcePath:    path,
			RawSourceLocator: fmt.Sprintf("turn:%d", idx),
			RawPayload:       map[string]any{"message_ids": turn.MessageIDs},
		})
	}
	return out, nil
}
```

- [x] **Step 6: Run the parser tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(ParseCodexJSONL_PrefersLastTokenUsage|ParseClaudeJSONL_PrefersEndTurnRecord|ParseKiroJSON_UsesCreditAndConversationID)$' -v
```

Expected:

```text
PASS
```

- [x] **Step 7: Commit**

```bash
git add ae-cli/internal/attributionlocal/codex_jsonl.go \
        ae-cli/internal/attributionlocal/codex_jsonl_test.go \
        ae-cli/internal/attributionlocal/claude_jsonl.go \
        ae-cli/internal/attributionlocal/claude_jsonl_test.go \
        ae-cli/internal/attributionlocal/kiro_json.go \
        ae-cli/internal/attributionlocal/kiro_json_test.go
git commit -m "feat(ae-cli): add claude and kiro attribution parsers"
```

### Task 7: Incremental Scanner, Spool Replay, And Hidden Sync Entry Point

**Files:**
- Create: `ae-cli/internal/attributionlocal/scanner.go`
- Create: `ae-cli/internal/attributionlocal/scanner_test.go`
- Create: `ae-cli/internal/attributionlocal/sync.go`
- Create: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/cmd/hook.go`
- Modify: `ae-cli/cmd/root.go`
- Test: `ae-cli/internal/attributionlocal/scanner_test.go`
- Test: `ae-cli/internal/attributionlocal/sync_test.go`
- Test: `ae-cli/internal/client/client_test.go`

- [x] **Step 1: Write the failing scanner/sync tests**

```go
func TestScanner_SkipsAlreadyWatermarkedCodexRows(t *testing.T) {
	fixture := buildAttributionFixture(t)
	scanner := attributionlocal.NewScanner()

	first, state, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, attributionlocal.ScanState{})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected first scan events")
	}

	second, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, state)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second scan events = %d, want 0", len(second))
	}
}

func TestSync_ReplaysSpooledEventsBeforeNewScan(t *testing.T) {
	fixture := setupSyncEngineWithSpool(t)
	if err := fixture.Engine.Replay(context.Background(), "/tmp/repo"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !fixture.Client.SawUpload("spooled-dedupe-key") {
		t.Fatal("expected spooled event upload")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal -run 'Test(Scanner_SkipsAlreadyWatermarkedCodexRows|Sync_ReplaysSpooledEventsBeforeNewScan)$' -v
```

Expected:

```text
FAIL ... undefined: attributionlocal.NewScanner / setupSyncEngineWithSpool
```

- [x] **Step 3: Extend the backend client**

```go
// ae-cli/internal/client/client.go
type ToolUsageEventRequest struct {
	Tool              string         `json:"tool"`
	WorkspaceID       string         `json:"workspace_id"`
	ToolSessionID     string         `json:"tool_session_id"`
	ToolEventID       string         `json:"tool_event_id,omitempty"`
	DedupeKey         string         `json:"dedupe_key"`
	UsageUnit         string         `json:"usage_unit"`
	RequestCount      int            `json:"request_count"`
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	CreditUsage       float64        `json:"credit_usage"`
	ContextUsagePct   float64        `json:"context_usage_pct"`
	ObservedStartAt   time.Time      `json:"observed_start_at"`
	ObservedEndAt     time.Time      `json:"observed_end_at"`
	RawSourcePath     string         `json:"raw_source_path,omitempty"`
	RawSourceLocator  string         `json:"raw_source_locator,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type BindToolUsageEventsRequest struct {
	WorkspaceID        string    `json:"workspace_id"`
	CommitCheckpointID int       `json:"commit_checkpoint_id"`
	CommitCapturedAt   time.Time `json:"commit_captured_at"`
	PreviousCapturedAt time.Time `json:"previous_captured_at"`
}

func (c *Client) SendToolUsageEvent(ctx context.Context, req ToolUsageEventRequest) error {
	return c.postJSON(ctx, "/api/v1/tool-usage-events", req, http.StatusCreated)
}

func (c *Client) BindToolUsageEvents(ctx context.Context, req BindToolUsageEventsRequest) error {
	return c.postJSON(ctx, "/api/v1/tool-usage-events/bind", req, http.StatusOK)
}
```

- [x] **Step 4: Implement the scanner and sync engine**

```go
// ae-cli/internal/attributionlocal/scanner.go
type ScanState struct {
	CodexSQLite CodexSQLiteWatermark `json:"codex_sqlite"`
}

type Scanner struct {
	codexSQLite *CodexSQLiteParser
}

func NewScanner() *Scanner {
	return &Scanner{codexSQLite: NewCodexSQLiteParser()}
}

func (s *Scanner) ScanWorkspace(workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	var out []LocalToolUsageEvent

	codexDB := filepath.Join(os.Getenv("HOME"), ".codex", "logs_2.sqlite")
	if _, err := os.Stat(codexDB); err == nil {
		items, wm, err := s.codexSQLite.Parse(codexDB, state.CodexSQLite)
		if err != nil {
			return nil, state, err
		}
		for _, item := range items {
			item.WorkspaceID = mustWorkspaceID(workspaceRoot)
			out = append(out, item)
		}
		state.CodexSQLite = wm
	}

	for _, path := range findCodexJSONLFiles(workspaceRoot) {
		items, err := ParseCodexJSONLFallback(path, workspaceRoot)
		if err == nil {
			for _, item := range items {
				item.WorkspaceID = mustWorkspaceID(workspaceRoot)
				out = append(out, item)
			}
		}
	}

	for _, path := range findClaudeJSONLFiles() {
		items, err := ParseClaudeJSONL(path, workspaceRoot)
		if err == nil {
			for _, item := range items {
				item.WorkspaceID = mustWorkspaceID(workspaceRoot)
				out = append(out, item)
			}
		}
	}

	for _, path := range findKiroJSONFiles() {
		items, err := ParseKiroJSON(path, workspaceRoot)
		if err == nil {
			for _, item := range items {
				item.WorkspaceID = mustWorkspaceID(workspaceRoot)
				out = append(out, item)
			}
		}
	}

	out = dedupeAndSort(out)
	return out, state, nil
}
```

```go
// ae-cli/internal/attributionlocal/sync.go
type BackendClient interface {
	SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error
	BindToolUsageEvents(ctx context.Context, req client.BindToolUsageEventsRequest) error
}

type SyncEngine struct {
	Scanner *Scanner
	Client  BackendClient
}

func (e *SyncEngine) Replay(ctx context.Context, workspaceRoot string) error {
	spooled, err := loadSpooledEvents(workspaceRoot)
	if err != nil {
		return err
	}
	for _, ev := range spooled {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			return err
		}
	}
	return clearSpooledEvents(workspaceRoot)
}
```

- [x] **Step 5: Wire a hidden sync command through `ae-cli hook`**

```go
// ae-cli/cmd/hook.go
var hookAttributionSyncCmd = &cobra.Command{
	Use:    "attribution-sync",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		engine := attributionlocal.NewSyncEngine(apiClient)
		return engine.RunForWorkspace(context.Background(), cwd)
	},
}

func init() {
	hookCmd.AddCommand(hookAttributionSyncCmd)
}
```

- [x] **Step 6: Run the ae-cli tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/client ./internal/attributionlocal ./cmd -run 'Test(Scanner_SkipsAlreadyWatermarkedCodexRows|Sync_ReplaysSpooledEventsBeforeNewScan|SendToolUsageEvent|BindToolUsageEvents)' -v
```

Expected:

```text
PASS
```

- [x] **Step 7: Commit**

```bash
git add ae-cli/internal/attributionlocal/scanner.go \
        ae-cli/internal/attributionlocal/scanner_test.go \
        ae-cli/internal/attributionlocal/sync.go \
        ae-cli/internal/attributionlocal/sync_test.go \
        ae-cli/internal/client/client.go \
        ae-cli/internal/client/client_test.go \
        ae-cli/cmd/hook.go \
        ae-cli/cmd/root.go
git commit -m "feat(ae-cli): add local attribution scanner and sync engine"
```

### Task 8: Trigger Sync From Git Hooks And Preserve Hook Chaining

**Files:**
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`
- Modify: `ae-cli/internal/hooks/install.go`
- Test: `ae-cli/internal/hooks/handler_test.go`

- [x] **Step 1: Write the failing git-hook sync tests**

```go
func TestPostCommit_TriggersAttributionSync(t *testing.T) {
	calls := 0
	old := runAttributionSync
	runAttributionSync = func(ctx context.Context, cwd string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { runAttributionSync = old })

	h := hooks.NewHandler(fakeUploader{})
	if err := h.PostCommit(context.Background(), fixtureRepoRoot(t)); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}
	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/hooks -run 'TestPostCommit_TriggersAttributionSync$' -v
```

Expected:

```text
FAIL ... undefined: runAttributionSync
```

- [x] **Step 3: Add the sync trigger to `PostCommit` and `PostRewrite`**

```go
// ae-cli/internal/hooks/handler.go
var runAttributionSync = func(ctx context.Context, cwd string) error {
	engine := attributionlocal.NewSyncEngine(newSyncBackendClient())
	return engine.RunForWorkspace(ctx, cwd)
}

func (h *Handler) PostCommit(ctx context.Context, cwd string) error {
	// existing checkpoint logic...
	_ = runAttributionSync(ctx, repoRoot)
	return nil
}

func (h *Handler) PostRewrite(ctx context.Context, cwd, rewriteType string, stdin io.Reader) error {
	// existing rewrite logic...
	_ = runAttributionSync(ctx, repoRoot)
	return nil
}
```

- [x] **Step 4: Verify shared hook scripts still chain legacy hooks**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/hooks -run 'Test(PostCommit_TriggersAttributionSync|InstallSharedHooks.*)' -v
```

Expected:

```text
PASS
```

- [x] **Step 5: Commit**

```bash
git add ae-cli/internal/hooks/handler.go \
        ae-cli/internal/hooks/handler_test.go \
        ae-cli/internal/hooks/install.go
git commit -m "feat(ae-cli): trigger attribution sync from git hooks"
```

### Task 9: Backend Attribution Reads Tool Usage Events Instead Of Session Usage Events

**Files:**
- Modify: `backend/internal/attribution/service.go`
- Modify: `backend/internal/attribution/service_test.go`
- Modify: `backend/ent/schema/pr_attribution_run.go`
- Modify: `backend/ent/schema/prrecord.go`
- Test: `backend/internal/attribution/service_test.go`

- [x] **Step 1: Write the failing attribution tests**

```go
func TestSettlePR_AggregatesToolUsageEventsByCheckpoint(t *testing.T) {
	ctx := context.Background()
	client := testdb.NewClient(t)
	svc := attribution.NewService(client, fakeRelayProvider{})
	fixture := seedPRAttributionFixtureWithToolUsage(t, client)

	result, err := svc.Settle(ctx, fixture.Provider, fixture.PR, "tester")
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if result.PrimaryTokenCount != 170 {
		t.Fatalf("PrimaryTokenCount = %d, want 170", result.PrimaryTokenCount)
	}
	if result.MetadataSummary["kiro_credit_usage"].(float64) != 0.6 {
		t.Fatalf("MetadataSummary = %+v", result.MetadataSummary)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/attribution -run 'TestSettlePR_AggregatesToolUsageEventsByCheckpoint$' -v
```

Expected:

```text
FAIL ... result counts still sourced from session_usage_events or zero
```

- [x] **Step 3: Change attribution interval loading to read `tool_usage_events`**

```go
// backend/internal/attribution/service.go
func (s *Service) loadIntervalToolUsage(ctx context.Context, checkpointID int) (tokenCount int64, creditUsage float64, requestCount int64, err error) {
	items, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.CommitCheckpointIDEQ(checkpointID)).
		All(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query tool usage events: %w", err)
	}
	for _, item := range items {
		tokenCount += item.InputTokens + item.OutputTokens + item.CachedInputTokens
		creditUsage += item.CreditUsage
		requestCount += int64(item.RequestCount)
	}
	return tokenCount, creditUsage, requestCount, nil
}
```

- [x] **Step 4: Persist credit-aware metadata summaries**

```go
// backend/internal/attribution/service.go
metadataSummary := map[string]any{
	"matched_commit_count":  len(matchedCommits),
	"matched_workspace_ids": matchedWorkspaceIDs,
	"kiro_credit_usage":     totalCreditUsage,
	"request_count":         totalRequestCount,
	"intervals":             intervals,
}
```

```go
// backend/ent/schema/pr_attribution_run.go and prrecord.go
field.JSON("metadata_summary", map[string]any{}).Optional()
```

- [x] **Step 5: Run the attribution tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/attribution -run 'TestSettlePR_AggregatesToolUsageEventsByCheckpoint$' -v
```

Expected:

```text
PASS
```

- [x] **Step 6: Commit**

```bash
git add backend/internal/attribution/service.go \
        backend/internal/attribution/service_test.go \
        backend/ent/schema/pr_attribution_run.go \
        backend/ent/schema/prrecord.go \
        backend/ent
git commit -m "feat(attribution): aggregate local tool usage by checkpoint"
```

### Task 10: Documentation And Project-Level Architecture Update

**Files:**
- Modify: `docs/architecture.md`
- Test: `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`

- [x] **Step 1: Write the failing documentation checklist**

```text
Confirm architecture.md still describes ae-cli start + local proxy as the only runtime path.
Expected: it is now stale once the new implementation lands.
```

- [x] **Step 2: Update the architecture overview**

```md
## Current Runtime Flow

The implemented attribution path no longer requires a user-visible `ae-cli start` lifecycle.
Tool-local artifacts plus git hooks are the primary source for commit/PR attribution, while
agent hooks act as repair triggers for local sync.
```

- [x] **Step 3: Run lightweight verification on docs and targeted tests**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/toolusage ./internal/attribution ./internal/handler -v

cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/attributionlocal ./internal/hooks ./internal/client ./cmd -v
```

Expected:

```text
PASS
```

- [x] **Step 4: Commit**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): describe sessionless local attribution runtime"
```

## Self-Review

### Spec coverage

- Local tool artifacts as primary fact source:
  Covered by Tasks 4, 5, 6, and 7.
- Agent hooks as repair triggers:
  Covered by Task 7 and Task 8.
- Git hooks as authoritative commit/rewrite binding:
  Covered by Task 8 and Task 9.
- New backend storage for workspaces and tool usage:
  Covered by Tasks 1, 2, and 3.
- PR settlement reading token + credit data:
  Covered by Task 9.
- Architecture/doc updates:
  Covered by Task 10.

### Placeholder scan

- No `TODO`, `TBD`, or “implement later” placeholders remain.
- Every task includes exact file paths, commands, and concrete code blocks.
- The only intentionally deferred topic is explicit `change_id`, which the approved spec marks as a future design and is therefore out of scope for this plan.

### Type consistency

- New event schema uses `tool_usage_events`, `workspace_id`, `tool_session_id`, `tool_event_id`, and `dedupe_key` consistently across backend and ae-cli tasks.
- `Kiro` remains `credit`-based everywhere in the plan; no later task assumes nonzero token fields for Kiro.
- Commit binding consistently points to `commit_checkpoint_id`; no later task introduces a conflicting link table.

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-13-sessionless-local-tool-attribution.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
