# PR Usage Snapshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the repo PR table’s attribution-centric UI with PR usage summaries and commit usage details, while making repo/project association and local usage collection reliable enough to support the new product path.

**Architecture:** The implementation keeps the existing sessionless fact chain (`ae-cli` local artifacts -> `tool_usage_events` -> `commit_checkpoints`) and adds a dedicated PR snapshot layer on top of it. Backend work splits into repo ensure, collector hardening, PR usage snapshot persistence, and PR API updates; frontend work only reads the new summary/detail contract and removes the old attribution UI. The refresh path is incremental: `Sync PRs` refreshes active PRs, while PR detail expansion can refresh one PR on demand.

**Tech Stack:** Go 1.23/1.24, Ent, Gin, Vue 3, TypeScript, Vitest, Cobra

---

## File Map

### Backend repo ensure and checkpoint fallback

- Create: `backend/internal/repo/remote_ensure.go`
- Create: `backend/internal/handler/repo_ensure_test.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/checkpoint/service.go`
- Modify: `backend/internal/checkpoint/service_test.go`
- Modify: `backend/internal/handler/checkpoint_test.go`

### ae-cli repo ensure wiring and collector hardening

- Create: `ae-cli/internal/repolink/ensure.go`
- Create: `ae-cli/internal/repolink/ensure_test.go`
- Create: `ae-cli/cmd/init_test.go`
- Create: `ae-cli/cmd/doctor_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/cmd/init.go`
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`
- Modify: `ae-cli/internal/attributionlocal/scanner.go`
- Modify: `ae-cli/internal/attributionlocal/types.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/scanner_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`

### PR usage snapshot persistence and service layer

- Create: `backend/ent/schema/pr_commit_usage_snapshot.go`
- Create: `backend/internal/prusage/service.go`
- Create: `backend/internal/prusage/service_test.go`
- Modify: `backend/ent/schema/prrecord.go`
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/cmd/server/main.go`
- Regenerate: `backend/ent/**`, `backend/ent/migrate/schema.go`

### PR sync and PR API refresh path

- Create: `backend/internal/handler/pr_usage_test.go`
- Modify: `backend/internal/prsync/service.go`
- Modify: `backend/internal/prsync/prsync_test.go`
- Modify: `backend/internal/prsync/prsync_extra_test.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/router.go`

### Frontend PR table and details view

- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

### Docs

- Modify: `docs/architecture.md`
- Review: `docs/superpowers/specs/2026-05-20-pr-usage-snapshots-design.md` for drift after implementation

### Verification

- Run: `cd ae-cli && go test ./cmd ./internal/repolink ./internal/attributionlocal ./internal/hooks`
- Run: `cd backend && go test ./internal/repo ./internal/checkpoint ./internal/prusage ./internal/prsync ./internal/handler`
- Run: `cd backend && go generate ./ent && go test ./...`
- Run: `cd frontend && pnpm test`

### Task 1: Add Backend Repo Ensure And Checkpoint Fallback

**Files:**
- Create: `backend/internal/repo/remote_ensure.go`
- Create: `backend/internal/handler/repo_ensure_test.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/checkpoint/service.go`
- Modify: `backend/internal/checkpoint/service_test.go`
- Modify: `backend/internal/handler/checkpoint_test.go`

- [x] **Step 1: Write failing tests for explicit repo ensure and checkpoint auto-create**

```go
func TestEnsureFromRemote_CreatesUnboundRepo(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	rc, err := svc.EnsureFromRemote(ctx, "https://github.com/acme/platform.git", "main")
	if err != nil {
		t.Fatalf("EnsureFromRemote error: %v", err)
	}
	if rc.RepoKey != "github.com/acme/platform" {
		t.Fatalf("RepoKey = %q, want github.com/acme/platform", rc.RepoKey)
	}
	if rc.ScmProviderID != nil {
		t.Fatalf("SCMProviderID = %v, want nil", rc.ScmProviderID)
	}

	count := client.RepoConfig.Query().CountX(ctx)
	if count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
}

func TestRecordCheckpointForUser_AutoCreatesRepoOnRemoteMiss(t *testing.T) {
	client := testdb.Open(t)
	svc := NewService(client)

	err := svc.RecordCheckpointForUser(context.Background(), 7, CommitCheckpointRequest{
		EventID:       "cp-auto-create",
		RepoFullName:  "https://github.com/acme/platform.git",
		WorkspaceID:   "ws-1",
		CommitSHA:     "abc123",
		BindingSource: "marker",
	})
	if err != nil {
		t.Fatalf("RecordCheckpointForUser error: %v", err)
	}

	rc := client.RepoConfig.Query().OnlyX(context.Background())
	if rc.RepoKey != "github.com/acme/platform" {
		t.Fatalf("RepoKey = %q, want github.com/acme/platform", rc.RepoKey)
	}
}
```

