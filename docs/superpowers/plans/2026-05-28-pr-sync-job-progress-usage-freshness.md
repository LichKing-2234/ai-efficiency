# PR Sync Job Progress And Usage Freshness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace blocking repo PR sync with a backend job that exposes stage progress and PR/commit usage freshness reasons in the repo detail UI.

**Architecture:** Keep the existing PR sync and PR usage snapshot chain, but add a `pr_sync_jobs` Ent model and progress-aware execution path in `backend/internal/prsync`. The handler starts or reuses a repo-scoped job, the frontend polls job status, and `prusage` provides lightweight freshness evaluation for PR list rows and commit detail rows.

**Tech Stack:** Go, Gin, Ent, PostgreSQL-backed Ent tests, Vue 3, TypeScript, Vitest, TailwindCSS.

**Status:** Tasks 1-2 complete in `feat/pr-sync-job-progress`; Tasks 3-7 remain.

---

## File Map

### Backend Job Model And Generated Ent Code

- Create: `backend/ent/schema/pr_sync_job.go`
- Modify: `backend/ent/schema/repoconfig.go`
- Regenerate: `backend/ent/**`
- Modify: `backend/ent/migrate/schema.go`

### Backend PR Sync Job Execution

- Modify: `backend/internal/prsync/service.go`
- Create: `backend/internal/prsync/job_test.go`
- Modify: `backend/internal/prsync/prsync_test.go`
- Modify: `backend/internal/prsync/prsync_extra_test.go`

### Backend Handlers And API

- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/router.go`
- Create: `backend/internal/handler/pr_sync_job_test.go`
- Modify: `backend/internal/handler/pr_usage_test.go`

### Backend Usage Freshness

- Create: `backend/internal/prusage/freshness.go`
- Create: `backend/internal/prusage/freshness_test.go`
- Modify: `backend/internal/prusage/service.go`

### Frontend API, Types, And Repo Detail UI

- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

### Docs

- Modify: `docs/architecture.md`

### Verification

- Run: `cd backend && go test ./internal/prsync ./internal/prusage ./internal/handler`
- Run: `cd backend && go generate ./ent`
- Run: `cd backend && go test ./...`
- Run: `cd frontend && pnpm test`

---

## Task 1: Add `pr_sync_jobs` Ent Schema

**Files:**
- Create: `backend/ent/schema/pr_sync_job.go`
- Modify: `backend/ent/schema/repoconfig.go`
- Regenerate: `backend/ent/**`
- Modify: `backend/ent/migrate/schema.go`

- [x] **Step 1: Create the job schema**

Add `backend/ent/schema/pr_sync_job.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PRSyncJob struct {
	ent.Schema
}

func (PRSyncJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int("repo_config_id"),
		field.Enum("status").
			Values("queued", "running", "completed", "failed", "cancelled", "abandoned").
			Default("queued"),
		field.Enum("phase").
			Values("queued", "fetching_prs", "upserting_prs", "labeling", "refreshing_usage", "completed", "failed").
			Default("queued"),
		field.Int("page_size").Default(100),
		field.Int("current_page").Default(0),
		field.Int("fetched_prs").Default(0),
		field.Int("total_prs").Default(0),
		field.Int("processed_prs").Default(0),
		field.Int("created_prs").Default(0),
		field.Int("changed_prs").Default(0),
		field.Int("unchanged_prs").Default(0),
		field.Int("upsert_failed_prs").Default(0),
		field.Int("labeled_prs").Default(0),
		field.Int("label_failed_prs").Default(0),
		field.Int("usage_total_prs").Default(0),
		field.Int("usage_refreshed_prs").Default(0),
		field.Int("usage_skipped_prs").Default(0),
		field.Int("usage_failed_prs").Default(0),
		field.String("last_error").Optional().Nillable(),
		field.JSON("error_summary", []map[string]any{}).Optional(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (PRSyncJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("pr_sync_jobs").
			Field("repo_config_id").
			Unique().
			Required(),
	}
}

func (PRSyncJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repo_config_id", "status"),
		index.Fields("created_at"),
	}
}
```

Also add the inverse edge in `backend/ent/schema/repoconfig.go`:

```go
edge.To("pr_sync_jobs", PRSyncJob.Type),
```

- [x] **Step 2: Generate Ent code**

Run:

```bash
cd backend && go generate ./ent
```

Expected: command exits 0 and generated files include `backend/ent/prsyncjob.go`, `backend/ent/prsyncjob/`, and migration changes.

- [x] **Step 3: Run focused schema compile check**

Run:

```bash
cd backend && go test ./ent ./internal/testdb
```

Expected: PASS.

- [x] **Step 4: Commit the schema**

```bash
git add backend/ent
git commit -m "feat(prsync): add PR sync job schema"
```

---

## Task 2: Add Progress-Aware PR Sync Execution

**Files:**
- Modify: `backend/internal/prsync/service.go`
- Create: `backend/internal/prsync/job_test.go`
- Modify: `backend/internal/prsync/prsync_test.go`
- Modify: `backend/internal/prsync/prsync_extra_test.go`

- [x] **Step 1: Write failing tests for job creation and reuse**

Create `backend/internal/prsync/job_test.go` with:

```go
package prsync

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func TestStartSyncJobCreatesQueuedJob(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-create-repo")
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if reused {
		t.Fatalf("reused = true, want false")
	}
	if job.RepoConfigID != rc.ID || job.Status != prsyncjob.StatusQueued || job.Phase != prsyncjob.PhaseQueued {
		t.Fatalf("job = %+v, want queued job for repo %d", job, rc.ID)
	}
}

func TestStartSyncJobReusesRunningJobForRepo(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-reuse-repo")
	existing := client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.PhaseFetchingPrs).
		SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if !reused {
		t.Fatalf("reused = false, want true")
	}
	if job.ID != existing.ID {
		t.Fatalf("job id = %d, want existing %d", job.ID, existing.ID)
	}
}
```

- [x] **Step 2: Run the job tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/prsync -run 'TestStartSyncJob' -count=1
```

Expected: FAIL because `StartSyncJob` is not defined.

- [x] **Step 3: Add job result and progress types**

In `backend/internal/prsync/service.go`, add these types near `SyncResult`:

```go
type UpsertState string

const (
	UpsertCreated   UpsertState = "created"
	UpsertChanged   UpsertState = "changed"
	UpsertUnchanged UpsertState = "unchanged"
)

type SyncProgress struct {
	Phase             string
	CurrentPage       int
	PageSize          int
	FetchedPRs        int
	TotalPRs          int
	ProcessedPRs      int
	CreatedPRs        int
	ChangedPRs        int
	UnchangedPRs      int
	UpsertFailedPRs   int
	LabeledPRs        int
	LabelFailedPRs    int
	UsageTotalPRs     int
	UsageRefreshedPRs int
	UsageSkippedPRs   int
	UsageFailedPRs    int
}

type ProgressSink interface {
	UpdateProgress(ctx context.Context, jobID int, progress SyncProgress) error
	FailJob(ctx context.Context, jobID int, phase string, err error) error
	CompleteJob(ctx context.Context, jobID int, result SyncResult) error
}
```

- [x] **Step 4: Implement `StartSyncJob` without worker launch**

In `backend/internal/prsync/service.go`, add:

```go
func (s *Service) StartSyncJob(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
	if s == nil || s.entClient == nil {
		return nil, false, fmt.Errorf("start PR sync job: ent client is required")
	}
	if scmProvider == nil {
		return nil, false, fmt.Errorf("start PR sync job: scm provider is required")
	}
	if rc == nil {
		return nil, false, fmt.Errorf("start PR sync job: repo config is required")
	}
	existing, err := s.entClient.PRSyncJob.Query().
		Where(
			prsyncjob.RepoConfigIDEQ(rc.ID),
			prsyncjob.StatusIn(prsyncjob.StatusQueued, prsyncjob.StatusRunning),
		).
		Order(ent.Desc(prsyncjob.FieldCreatedAt)).
		First(ctx)
	if err == nil {
		return existing, true, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("query running PR sync job: %w", err)
	}
	job, err := s.entClient.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusQueued).
		SetPhase(prsyncjob.PhaseQueued).
		SetPageSize(100).
		Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("create PR sync job: %w", err)
	}
	return job, false, nil
}
```

Add imports for `github.com/ai-efficiency/backend/ent/prsyncjob`.

- [x] **Step 5: Run job creation tests**

Run:

```bash
cd backend && go test ./internal/prsync -run 'TestStartSyncJob' -count=1
```

Expected: PASS.

- [x] **Step 6: Write failing test for upsert changed vs unchanged**

Append to `backend/internal/prsync/job_test.go`:

```go
func TestUpsertPRDistinguishesChangedAndUnchanged(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "upsert-state-repo")
	svc := NewService(client, nil, zap.NewNop())

	first := &scm.PR{ID: 7, Title: "Initial", Author: "alice", SourceBranch: "feat", TargetBranch: "main", State: "open", URL: "https://example.com/pr/7"}
	_, state, err := svc.upsertPR(ctx, rc.ID, first)
	if err != nil {
		t.Fatalf("first upsert error: %v", err)
	}
	if state != UpsertCreated {
		t.Fatalf("first state = %s, want %s", state, UpsertCreated)
	}

	_, state, err = svc.upsertPR(ctx, rc.ID, first)
	if err != nil {
		t.Fatalf("second upsert error: %v", err)
	}
	if state != UpsertUnchanged {
		t.Fatalf("second state = %s, want %s", state, UpsertUnchanged)
	}

	changed := *first
	changed.Title = "Changed"
	_, state, err = svc.upsertPR(ctx, rc.ID, &changed)
	if err != nil {
		t.Fatalf("third upsert error: %v", err)
	}
	if state != UpsertChanged {
		t.Fatalf("third state = %s, want %s", state, UpsertChanged)
	}
}
```

- [x] **Step 7: Run the upsert-state test and verify it fails**

Run:

```bash
cd backend && go test ./internal/prsync -run TestUpsertPRDistinguishesChangedAndUnchanged -count=1
```

Expected: FAIL because `upsertPR` still returns `(int, bool, error)`.

- [x] **Step 8: Change `upsertPR` to return `UpsertState`**

Update the signature:

```go
func (s *Service) upsertPR(ctx context.Context, repoConfigID int, pr *scm.PR) (int, UpsertState, error)
```

Add a helper in `backend/internal/prsync/service.go`:

```go
func prChanged(existing *ent.PrRecord, pr *scm.PR) bool {
	if existing.Title != pr.Title ||
		existing.Author != pr.Author ||
		existing.SourceBranch != pr.SourceBranch ||
		existing.TargetBranch != pr.TargetBranch ||
		existing.Status != mapPRStatus(pr.State) ||
		existing.ScmPrURL != pr.URL ||
		existing.LinesAdded != pr.LinesAdded ||
		existing.LinesDeleted != pr.LinesDeleted {
		return true
	}
	if !pr.CreatedAt.IsZero() && !existing.CreatedAt.Equal(pr.CreatedAt) {
		return true
	}
	if !pr.MergedAt.IsZero() {
		if existing.MergedAt == nil || !existing.MergedAt.Equal(pr.MergedAt) {
			return true
		}
	}
	if len(pr.Labels) > 0 && !slices.Equal(existing.Labels, pr.Labels) {
		return true
	}
	return false
}
```

Add import `slices`.

In the existing-record branch, return `UpsertUnchanged` without calling update when `prChanged(existing, pr)` is false. Return `UpsertChanged` after a successful update. Return `UpsertCreated` after create.

- [x] **Step 9: Update existing tests for new return type**

In `backend/internal/prsync/prsync_test.go` and `backend/internal/prsync/prsync_extra_test.go`, replace checks of `created` bool with `UpsertState`.

Example replacement:

```go
_, state, err := svc.upsertPR(ctx, rc.ID, pr)
if err != nil {
	t.Fatalf("upsertPR error: %v", err)
}
if state != UpsertChanged {
	t.Fatalf("state = %s, want %s", state, UpsertChanged)
}
```

- [x] **Step 10: Run PR sync tests**

Run:

```bash
cd backend && go test ./internal/prsync -count=1
```

Expected: PASS.

- [x] **Step 11: Implement job progress persistence**

Add methods to `backend/internal/prsync/service.go`:

```go
func (s *Service) UpdateProgress(ctx context.Context, jobID int, p SyncProgress) error {
	update := s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.Phase(p.Phase)).
		SetCurrentPage(p.CurrentPage).
		SetPageSize(p.PageSize).
		SetFetchedPrs(p.FetchedPRs).
		SetTotalPrs(p.TotalPRs).
		SetProcessedPrs(p.ProcessedPRs).
		SetCreatedPrs(p.CreatedPRs).
		SetChangedPrs(p.ChangedPRs).
		SetUnchangedPrs(p.UnchangedPRs).
		SetUpsertFailedPrs(p.UpsertFailedPRs).
		SetLabeledPrs(p.LabeledPRs).
		SetLabelFailedPrs(p.LabelFailedPRs).
		SetUsageTotalPrs(p.UsageTotalPRs).
		SetUsageRefreshedPrs(p.UsageRefreshedPRs).
		SetUsageSkippedPrs(p.UsageSkippedPRs).
		SetUsageFailedPrs(p.UsageFailedPRs)
	if p.Phase == string(prsyncjob.PhaseFetchingPrs) || p.Phase == string(prsyncjob.PhaseUpsertingPrs) {
		update.SetStartedAt(time.Now().UTC())
	}
	return update.Exec(ctx)
}

func (s *Service) FailJob(ctx context.Context, jobID int, phase string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusFailed).
		SetPhase(prsyncjob.PhaseFailed).
		SetNillableLastError(&msg).
		SetCompletedAt(time.Now().UTC()).
		Exec(ctx)
}

func (s *Service) CompleteJob(ctx context.Context, jobID int, result SyncResult) error {
	return s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusCompleted).
		SetPhase(prsyncjob.PhaseCompleted).
		SetTotalPrs(result.Total).
		SetCreatedPrs(result.Created).
		SetChangedPrs(result.Updated).
		SetCompletedAt(time.Now().UTC()).
		Exec(ctx)
}
```

- [x] **Step 12: Add progress-aware sync runner**

Add this method to `backend/internal/prsync/service.go`:

```go
func (s *Service) RunSyncJob(ctx context.Context, jobID int, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	result, err := s.SyncWithProgress(ctx, scmProvider, rc, jobID, s)
	if err != nil {
		_ = s.FailJob(context.Background(), jobID, "failed", err)
		return nil, err
	}
	_ = s.CompleteJob(context.Background(), jobID, *result)
	return result, nil
}
```

Implement `SyncWithProgress` by moving the current `Sync` body into a new method:

```go
func (s *Service) SyncWithProgress(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig, jobID int, sink ProgressSink) (*SyncResult, error)
```

Keep existing `Sync` as:

```go
func (s *Service) Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	return s.SyncWithProgress(ctx, scmProvider, rc, 0, nil)
}
```

`SyncWithProgress` must call `sink.UpdateProgress` after each fetch page, after upsert progress changes, after labeling, and after each usage refresh. It must still return `SyncResult`.

- [x] **Step 13: Add focused progress test**

Append to `backend/internal/prsync/job_test.go`:

```go
func TestRunSyncJobUpdatesProgressAndCompletes(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-progress-repo")
	svc := NewService(client, nil, zap.NewNop())
	job := client.PRSyncJob.Create().SetRepoConfigID(rc.ID).SaveX(ctx)

	callCount := 0
	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			callCount++
			if callCount == 1 {
				items := make([]*scm.PR, 100)
				for i := range items {
					items[i] = &scm.PR{ID: i + 1, Title: "PR", Author: "alice", SourceBranch: "feat", TargetBranch: "main", State: "open"}
				}
				return items, nil
			}
			return []*scm.PR{{ID: 101, Title: "Last PR", Author: "bob", SourceBranch: "feat", TargetBranch: "main", State: "open"}}, nil
		},
	}

	result, err := svc.RunSyncJob(ctx, job.ID, mock, rc)
	if err != nil {
		t.Fatalf("RunSyncJob error: %v", err)
	}
	if result.Total != 101 {
		t.Fatalf("total = %d, want 101", result.Total)
	}
	loaded := client.PRSyncJob.GetX(ctx, job.ID)
	if loaded.Status != prsyncjob.StatusCompleted || loaded.Phase != prsyncjob.PhaseCompleted {
		t.Fatalf("job status=%s phase=%s, want completed/completed", loaded.Status, loaded.Phase)
	}
	if loaded.FetchedPrs != 101 || loaded.ProcessedPrs != 101 {
		t.Fatalf("progress fetched=%d processed=%d, want 101/101", loaded.FetchedPrs, loaded.ProcessedPrs)
	}
}
```

- [x] **Step 14: Run PR sync package tests**

Run:

```bash
cd backend && go test ./internal/prsync -count=1
```

Expected: PASS.

- [x] **Step 15: Commit PR sync job execution**

```bash
git add backend/internal/prsync
git commit -m "feat(prsync): run PR sync as progress job"
```

---

## Task 3: Add PR Sync Job HTTP API

**Files:**
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/router.go`
- Create: `backend/internal/handler/pr_sync_job_test.go`

- [x] **Step 1: Write failing handler tests**

Create `backend/internal/handler/pr_sync_job_test.go`:

```go
package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/scm"
)

type mockPRSyncJobber struct {
	startFn func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error)
	runFn   func(ctx context.Context, jobID int, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	getFn   func(ctx context.Context, id int) (*ent.PRSyncJob, error)
}

func (m *mockPRSyncJobber) Sync(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error) {
	return nil, nil
}

func (m *mockPRSyncJobber) StartSyncJob(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
	return m.startFn(ctx, provider, rc)
}

func (m *mockPRSyncJobber) RunSyncJob(ctx context.Context, jobID int, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error) {
	if m.runFn == nil {
		return nil, nil
	}
	return m.runFn(ctx, jobID, provider, rc)
}

func (m *mockPRSyncJobber) GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error) {
	return m.getFn(ctx, id)
}

func attachPRSyncJobRoutes(t *testing.T, env *mockTestEnv, repoSCM repoSCMProvider, syncer prSyncer) {
	t.Helper()
	prHandler := NewPRHandler(env.client, repoSCM, syncer, nil)
	api := env.router.Group("/api/v1")
	api.Use(auth.RequireAuth(env.authSvc))
	api.POST("/repos/:id/sync-prs", prHandler.SyncPRs)
	api.GET("/pr-sync-jobs/:id", prHandler.GetSyncJob)
}

func TestSyncPRsStartsJobAndReturnsJobID(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: id, FullName: "org/repo"}, nil
		},
	}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
			return &ent.PRSyncJob{ID: 44, RepoConfigID: rc.ID, Status: prsyncjob.StatusQueued, Phase: prsyncjob.PhaseQueued}, false, nil
		},
		getFn: func(ctx context.Context, id int) (*ent.PRSyncJob, error) { return nil, nil },
	})

	w := doMockRequest(env, "POST", "/api/v1/repos/9/sync-prs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["job_id"] != float64(44) || data["status"] != "queued" {
		t.Fatalf("data = %+v, want job_id=44 status=queued", data)
	}
}

func TestGetPRSyncJobReturnsProgress(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) { return nil, false, nil },
		getFn: func(ctx context.Context, id int) (*ent.PRSyncJob, error) {
			return &ent.PRSyncJob{ID: id, RepoConfigID: 9, Status: prsyncjob.StatusRunning, Phase: prsyncjob.PhaseRefreshingUsage, FetchedPrs: 120, ProcessedPrs: 100}, nil
		},
	})

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/pr-sync-jobs/%d", 44), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["phase"] != "refreshing_usage" || data["fetched_prs"] != float64(120) {
		t.Fatalf("data = %+v, want phase refreshing_usage fetched_prs 120", data)
	}
}
```

- [x] **Step 2: Run handler tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestSyncPRsStartsJob|TestGetPRSyncJob' -count=1
```

Expected: FAIL because `GetSyncJob`, `StartSyncJob`, and `GetSyncJob` interface methods are not wired.

- [x] **Step 3: Extend handler interface**

Update `backend/internal/handler/interfaces.go`:

```go
type prSyncer interface {
	Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	StartSyncJob(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error)
	RunSyncJob(ctx context.Context, jobID int, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error)
}
```

- [x] **Step 4: Add `GetSyncJob` service method**

In `backend/internal/prsync/service.go`:

```go
func (s *Service) GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("get PR sync job: ent client is required")
	}
	job, err := s.entClient.PRSyncJob.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get PR sync job %d: %w", id, err)
	}
	return job, nil
}
```

- [x] **Step 5: Change `SyncPRs` handler to return job**

Replace the final synchronous call in `backend/internal/handler/pr.go`:

```go
job, reused, err := h.syncService.StartSyncJob(c.Request.Context(), scmProvider, rc)
if err != nil {
	pkg.Error(c, http.StatusInternalServerError, "sync failed: "+err.Error())
	return
}
pkg.Success(c, gin.H{
	"job_id": job.ID,
	"status": string(job.Status),
	"phase":  string(job.Phase),
	"reused": reused,
})
```

After the job is created, start the worker in a goroutine only when `reused == false`:

```go
if !reused {
	go func(jobID int, provider scm.SCMProvider, repoConfig *ent.RepoConfig) {
		_, _ = h.syncService.RunSyncJob(context.Background(), jobID, provider, repoConfig)
	}(job.ID, scmProvider, rc)
}
```

- [x] **Step 6: Add job detail handler**

In `backend/internal/handler/pr.go`:

```go
func (h *PRHandler) GetSyncJob(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.syncService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr sync service is not configured")
		return
	}
	job, err := h.syncService.GetSyncJob(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR sync job not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{
		"id": job.ID,
		"repo_config_id": job.RepoConfigID,
		"status": string(job.Status),
		"phase": string(job.Phase),
		"current_page": job.CurrentPage,
		"page_size": job.PageSize,
		"fetched_prs": job.FetchedPrs,
		"total_prs": job.TotalPrs,
		"processed_prs": job.ProcessedPrs,
		"created_prs": job.CreatedPrs,
		"changed_prs": job.ChangedPrs,
		"unchanged_prs": job.UnchangedPrs,
		"usage_total_prs": job.UsageTotalPrs,
		"usage_refreshed_prs": job.UsageRefreshedPrs,
		"usage_skipped_prs": job.UsageSkippedPrs,
		"usage_failed_prs": job.UsageFailedPrs,
		"last_error": job.LastError,
	})
}
```

- [x] **Step 7: Register route**

In `backend/internal/handler/router.go`, add after the PR group:

```go
prSyncJobGroup := protected.Group("/pr-sync-jobs")
{
	prSyncJobGroup.GET("/:id", prHandler.GetSyncJob)
}
```

- [x] **Step 8: Run handler tests**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestSyncPRsStartsJob|TestGetPRSyncJob' -count=1
```

Expected: PASS.

- [x] **Step 9: Run broader handler/prsync tests**

Run:

```bash
cd backend && go test ./internal/prsync ./internal/handler -count=1
```

Expected: PASS.

- [x] **Step 10: Commit API changes**

```bash
git add backend/internal/prsync backend/internal/handler
git commit -m "feat(backend): expose PR sync job API"
```

---

## Task 4: Add Usage Freshness Evaluation

**Files:**
- Create: `backend/internal/prusage/freshness.go`
- Create: `backend/internal/prusage/freshness_test.go`
- Modify: `backend/internal/prusage/service.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/pr_usage_test.go`

- [x] **Step 1: Write failing freshness tests**

Create `backend/internal/prusage/freshness_test.go`:

```go
package prusage

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
)

func TestEvaluateCommitFreshnessNoCheckpoint(t *testing.T) {
	client, _, pr, _ := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusNoCheckpoint {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusNoCheckpoint)
	}
}

func TestEvaluateCommitFreshnessNoUsageEvents(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	cp := client.CommitCheckpoint.Create().
		SetEventID("fresh-cp").
		SetUserID(userID).
		SetWorkspaceID("ws-fresh").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("abc123").
		SetParentShas([]string{"base"}).
		SetCapturedAt(time.Now().UTC()).
		SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("abc123").
		SetCommitCheckpointID(cp.ID).
		SetSortOrder(0).
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusNoUsageEvents {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusNoUsageEvents)
	}
	if len(status.Commits) != 1 || status.Commits[0].Status != UsageStatusNoUsageEvents {
		t.Fatalf("commits = %+v, want no_usage_events", status.Commits)
	}
}

func TestEvaluateCommitFreshnessPendingUploadWhenUnboundUsageExists(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-pending").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("session-pending").
		SetUsageUnit("token").
		SetInputTokens(42).
		SetOutputTokens(7).
		SetObservedStartAt(now.Add(-2 * time.Minute)).
		SetObservedEndAt(now.Add(-1 * time.Minute)).
		SetDedupeKey("pending-upload-1").
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusPendingUpload {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusPendingUpload)
	}
}

func TestEvaluateCommitFreshnessStaleSnapshotWhenNewUsageArrives(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	refreshedAt := time.Now().Add(-30 * time.Minute).UTC()
	cp := client.CommitCheckpoint.Create().
		SetEventID("stale-cp").
		SetUserID(userID).
		SetWorkspaceID("ws-stale").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("stale-sha").
		SetParentShas([]string{"base"}).
		SetCapturedAt(refreshedAt.Add(-5 * time.Minute)).
		SaveX(ctx)
	client.PrRecord.UpdateOneID(pr.ID).
		SetUsageRefreshedAt(refreshedAt).
		ExecX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("stale-sha").
		SetCommitCheckpointID(cp.ID).
		SetInputTokens(1).
		SetSortOrder(0).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-stale").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("session-stale").
		SetUsageUnit("token").
		SetInputTokens(9).
		SetObservedStartAt(refreshedAt.Add(5 * time.Minute)).
		SetObservedEndAt(refreshedAt.Add(6 * time.Minute)).
		SetDedupeKey("stale-usage-1").
		SetCommitCheckpointID(cp.ID).
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusStaleSnapshot {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusStaleSnapshot)
	}
}
```

- [x] **Step 2: Run freshness tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/prusage -run TestEvaluateCommitFreshness -count=1
```

Expected: FAIL because `EvaluatePRFreshness` and status constants are not defined.

- [x] **Step 3: Implement freshness types**

Add `backend/internal/prusage/freshness.go`:

```go
package prusage

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type UsageStatus string

const (
	UsageStatusFresh         UsageStatus = "fresh"
	UsageStatusPendingUpload UsageStatus = "pending_upload"
	UsageStatusNoCheckpoint  UsageStatus = "no_checkpoint"
	UsageStatusNoUsageEvents UsageStatus = "no_usage_events"
	UsageStatusUnbound       UsageStatus = "unbound"
	UsageStatusStaleSnapshot UsageStatus = "stale_snapshot"
	UsageStatusRefreshFailed UsageStatus = "refresh_failed"
	UsageStatusUnknown       UsageStatus = "unknown"
)

type CommitFreshness struct {
	CommitSHA       string      `json:"commit_sha"`
	Status          UsageStatus `json:"usage_status"`
	Reason          string      `json:"usage_status_reason"`
	CheckpointFound bool        `json:"checkpoint_found"`
	UsageEventFound bool        `json:"usage_event_found"`
}

type PRFreshness struct {
	Status    UsageStatus       `json:"usage_status"`
	Reason    string            `json:"usage_status_reason"`
	CheckedAt time.Time         `json:"usage_status_checked_at"`
	Commits   []CommitFreshness `json:"commits"`
}
```

- [x] **Step 4: Implement evaluator**

In `backend/internal/prusage/freshness.go`, add:

```go
func (s *Service) EvaluatePRFreshness(ctx context.Context, prID int) (*PRFreshness, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("evaluate PR freshness: ent client is required")
	}
	pr, err := s.entClient.PrRecord.Query().
		Where(prrecord.IDEQ(prID)).
		WithPrCommitUsageSnapshots().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate PR freshness: load PR: %w", err)
	}
	snapshots := pr.Edges.PrCommitUsageSnapshots
	checkedAt := time.Now().UTC()
	if len(snapshots) == 0 {
		pendingCount, err := s.entClient.ToolUsageEvent.Query().
			Where(
				toolusageevent.RepoConfigIDEQ(pr.RepoConfigID),
				toolusageevent.CommitCheckpointIDIsNil(),
			).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate PR freshness: count pending usage events: %w", err)
		}
		if pendingCount > 0 {
			return &PRFreshness{Status: UsageStatusPendingUpload, Reason: "Unbound usage events exist for this repo and may still be waiting for checkpoint binding.", CheckedAt: checkedAt}, nil
		}
		if pr.UsageRefreshedAt == nil {
			return &PRFreshness{Status: UsageStatusNoCheckpoint, Reason: "No PR commit snapshot has been generated yet.", CheckedAt: checkedAt}, nil
		}
		return &PRFreshness{Status: UsageStatusNoCheckpoint, Reason: "Snapshot refresh ran but no PR commit rows were recorded.", CheckedAt: checkedAt}, nil
	}

	commits := make([]CommitFreshness, 0, len(snapshots))
	overall := UsageStatusFresh
	reason := "Usage snapshot is current."
	for _, snapshot := range snapshots {
		item := CommitFreshness{CommitSHA: snapshot.CommitSha, Status: UsageStatusFresh, Reason: "Usage events were included.", UsageEventFound: true}
		if snapshot.CommitCheckpointID == nil {
			item.Status = UsageStatusNoCheckpoint
			item.Reason = "No checkpoint matched this PR commit."
			item.CheckpointFound = false
			item.UsageEventFound = false
		} else {
			item.CheckpointFound = true
			count, err := s.entClient.ToolUsageEvent.Query().
				Where(toolusageevent.CommitCheckpointIDEQ(*snapshot.CommitCheckpointID)).
				Count(ctx)
			if err != nil {
				return nil, fmt.Errorf("evaluate PR freshness: count usage events: %w", err)
			}
			if count == 0 {
				item.Status = UsageStatusNoUsageEvents
				item.Reason = "Checkpoint exists but no usage events are bound to it."
				item.UsageEventFound = false
			} else if pr.UsageRefreshedAt != nil {
				newerCount, err := s.entClient.ToolUsageEvent.Query().
					Where(
						toolusageevent.CommitCheckpointIDEQ(*snapshot.CommitCheckpointID),
						toolusageevent.ObservedEndAtGT(*pr.UsageRefreshedAt),
					).
					Count(ctx)
				if err != nil {
					return nil, fmt.Errorf("evaluate PR freshness: count newer usage events: %w", err)
				}
				if newerCount > 0 {
					item.Status = UsageStatusStaleSnapshot
					item.Reason = "Usage events newer than the PR snapshot are bound to this checkpoint."
				}
			}
		}
		commits = append(commits, item)
		if item.Status != UsageStatusFresh && overall == UsageStatusFresh {
			overall = item.Status
			reason = item.Reason
		}
	}
	return &PRFreshness{Status: overall, Reason: reason, CheckedAt: checkedAt, Commits: commits}, nil
}
```

- [x] **Step 5: Run freshness tests**

Run:

```bash
cd backend && go test ./internal/prusage -run TestEvaluateCommitFreshness -count=1
```

Expected: PASS.

- [x] **Step 6: Add response shaping in PR handler**

In `backend/internal/handler/pr.go`, add response helper types:

```go
type prResponse struct {
	*ent.PrRecord
	UsageStatus          string                    `json:"usage_status"`
	UsageStatusReason    string                    `json:"usage_status_reason"`
	UsageStatusCheckedAt *time.Time                `json:"usage_status_checked_at,omitempty"`
	CommitFreshness      []prusage.CommitFreshness `json:"commit_freshness,omitempty"`
}
```

Add helper:

```go
func (h *PRHandler) buildPRResponse(ctx context.Context, pr *ent.PrRecord, includeCommits bool) any {
	resp := &prResponse{PrRecord: pr, UsageStatus: string(prusage.UsageStatusUnknown), UsageStatusReason: "Usage freshness has not been evaluated."}
	if h.usageRefresher == nil {
		return resp
	}
	if svc, ok := h.usageRefresher.(*prusage.Service); ok {
		freshness, err := svc.EvaluatePRFreshness(ctx, pr.ID)
		if err == nil && freshness != nil {
			resp.UsageStatus = string(freshness.Status)
			resp.UsageStatusReason = freshness.Reason
			resp.UsageStatusCheckedAt = &freshness.CheckedAt
			if includeCommits {
				resp.CommitFreshness = freshness.Commits
			}
		}
	}
	return resp
}
```

Use this helper in `ListByRepo`, `Get`, and `RefreshUsage`. For `ListByRepo`, map each PR row through `buildPRResponse(ctx, pr, false)`. For `Get` and `RefreshUsage`, use `includeCommits=true`.

- [x] **Step 7: Extend handler test for freshness fields**

Append to `backend/internal/handler/pr_usage_test.go`:

```go
func TestPRHandlerGetIncludesUsageFreshness(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(202).
		SetTitle("freshness").
		SetStatus("open").
		SaveX(context.Background())

	prHandler := NewPRHandler(env.client, repoSCM, nil, nil, prusage.NewService(env.client))
	api := env.router.Group("/api/v1/freshness-test")
	api.Use(auth.RequireAuth(env.authSvc))
	api.GET("/prs/:id", prHandler.Get)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/freshness-test/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["usage_status"] != "no_checkpoint" {
		t.Fatalf("usage_status = %v, want no_checkpoint", data["usage_status"])
	}
}
```

- [x] **Step 8: Run handler and prusage tests**

Run:

```bash
cd backend && go test ./internal/prusage ./internal/handler -run 'TestEvaluateCommitFreshness|TestPRHandlerGetIncludesUsageFreshness|TestPRHandlerRefreshUsage' -count=1
```

Expected: PASS.

- [x] **Step 9: Commit freshness backend**

```bash
git add backend/internal/prusage backend/internal/handler
git commit -m "feat(prsync): expose PR usage freshness"
```

---

## Task 5: Update Frontend API And Types

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`

- [ ] **Step 1: Write failing API tests**

In `frontend/src/__tests__/api-modules.test.ts`, update the PR API import:

```ts
import { listPRs, getPR, syncPRs, getPRSyncJob, settlePR, refreshPRUsage } from '@/api/pr'
```

Replace the sync test with:

```ts
it('syncPRs starts a PR sync job', async () => {
  mockClient.post.mockResolvedValue({ data: { data: { job_id: 44, status: 'queued', phase: 'queued' } } })
  await syncPRs(5)
  expect(mockClient.post).toHaveBeenCalledWith('/repos/5/sync-prs')
})

it('getPRSyncJob calls GET /pr-sync-jobs/:id', async () => {
  mockClient.get.mockResolvedValue({ data: { data: { id: 44, status: 'running', phase: 'fetching_prs' } } })
  await getPRSyncJob(44)
  expect(mockClient.get).toHaveBeenCalledWith('/pr-sync-jobs/44')
})
```

- [ ] **Step 2: Run frontend API tests and verify they fail**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts
```

Expected: FAIL because `getPRSyncJob` is not exported and `syncPRs` still passes a timeout argument.

- [ ] **Step 3: Add TypeScript types**

In `frontend/src/types/index.ts`, add:

```ts
export type UsageStatus =
  | 'fresh'
  | 'pending_upload'
  | 'no_checkpoint'
  | 'no_usage_events'
  | 'unbound'
  | 'stale_snapshot'
  | 'refresh_failed'
  | 'unknown'

export interface CommitFreshness {
  commit_sha: string
  usage_status: UsageStatus
  usage_status_reason: string
  checkpoint_found: boolean
  usage_event_found: boolean
}

export interface PRSyncJob {
  id: number
  repo_config_id: number
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'abandoned'
  phase: 'queued' | 'fetching_prs' | 'upserting_prs' | 'labeling' | 'refreshing_usage' | 'completed' | 'failed'
  current_page: number
  page_size: number
  fetched_prs: number
  total_prs: number
  processed_prs: number
  created_prs: number
  changed_prs: number
  unchanged_prs: number
  usage_total_prs: number
  usage_refreshed_prs: number
  usage_skipped_prs: number
  usage_failed_prs: number
  last_error?: string | null
}
```

Extend `PRRecord`:

```ts
usage_status?: UsageStatus
usage_status_reason?: string
usage_status_checked_at?: string | null
commit_freshness?: CommitFreshness[]
```

- [ ] **Step 4: Update PR API module**

In `frontend/src/api/pr.ts`:

```ts
import client from './client'
import type { ApiResponse, PRRecord, PRSyncJob } from '@/types'

export function listPRs(repoId: number, params?: { status?: string; limit?: number; offset?: number; months?: number }) {
  return client.get<ApiResponse<{ items: PRRecord[]; total: number }>>(`/repos/${repoId}/prs`, { params })
}

export function getPR(prId: number) {
  return client.get<ApiResponse<PRRecord>>(`/prs/${prId}`)
}

export function syncPRs(repoId: number) {
  return client.post<ApiResponse<{ job_id: number; status: string; phase: string; reused?: boolean }>>(`/repos/${repoId}/sync-prs`)
}

export function getPRSyncJob(jobId: number) {
  return client.get<ApiResponse<PRSyncJob>>(`/pr-sync-jobs/${jobId}`)
}

export function settlePR(prId: number) {
  return client.post<ApiResponse<{ attribution_status: string }>>(`/prs/${prId}/settle`)
}

export function refreshPRUsage(prId: number) {
  return client.post<ApiResponse<PRRecord>>(`/prs/${prId}/refresh-usage`)
}
```

- [ ] **Step 5: Run frontend API tests**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit frontend API types**

```bash
git add frontend/src/types/index.ts frontend/src/api/pr.ts frontend/src/__tests__/api-modules.test.ts
git commit -m "feat(frontend): add PR sync job API client"
```

---

## Task 6: Add Repo Detail Polling And Freshness UI

**Files:**
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

- [ ] **Step 1: Update frontend mocks for job API**

In `frontend/src/__tests__/repo-detail-view.test.ts`, update the `@/api/pr` mock:

```ts
vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(),
  getPR: vi.fn(),
  syncPRs: vi.fn(),
  getPRSyncJob: vi.fn(),
  refreshPRUsage: vi.fn(),
}))
```

- [ ] **Step 2: Write failing polling test**

Replace `shows PR sync result after syncing` with:

```ts
it('polls and shows PR sync job progress after syncing', async () => {
  vi.useFakeTimers()
  const { syncPRs, getPRSyncJob } = await import('@/api/pr')
  ;(syncPRs as any).mockResolvedValue({ data: { data: { job_id: 44, status: 'queued', phase: 'queued' } } })
  ;(getPRSyncJob as any)
    .mockResolvedValueOnce({ data: { data: { id: 44, status: 'running', phase: 'fetching_prs', current_page: 2, page_size: 100, fetched_prs: 200, total_prs: 0, processed_prs: 0, created_prs: 0, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 0, usage_refreshed_prs: 0, usage_skipped_prs: 0, usage_failed_prs: 0 } } })
    .mockResolvedValueOnce({ data: { data: { id: 44, status: 'completed', phase: 'completed', current_page: 2, page_size: 100, fetched_prs: 200, total_prs: 200, processed_prs: 200, created_prs: 2, changed_prs: 3, unchanged_prs: 195, usage_total_prs: 5, usage_refreshed_prs: 4, usage_skipped_prs: 1, usage_failed_prs: 0 } } })

  const { wrapper } = await mountRepoDetail()
  const syncButton = wrapper.findAll('button').find((b) => b.text() === 'Sync PRs')
  expect(syncButton).toBeTruthy()

  await syncButton!.trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Fetching PRs')
  expect(wrapper.text()).toContain('200 fetched')

  await vi.runOnlyPendingTimersAsync()
  await flushPromises()
  expect(wrapper.text()).toContain('Sync completed')
  expect(wrapper.text()).toContain('2 created')
  expect(wrapper.text()).toContain('3 changed')

  vi.useRealTimers()
})
```

- [ ] **Step 3: Write failing freshness badge test**

Append:

```ts
it('renders PR usage freshness badge and commit reason', async () => {
  const { wrapper } = await mountRepoDetail(undefined, undefined, {
    prs: [{
      id: 101,
      scm_pr_id: 88,
      scm_pr_url: 'https://github.com/org/repo-a/pull/88',
      author: 'alice',
      title: 'Missing checkpoint',
      source_branch: 'feat/a',
      target_branch: 'main',
      status: 'merged',
      labels: [],
      lines_added: 10,
      lines_deleted: 2,
      cycle_time_hours: 5,
      merged_at: '2026-03-30T00:00:00Z',
      created_at: '2026-03-29T00:00:00Z',
      usage_status: 'no_checkpoint',
      usage_status_reason: 'No checkpoint matched this PR commit.',
    }],
    getPRImpl: vi.fn(async () => ({
      data: {
        data: {
          ...detailFor(101).data.data,
          usage_status: 'no_checkpoint',
          usage_status_reason: 'No checkpoint matched this PR commit.',
          commit_freshness: [{
            commit_sha: 'abc123',
            usage_status: 'no_checkpoint',
            usage_status_reason: 'No checkpoint for this commit',
            checkpoint_found: false,
            usage_event_found: false,
          }],
        },
      },
    })),
  })

  expect(wrapper.text()).toContain('No checkpoint')
  const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
  await detailsButton!.trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('No checkpoint for this commit')
})
```

- [ ] **Step 4: Run repo detail tests and verify they fail**

Run:

```bash
cd frontend && pnpm test -- repo-detail-view.test.ts
```

Expected: FAIL because polling state and freshness rendering are not implemented.

- [ ] **Step 5: Add polling state and helpers**

In `frontend/src/views/repos/RepoDetailView.vue`, update imports:

```ts
import { getPR, getPRSyncJob, listPRs, refreshPRUsage, syncPRs } from '@/api/pr'
import type { CommitFreshness, PRCommitUsageSnapshot, PRRecord, PRSyncJob, RepoConfig, SCMProvider, UsageStatus } from '@/types'
```

Add refs:

```ts
const syncJob = ref<PRSyncJob | null>(null)
const syncPollTimer = ref<number | null>(null)
```

Add helpers:

```ts
function isTerminalJob(job: PRSyncJob) {
  return ['completed', 'failed', 'cancelled', 'abandoned'].includes(job.status)
}

function phaseLabel(phase?: string) {
  const labels: Record<string, string> = {
    queued: 'Queued',
    fetching_prs: 'Fetching PRs',
    upserting_prs: 'Updating PRs',
    labeling: 'Labeling PRs',
    refreshing_usage: 'Refreshing usage',
    completed: 'Completed',
    failed: 'Failed',
  }
  return phase ? labels[phase] ?? phase : 'Queued'
}

function usageStatusLabel(status?: UsageStatus) {
  const labels: Record<UsageStatus, string> = {
    fresh: 'Fresh',
    pending_upload: 'Pending',
    no_checkpoint: 'No checkpoint',
    no_usage_events: 'No usage',
    unbound: 'Unbound',
    stale_snapshot: 'Stale',
    refresh_failed: 'Failed',
    unknown: 'Unknown',
  }
  return labels[status ?? 'unknown']
}

function commitFreshnessFor(pr: PRRecord, commitSha: string): CommitFreshness | undefined {
  const detail = resolvedPR(pr)
  return detail.commit_freshness?.find((item) => item.commit_sha === commitSha)
}
```

- [ ] **Step 6: Implement job polling**

In `RepoDetailView.vue`, add:

```ts
async function pollSyncJob(jobId: number) {
  try {
    const res = await getPRSyncJob(jobId)
    const job = res.data.data ?? null
    syncJob.value = job
    if (!job) return
    if (isTerminalJob(job)) {
      if (syncPollTimer.value != null) {
        window.clearTimeout(syncPollTimer.value)
        syncPollTimer.value = null
      }
      syncing.value = false
      if (job.status === 'completed') {
        prsPage.value = 0
        await loadPRs()
        syncMessageTone.value = 'success'
        syncMessage.value = `Sync completed: ${formatCount(job.created_prs)} created, ${formatCount(job.changed_prs)} changed, ${formatCount(job.unchanged_prs)} unchanged`
      } else {
        syncMessageTone.value = 'error'
        syncMessage.value = job.last_error || `Sync ${job.status}`
      }
      return
    }
    syncPollTimer.value = window.setTimeout(() => pollSyncJob(job.id), 1500)
  } catch (error: any) {
    syncing.value = false
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || 'Failed to load PR sync progress'
  }
}
```

Update `handleSyncPRs`:

```ts
async function handleSyncPRs() {
  syncing.value = true
  syncMessage.value = ''
  syncJob.value = null
  if (syncPollTimer.value != null) {
    window.clearTimeout(syncPollTimer.value)
    syncPollTimer.value = null
  }
  try {
    const res = await syncPRs(repoId)
    const result = res.data.data
    if (!result?.job_id) {
      throw new Error('PR sync job was not created')
    }
    syncJob.value = {
      id: result.job_id,
      repo_config_id: repoId,
      status: result.status as PRSyncJob['status'],
      phase: result.phase as PRSyncJob['phase'],
      current_page: 0,
      page_size: 100,
      fetched_prs: 0,
      total_prs: 0,
      processed_prs: 0,
      created_prs: 0,
      changed_prs: 0,
      unchanged_prs: 0,
      usage_total_prs: 0,
      usage_refreshed_prs: 0,
      usage_skipped_prs: 0,
      usage_failed_prs: 0,
    }
    await pollSyncJob(result.job_id)
  } catch (error: any) {
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || error?.message || 'Failed to sync PRs'
    syncing.value = false
  }
}
```

Add cleanup:

```ts
onUnmounted(() => {
  if (syncPollTimer.value != null) {
    window.clearTimeout(syncPollTimer.value)
  }
})
```

- [ ] **Step 7: Render progress and badges**

Add a status block below the sync button:

```vue
<div v-if="syncJob" class="rounded-md bg-blue-50 p-3 text-sm text-blue-900">
  <div class="font-medium">{{ phaseLabel(syncJob.phase) }}</div>
  <div class="mt-1 text-xs text-blue-800">
    {{ formatCount(syncJob.fetched_prs) }} fetched ·
    {{ formatCount(syncJob.processed_prs) }} processed ·
    {{ formatCount(syncJob.usage_refreshed_prs) }} usage refreshed ·
    {{ formatCount(syncJob.usage_skipped_prs) }} skipped ·
    {{ formatCount(syncJob.usage_failed_prs) }} failed
  </div>
</div>
```

Add a `Usage` column next to `Status`:

```vue
<th class="px-3 py-2 text-left font-medium">Usage</th>
```

Render badge:

```vue
<td class="px-3 py-2">
  <span class="inline-flex rounded-full bg-gray-50 px-2 text-xs font-medium leading-5 text-gray-600" :title="pr.usage_status_reason || ''">
    {{ usageStatusLabel(pr.usage_status) }}
  </span>
</td>
```

Increase expanded-row `colspan` by one.

In commit rows, add a `Usage Status` column and cell:

```vue
<th class="px-2 py-1 text-left font-medium">Usage Status</th>
```

```vue
<td class="px-2 py-1 text-gray-700">
  {{ commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status_reason || usageStatusLabel(commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status) }}
</td>
```

- [ ] **Step 8: Run repo detail tests**

Run:

```bash
cd frontend && pnpm test -- repo-detail-view.test.ts
```

Expected: PASS.

- [ ] **Step 9: Run frontend test suite**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [ ] **Step 10: Commit frontend UI**

```bash
git add frontend/src/views/repos/RepoDetailView.vue frontend/src/__tests__/repo-detail-view.test.ts
git commit -m "feat(frontend): show PR sync progress and usage freshness"
```

---

## Task 7: Update Architecture Docs And Run Full Verification

**Files:**
- Modify: `docs/architecture.md`

- [ ] **Step 1: Update architecture current-state docs**

In `docs/architecture.md`, update the PR sync / repo detail section to state:

```markdown
`POST /api/v1/repos/:id/sync-prs` creates a backend `pr_sync_jobs` record and the backend process performs PR metadata sync plus active PR usage refresh asynchronously. The repo detail frontend polls `GET /api/v1/pr-sync-jobs/:id` for phase and counter progress, then reloads `GET /api/v1/repos/:id/prs` when the job completes. PR usage numbers still come from `tool_usage_events -> commit_checkpoints -> pr_commit_usage_snapshots`; freshness fields explain missing or stale usage without counting unbound evidence as valid PR usage.
```

Place this near the existing repo/PR usage architecture description.

- [ ] **Step 2: Run backend focused verification**

Run:

```bash
cd backend && go test ./internal/prsync ./internal/prusage ./internal/handler
```

Expected: PASS.

- [ ] **Step 3: Regenerate Ent and check no drift**

Run:

```bash
cd backend && go generate ./ent
git diff --exit-code backend/ent backend/ent/migrate/schema.go
```

Expected: both commands exit 0. If `git diff --exit-code` fails, inspect generated changes, stage legitimate generated files, and rerun the check.

- [ ] **Step 4: Run full backend tests**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS. If local PostgreSQL-backed tests require the known local DSN, rerun with:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./...
```

- [ ] **Step 5: Run frontend tests**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [ ] **Step 6: Inspect final diff**

Run:

```bash
git status --short
git diff --stat
```

Expected: only intended backend, frontend, generated Ent, and docs files are modified.

- [ ] **Step 7: Commit docs and final integration changes**

If Task 7 only changed docs:

```bash
git add docs/architecture.md
git commit -m "docs(architecture): document PR sync jobs"
```

If Task 7 also contains final generated or integration fixes:

```bash
git add backend frontend docs/architecture.md
git commit -m "feat(prsync): integrate PR sync job progress"
```

Use the first command when only docs changed; use the second when final integration fixes are present.

---

## Completion Checklist

- [ ] `POST /api/v1/repos/:id/sync-prs` returns a job id immediately.
- [ ] `GET /api/v1/pr-sync-jobs/:id` returns phase and counters.
- [ ] Duplicate sync clicks reuse the existing queued or running job.
- [ ] PR fetch progress is updated page by page.
- [ ] Upsert distinguishes created, changed, and unchanged PRs.
- [ ] Usage refresh skips unchanged inactive PRs and reports skip counts.
- [ ] PR list includes PR-level usage freshness status.
- [ ] PR details include commit-level freshness reasons.
- [ ] Existing PR list pagination and month filters still work.
- [ ] Existing single-PR `refresh-usage` still works.
- [ ] `docs/architecture.md` reflects the implemented runtime relationship.
- [ ] Backend and frontend verification commands pass or any environment-sensitive failures are recorded with exact output.