- [x] **Step 2: Run backend repo/checkpoint tests to verify they fail**

Run: `cd backend && go test ./internal/repo ./internal/checkpoint ./internal/handler -run 'TestEnsureFromRemote|TestRecordCheckpointForUser_AutoCreatesRepoOnRemoteMiss' -v`

Expected: FAIL because `EnsureFromRemote` does not exist yet and checkpoint still returns `repo not found` on repo lookup miss.

- [x] **Step 3: Implement a shared repo ensure path and use it from checkpoint writes**

```go
// backend/internal/repo/remote_ensure.go
package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
)

type RemoteEnsurer struct {
	entClient *ent.Client
}

func NewRemoteEnsurer(entClient *ent.Client) *RemoteEnsurer {
	return &RemoteEnsurer{entClient: entClient}
}

func (e *RemoteEnsurer) EnsureFromRemote(ctx context.Context, remoteURL, branch string) (*ent.RepoConfig, error) {
	if e == nil || e.entClient == nil {
		return nil, fmt.Errorf("ensure repo: ent client is required")
	}
	svc := NewService(e.entClient, "", nil)
	return svc.FindOrCreateFromRemote(ctx, strings.TrimSpace(remoteURL), strings.TrimSpace(branch))
}
```

```go
// backend/internal/handler/repo.go
type ensureRemoteRequest struct {
	RemoteURL string `json:"remote_url" binding:"required"`
	Branch    string `json:"branch"`
}

func (h *RepoHandler) EnsureRemote(c *gin.Context) {
	var req ensureRemoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	r, err := h.repoService.EnsureFromRemote(c.Request.Context(), req.RemoteURL, req.Branch)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	loaded, err := h.repoService.Get(c.Request.Context(), r.ID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, buildRepoResponse(loaded))
}
```

```go
// backend/internal/checkpoint/service.go
func (s *Service) resolveOrEnsureRepoConfig(ctx context.Context, repoFullName, cloneURL, branch string) (*ent.RepoConfig, error) {
	rc, err := s.resolveRepoConfig(ctx, repoFullName, cloneURL)
	if err == nil {
		return rc, nil
	}

	remoteURL := firstNonEmpty(strings.TrimSpace(cloneURL), strings.TrimSpace(repoFullName))
	if remoteURL == "" {
		return nil, err
	}

	ensurer := repo.NewRemoteEnsurer(s.entClient)
	return ensurer.EnsureFromRemote(ctx, remoteURL, branch)
}
```

- [x] **Step 4: Run the repo/checkpoint tests again**

Run: `cd backend && go test ./internal/repo ./internal/checkpoint ./internal/handler -run 'TestEnsureFromRemote|TestRecordCheckpointForUser_AutoCreatesRepoOnRemoteMiss|TestCheckpointHandler' -v`

Expected: PASS for the new ensure path, and checkpoint tests no longer fail on missing repo rows when a remote URL is present.

- [x] **Step 5: Commit the backend repo ensure foundation**

```bash
git add backend/internal/repo/remote_ensure.go \
  backend/internal/repo/service.go \
  backend/internal/repo/repo_test.go \
  backend/internal/handler/repo.go \
  backend/internal/handler/router.go \
  backend/internal/handler/repo_ensure_test.go \
  backend/internal/checkpoint/service.go \
  backend/internal/checkpoint/service_test.go \
  backend/internal/handler/checkpoint_test.go
git commit -m "feat(backend): ensure repos from git remotes"
```

### Task 2: Wire `ae-cli` Init Sync Doctor And Hooks To The Repo Ensure API

**Files:**
- Create: `ae-cli/internal/repolink/ensure.go`
- Create: `ae-cli/internal/repolink/ensure_test.go`
- Create: `ae-cli/cmd/init_test.go`
- Create: `ae-cli/cmd/doctor_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/cmd/init.go`
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`

- [x] **Step 1: Write failing tests for CLI-side repo ensure and hook ordering**

```go
func TestEnsureRepoFromRemote_SendsRemoteURLAndBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/ensure-remote" {
			t.Fatalf("path = %s, want /api/v1/repos/ensure-remote", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":17,"full_name":"github.com/acme/platform"}}`)
	}))
	defer server.Close()

	c := client.New(server.URL, "tok")
	if _, err := c.EnsureRepoFromRemote(context.Background(), "https://github.com/acme/platform.git", "main"); err != nil {
		t.Fatalf("EnsureRepoFromRemote error: %v", err)
	}
}

func TestPostCommitEnsuresRepoBeforeCheckpoint(t *testing.T) {
	clientStub := &recordingBackendHookClient{}
	h := NewHandler(NewBackendUploader(clientStub))

	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}
	if clientStub.order[0] != "ensure_repo" || clientStub.order[1] != "checkpoint" {
		t.Fatalf("order = %v, want ensure_repo then checkpoint", clientStub.order)
	}
}

func TestInitInstallsHooksWhenRepoEnsureIsSkippedOrFails(t *testing.T) {
  t.Setenv("AE_SERVER_TOKEN", "")
  called := false
  installSharedHooks = func(string, string) error {
    called = true
    return nil
  }

  err := initCmd.RunE(initCmd, nil)
  if err != nil {
    t.Fatalf("initCmd.RunE error: %v", err)
  }
  if !called {
    t.Fatal("expected shared hooks to be installed even when repo ensure is skipped")
  }
}
```

- [x] **Step 2: Run ae-cli command and hook tests to verify they fail**

Run: `cd ae-cli && go test ./cmd ./internal/repolink ./internal/hooks -run 'TestEnsureRepoFromRemote|TestPostCommitEnsuresRepoBeforeCheckpoint|TestDoctor' -v`

Expected: FAIL because the client has no ensure call, `init`/`doctor` do not report repo link state, and hooks never perform a repo ensure step.

- [x] **Step 3: Add an ae-cli repo-link helper and call it from init sync doctor and hooks**

```go
// ae-cli/internal/repolink/ensure.go
package repolink

import (
	"context"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type Ensurer interface {
	EnsureRepoFromRemote(ctx context.Context, remoteURL, branch string) (*client.RepoEnsureResponse, error)
}

func Ensure(ctx context.Context, c Ensurer, remoteURL, branch string) (string, error) {
	if c == nil || strings.TrimSpace(remoteURL) == "" {
		return "skipped", nil
	}
	_, err := c.EnsureRepoFromRemote(ctx, remoteURL, branch)
	if err != nil {
		return "failed", err
	}
	return "linked", nil
}
```

```go
// ae-cli/cmd/init.go
status := "skipped"
if token := resolveToken(configToken, ""); token != "" {
	status, err = repolink.Ensure(context.Background(), apiClient, detectRemoteURL(ctx.repoRoot), detectBranch(ctx.repoRoot))
}
fmt.Fprintf(out, "  Repo Link:     %s\n", status)
```

```go
// ae-cli/internal/hooks/handler.go
if syncClient != nil {
	if rc, ok := syncClient.(interface {
		EnsureRepoFromRemote(context.Context, string, string) (*client.RepoEnsureResponse, error)
	}); ok {
		_, _ = rc.EnsureRepoFromRemote(ctx, repoHint, branchSnapshot(cwd))
	}
}
```

- [x] **Step 4: Run the ae-cli tests again**

Run: `cd ae-cli && go test ./cmd ./internal/repolink ./internal/hooks -run 'TestEnsureRepoFromRemote|TestPostCommitEnsuresRepoBeforeCheckpoint|TestInit|TestDoctor' -v`

Expected: PASS, with `init` and `doctor` exposing repo-link status and hook ordering including the ensure step before checkpoint upload.

- [x] **Step 5: Commit the ae-cli repo-link wiring**

```bash
git add ae-cli/internal/client/client.go \
  ae-cli/internal/repolink/ensure.go \
  ae-cli/internal/repolink/ensure_test.go \
  ae-cli/cmd/init.go \
  ae-cli/cmd/sync.go \
  ae-cli/cmd/doctor.go \
  ae-cli/cmd/init_test.go \
  ae-cli/cmd/doctor_test.go \
  ae-cli/internal/hooks/handler.go \
  ae-cli/internal/hooks/handler_test.go
git commit -m "feat(ae-cli): ensure repos before attribution uploads"
```

### Task 3: Promote Codex SQLite To The Main Scanner Path And Add Incremental Scan State

**Files:**
- Modify: `ae-cli/internal/attributionlocal/scanner.go`
- Modify: `ae-cli/internal/attributionlocal/types.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/scanner_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `ae-cli/internal/attributionlocal/codex_sqlite.go`

- [x] **Step 1: Write failing scanner tests for sqlite priority and stateful re-scan**

```go
func TestScanner_UsesCodexSQLiteBeforeJSONLFallback(t *testing.T) {
	fixture := buildSQLiteOnlyAttributionFixture(t)
	scanner := NewScanner()

	events, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("ScanWorkspace: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].DedupeKey != "codex:conv-1:resp-1" {
		t.Fatalf("dedupe_key = %q, want codex:conv-1:resp-1", events[0].DedupeKey)
	}
}

func TestScanner_SecondScanWithStateReturnsNoDuplicateSQLiteEvents(t *testing.T) {
	fixture := buildSQLiteOnlyAttributionFixture(t)
	scanner := NewScanner()

	first, state, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	second, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, state)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d, want first=1 second=0", len(first), len(second))
	}
}
```

- [x] **Step 2: Run attributionlocal tests to verify they fail**

Run: `cd ae-cli && go test ./internal/attributionlocal -run 'TestScanner_UsesCodexSQLiteBeforeJSONLFallback|TestScanner_SecondScanWithStateReturnsNoDuplicateSQLiteEvents' -v`

Expected: FAIL because the scanner still ignores sqlite and `ScanState` does not store any watermark.

- [x] **Step 3: Extend `ScanState`, scan sqlite first, and persist per-source watermarks**

```go
// ae-cli/internal/attributionlocal/types.go
type ScanState struct {
	CodexSQLite map[string]CodexSQLiteWatermark `json:"codex_sqlite,omitempty"`
	FileModUnix map[string]int64                `json:"file_mod_unix,omitempty"`
}
```

```go
// ae-cli/internal/attributionlocal/scanner.go
for _, path := range findCodexSQLiteFiles(homeDir) {
	parser := NewCodexSQLiteParser()
	wm := state.CodexSQLite[path]
	items, nextWM, err := parser.Parse(path, wm)
	if err == nil {
		nextState.CodexSQLite[path] = nextWM
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}
}
```

```go
// ae-cli/internal/attributionlocal/scanner.go
func shouldScanFile(path string, state ScanState) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.ModTime().Unix() > state.FileModUnix[path]
}
```

- [x] **Step 4: Run attributionlocal tests again**

Run: `cd ae-cli && go test ./internal/attributionlocal -run 'TestScanner_|TestSyncEngine_' -v`

Expected: PASS, with sqlite-generated Codex events emitted on the first scan and filtered out on a second scan using the persisted state.

- [x] **Step 5: Commit the collector hardening changes**

```bash
git add ae-cli/internal/attributionlocal/scanner.go \
  ae-cli/internal/attributionlocal/types.go \
  ae-cli/internal/attributionlocal/sync.go \
  ae-cli/internal/attributionlocal/codex_sqlite.go \
  ae-cli/internal/attributionlocal/scanner_test.go \
  ae-cli/internal/attributionlocal/sync_test.go
git commit -m "fix(ae-cli): use codex sqlite and stateful attribution scans"
```

### Task 4: Add PR Usage Snapshot Schema And Aggregation Service

**Files:**
- Create: `backend/ent/schema/pr_commit_usage_snapshot.go`
- Create: `backend/internal/prusage/service.go`
- Create: `backend/internal/prusage/service_test.go`
- Modify: `backend/ent/schema/prrecord.go`
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/cmd/server/main.go`
- Regenerate: `backend/ent/**`, `backend/ent/migrate/schema.go`

- [x] **Step 1: Write failing tests for PR summary and commit snapshot aggregation**

```go
func TestRefreshPRUsage_WritesCommitSnapshotsAndSummary(t *testing.T) {
	fixture := newPRUsageFixture(t)
	result, err := fixture.Service.RefreshPR(context.Background(), fixture.Provider, fixture.PR)
	if err != nil {
		t.Fatalf("RefreshPR error: %v", err)
	}
	if result.Summary.InputTokens != 11 || result.Summary.OutputTokens != 7 {
		t.Fatalf("summary = %+v, want input=11 output=7", result.Summary)
	}
	rows := fixture.Client.PRCommitUsageSnapshot.Query().AllX(context.Background())
	if len(rows) != 2 {
		t.Fatalf("commit snapshots = %d, want 2", len(rows))
	}
}

func TestRefreshPRUsage_UsesCommitRewrites(t *testing.T) {
	fixture := newRewritePRUsageFixture(t)
	_, err := fixture.Service.RefreshPR(context.Background(), fixture.Provider, fixture.PR)
	if err != nil {
		t.Fatalf("RefreshPR error: %v", err)
	}
	row := fixture.Client.PRCommitUsageSnapshot.Query().OnlyX(context.Background())
	if row.CommitSHA != fixture.ExpectedCommitSHA {
		t.Fatalf("commit_sha = %q, want %q", row.CommitSHA, fixture.ExpectedCommitSHA)
	}
}
```

- [x] **Step 2: Run PR usage tests to verify they fail**

Run: `cd backend && go test ./internal/prusage -run 'TestRefreshPRUsage_' -v`

Expected: FAIL because the `prusage` package and `PRCommitUsageSnapshot` schema do not exist yet.

- [x] **Step 3: Add schema fields, create the `prusage` service, and regenerate Ent**

```go
// backend/ent/schema/prrecord.go
field.Int64("usage_input_tokens").Default(0),
field.Int64("usage_output_tokens").Default(0),
field.Int64("usage_cached_input_tokens").Default(0),
field.Int64("usage_reasoning_tokens").Default(0),
field.Float("usage_credit_usage").Default(0),
field.Int("usage_request_count").Default(0),
field.Int("usage_commit_count").Default(0),
field.Time("usage_refreshed_at").Optional().Nillable(),
field.String("usage_commit_snapshot_hash").Optional().Nillable(),
```

```go
// backend/internal/prusage/service.go
type Summary struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	RequestCount      int
	CommitCount       int
}

func (s *Service) RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*Result, error) {
	commitSHAs, err := provider.ListPRCommits(ctx, repoFullName, pr.ScmPrID)
	if err != nil {
		return nil, err
	}
	expanded, err := s.expandCommitCandidates(ctx, repoID, commitSHAs)
	if err != nil {
		return nil, err
	}
	// Load checkpoints, aggregate tool_usage_events by checkpoint, write summary + snapshots in one tx.
}
```

Run: `cd backend && go generate ./ent`

Expected: regenerated `backend/ent/**` and `backend/ent/migrate/schema.go` include the new table and `pr_records` fields.

- [x] **Step 4: Run PR usage tests and the targeted schema-dependent packages**

Run: `cd backend && go test ./internal/prusage ./internal/handler ./internal/prsync -run 'TestRefreshPRUsage_|TestPRHandler' -v`

Expected: PASS for the new aggregation path and schema wiring.

- [x] **Step 5: Commit the PR usage schema and service**

```bash
git add backend/ent/schema/prrecord.go \
  backend/ent/schema/pr_commit_usage_snapshot.go \
  backend/ent/generate.go \
  backend/ent \
  backend/internal/prusage/service.go \
  backend/internal/prusage/service_test.go \
  backend/internal/handler/interfaces.go \
  backend/cmd/server/main.go
git commit -m "feat(backend): add PR usage snapshot model"
```

### Task 5: Integrate PR Sync And PR Refresh APIs With Active-PR Rules

**Files:**
- Create: `backend/internal/handler/pr_usage_test.go`
- Modify: `backend/internal/prsync/service.go`
- Modify: `backend/internal/prsync/prsync_test.go`
- Modify: `backend/internal/prsync/prsync_extra_test.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/router.go`

- [x] **Step 1: Write failing tests for active-PR refresh and per-PR detail refresh**

```go
func TestSyncPRs_RefreshesOnlyActivePRs(t *testing.T) {
	svc := newPRSyncFixture(t)
	svc.UsageRefresher = &recordingUsageRefresher{}

	_, err := svc.Sync(context.Background(), svc.Provider, svc.Repo)
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}
	if got := svc.UsageRefresher.(*recordingUsageRefresher).PRIDs(); !reflect.DeepEqual(got, []int{11, 14}) {
		t.Fatalf("refreshed PR IDs = %v, want [11 14]", got)
	}
}

func TestPRHandlerRefreshUsage_ReturnsUpdatedPR(t *testing.T) {
	env := newPRHandlerFixture(t)
	w := doMockRequest(env, "POST", "/api/v1/prs/101/refresh-usage", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "\"usage_commit_count\":2") {
		t.Fatalf("body = %s", w.Body.String())
	}
}
```

- [x] **Step 2: Run PR sync and handler tests to verify they fail**

Run: `cd backend && go test ./internal/prsync ./internal/handler -run 'TestSyncPRs_RefreshesOnlyActivePRs|TestPRHandlerRefreshUsage_ReturnsUpdatedPR' -v`

Expected: FAIL because `prsync.Service` has no PR usage refresher and the handler has no `/refresh-usage` endpoint.

- [x] **Step 3: Inject the PR usage refresher into PR sync and expose a hidden refresh endpoint**

```go
// backend/internal/prsync/service.go
type usageRefresher interface {
	RefreshActivePRs(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, prIDs []int) error
}

type Service struct {
	entClient      *ent.Client
	usageRefresher usageRefresher
	logger         *zap.Logger
}

func (s *Service) Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	// upsert PR metadata first
	// collect active PR ids
	// refresh only active PRs via usageRefresher
}
```

```go
// backend/internal/handler/pr.go
func (h *PRHandler) RefreshUsage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	pr, err := h.entClient.PrRecord.Get(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "PR not found")
		return
	}
	if err := h.usageRefresher.RefreshPRByID(c.Request.Context(), pr.ID); err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	loaded, err := h.loadPRWithUsage(c.Request.Context(), pr.ID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get PR")
		return
	}
	pkg.Success(c, loaded)
}
```

- [x] **Step 4: Run PR sync and handler tests again**

Run: `cd backend && go test ./internal/prsync ./internal/handler -run 'TestSyncPRs_|TestPRHandlerRefreshUsage_' -v`

Expected: PASS, with `Sync PRs` touching only active PRs and `POST /api/v1/prs/:id/refresh-usage` returning the refreshed record.

- [x] **Step 5: Commit the PR sync and API integration**

```bash
git add backend/internal/prsync/service.go \
  backend/internal/prsync/prsync_test.go \
  backend/internal/prsync/prsync_extra_test.go \
  backend/internal/handler/pr.go \
  backend/internal/handler/router.go \
  backend/internal/handler/pr_usage_test.go
git commit -m "feat(prsync): refresh active PR usage snapshots"
```

### Task 6: Replace The Frontend Attribution UI With PR Usage Summaries And Commit Details

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

- [x] **Step 1: Write failing frontend tests for the new table columns and commit detail rendering**

```ts
it('does not render AI label or settle columns and shows usage summary columns', async () => {
  const { wrapper } = await mountRepoDetail()
  expect(wrapper.text()).not.toContain('AI Label')
  expect(wrapper.text()).not.toContain('Confidence')
  expect(wrapper.text()).not.toContain('Settle')
  expect(wrapper.text()).toContain('Input')
  expect(wrapper.text()).toContain('Output')
  expect(wrapper.text()).toContain('Credits')
})

it('renders commit rows from pr_commit_usage_snapshots and refreshes missing details once', async () => {
  const { wrapper, refreshPRUsage } = await mountRepoDetail()
  await wrapper.findAll('button').find((b) => b.text() === 'Details')!.trigger('click')
  expect(refreshPRUsage).toHaveBeenCalledWith(101)
  expect(wrapper.text()).toContain('Commit SHA')
  expect(wrapper.text()).toContain('abc123')
})

it('distinguishes a missing snapshot from a real zero value', async () => {
  const { wrapper } = await mountRepoDetail(undefined, [{
    id: 101,
    title: 'missing snapshot',
    usage_input_tokens: undefined,
    usage_output_tokens: 0,
    usage_credit_usage: 0,
  }])
  expect(wrapper.text()).toContain('—')
  expect(wrapper.text()).toContain('0')
})
```

- [x] **Step 2: Run the frontend tests to verify they fail**

Run: `cd frontend && pnpm test src/__tests__/api-modules.test.ts src/__tests__/repo-detail-view.test.ts`

Expected: FAIL because the old view still renders attribution columns and there is no `refreshPRUsage` client call.

- [x] **Step 3: Update the frontend types, API client, and view logic**

```ts
// frontend/src/types/index.ts
export interface PRCommitUsageSnapshot {
  commit_sha: string
  captured_at: string | null
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  reasoning_tokens: number
  credit_usage: number
  request_count: number
}

export interface PRRecord {
  // existing fields...
  usage_input_tokens?: number
  usage_output_tokens?: number
  usage_cached_input_tokens?: number
  usage_reasoning_tokens?: number
  usage_credit_usage?: number
  usage_request_count?: number
  usage_commit_count?: number
  usage_refreshed_at?: string | null
  edges?: {
    pr_commit_usage_snapshots?: PRCommitUsageSnapshot[]
  }
}
```

```ts
// frontend/src/api/pr.ts
export function refreshPRUsage(prId: number) {
  return client.post<ApiResponse<PRRecord>>(`/prs/${prId}/refresh-usage`)
}
```

```vue
<!-- frontend/src/views/repos/RepoDetailView.vue -->
<th class="px-3 py-2 text-left font-medium">Input</th>
<th class="px-3 py-2 text-left font-medium">Output</th>
<th class="px-3 py-2 text-left font-medium">Cache</th>
<th class="px-3 py-2 text-left font-medium">Reasoning</th>
<th class="px-3 py-2 text-left font-medium">Credits</th>
<th class="px-3 py-2 text-left font-medium">Requests</th>
```

- [x] **Step 4: Run the frontend tests and build checks**

Run: `cd frontend && pnpm test src/__tests__/api-modules.test.ts src/__tests__/repo-detail-view.test.ts && pnpm run build`

Expected: PASS for targeted tests, then a successful `vue-tsc` + Vite production build.

- [x] **Step 5: Commit the frontend PR usage UI**

```bash
git add frontend/src/types/index.ts \
  frontend/src/api/pr.ts \
  frontend/src/views/repos/RepoDetailView.vue \
  frontend/src/__tests__/api-modules.test.ts \
  frontend/src/__tests__/repo-detail-view.test.ts
git commit -m "feat(frontend): show PR usage summaries and commit details"
```

### Task 7: Update Architecture Docs And Run Full Verification

**Files:**
- Modify: `docs/architecture.md`
- Review: `docs/superpowers/specs/2026-05-20-pr-usage-snapshots-design.md` for drift after implementation

- [x] **Step 1: Write the failing documentation expectations as a checklist**

```md
- Repo detail PR view is described as usage-summary-first, not attribution-status-first.
- Repo/project ensure is described as part of the active sessionless runtime.
- Current collector status explicitly says Codex sqlite is the main path and watermark scanning is implemented.
```

- [x] **Step 2: Run focused verification before editing docs**

Run: `cd ae-cli && go test ./cmd ./internal/repolink ./internal/attributionlocal ./internal/hooks && cd ../backend && go test ./internal/repo ./internal/checkpoint ./internal/prusage ./internal/prsync ./internal/handler && cd ../frontend && pnpm test`

Expected: PASS across the targeted packages that changed the active architecture story.

- [x] **Step 3: Update architecture docs to match the shipped implementation**

```md
The repo detail view now treats PR usage summaries and commit usage details as the primary user-facing attribution surface. `ae-cli init`, `ae-cli sync`, and git hooks all best-effort ensure the remote repo exists in the backend before checkpoint and usage ingestion continue. The active collector path now uses Codex sqlite transport logs first, with JSONL fallback and persisted scan state to avoid repeated full scans.
```

- [x] **Step 4: Run full repository verification**

Run: `cd ae-cli && go test ./...`

Run: `cd backend && go generate ./ent && go test ./...`

Run: `cd frontend && pnpm test`

Expected: PASS across all three workspaces, with Ent generation producing no unstaged follow-up changes after the final test run.

- [ ] **Step 5: Commit the docs sync and full verification pass**

```bash
git add docs/architecture.md docs/superpowers/specs/2026-05-20-pr-usage-snapshots-design.md
git commit -m "docs(architecture): document PR usage snapshot runtime"
```
