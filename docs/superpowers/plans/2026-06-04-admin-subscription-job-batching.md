# Admin Subscription Job Batching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace synchronous admin sub2api subscription batch execution with a persisted asynchronous job flow so large selected-user operations do not hit the frontend 15s request timeout.

**Architecture:** The frontend submits the chosen subscription operation and receives a job `id` immediately. The backend stores the request, target snapshot, progress counters, and per-user results in a new Ent-backed job table, then processes relay/sub2api mutations in a background goroutine using the existing relay provider interfaces. The admin users page polls the job endpoint, renders progress and final per-user results, and does not extend the global axios timeout.

**Tech Stack:** Go, Gin, Ent, SQLite/PostgreSQL auto-migration, Vue 3, Vite/Vitest, TailwindCSS, existing `backend/internal/relay.Provider` adapter interfaces.

**Status:** Completed and verified. The previous frontend-only timeout patch was reverted; the implementation now uses async job processing.

---

## File Map

- Modify: `docs/superpowers/specs/2026-06-04-admin-sub2api-subscription-assignment-design.md`
  Update the active contract from synchronous `/subscriptions/batch` to async subscription jobs.
- Modify: `docs/architecture.md`
  Mention the new admin subscription job boundary if the backend job module/table lands.
- Create: `backend/ent/schema/admin_subscription_job.go`
  Persist admin subscription job request, target snapshot, status, progress counters, JSON result rows, error text, and timestamps.
- Regenerate: `backend/ent/**`, `backend/ent/migrate/schema.go`
  Generated Ent model/query/update/migration code for `AdminSubscriptionJob`.
- Create: `backend/internal/adminsubscription/job.go`
  Service that creates jobs, resolves target users, updates progress, runs relay mutations, and reads jobs.
- Create: `backend/internal/adminsubscription/job_test.go`
  Unit tests for job creation, progress, skipped users, per-user failures, completion, and oversized target rejection.
- Modify: `backend/internal/handler/admin_users.go`
  Route handlers for starting subscription jobs and reading job progress; keep the old synchronous compatibility endpoint for now.
- Modify: `backend/internal/handler/router.go`
  Wire the new job service/handlers under admin routes.
- Modify: `backend/internal/handler/admin_users_subscription_test.go`
  HTTP tests for `POST /admin/users/subscription-jobs` and `GET /admin/users/subscription-jobs/:id`.
- Modify: `backend/cmd/server/main.go`
  Wire the new service into `handler.SetupRouter` if router construction needs an explicit dependency.
- Modify: `frontend/src/types/index.ts`
  Add `AdminSubscriptionJob`, status/phase types, and start-job response types.
- Modify: `frontend/src/api/adminUsers.ts`
  Add `startAdminUserSubscriptionJob`, `getAdminUserSubscriptionJob`, and optional latest active job read. Do not add timeout overrides.
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
  Submit jobs, poll progress, recover active/latest job on mount, render counters and per-user results.
- Modify: `frontend/src/i18n.ts`
  Add English/Chinese labels for queued/running/completed/failed progress and result summary.
- Modify: `frontend/src/__tests__/admin-users-view.test.ts`
  Update mocked admin API calls and view tests to assert job start + polling behavior.
- Modify: `frontend/src/__tests__/api-modules.test.ts`
  Add API wrapper tests for the new job endpoints and assert no timeout override is used.

## Task 1: Update the Contract Before Code

**Files:**
- Modify: `docs/superpowers/specs/2026-06-04-admin-sub2api-subscription-assignment-design.md`
- Modify: `docs/architecture.md`

- [x] **Step 1: Change the spec status and non-goal**

Replace the status/header wording so it no longer claims synchronous batch is the current durable contract:

```md
**Status:** Implemented with async subscription-job contract update in progress
```

Replace the old non-goal:

```md
No async job table for subscription batches; the implemented batch API is synchronous, capped at 500 target users per request, and returns per-user results.
```

with:

```md
No frontend timeout extension as the durability mechanism. Large subscription operations run as backend jobs and expose progress through polling endpoints.
```

- [x] **Step 2: Add the async backend contract**

Document these endpoints in the spec:

```text
POST /api/v1/admin/users/subscription-jobs
GET /api/v1/admin/users/subscription-jobs/:id
GET /api/v1/admin/users/subscription-jobs/latest
```

Define response fields:

```json
{
  "id": 12,
  "status": "queued",
  "phase": "queued",
  "scope": "selected",
  "operation": "add",
  "provider_id": 1,
  "group_id": "42",
  "total_count": 30,
  "processed_count": 0,
  "success_count": 0,
  "skipped_count": 0,
  "failed_count": 0,
  "results": []
}
```

- [x] **Step 3: Preserve compatibility in the spec**

Document that `POST /api/v1/admin/users/subscriptions/batch` remains a compatibility endpoint for small synchronous callers, but the frontend uses subscription jobs.

- [x] **Step 4: Update architecture**

In `docs/architecture.md`, add a short note under backend admin/user management that admin subscription jobs are persisted local backend jobs and all relay mutations still go through `backend/internal/relay.Provider`.

## Task 2: Add Ent Schema and Generate Code

**Files:**
- Create: `backend/ent/schema/admin_subscription_job.go`
- Regenerate: `backend/ent/**`
- Regenerate: `backend/ent/migrate/schema.go`

- [x] **Step 1: Add the schema**

Create `backend/ent/schema/admin_subscription_job.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AdminSubscriptionJob struct {
	ent.Schema
}

func (AdminSubscriptionJob) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("status").
			Values("queued", "running", "completed", "failed", "abandoned").
			Default("queued"),
		field.Enum("phase").
			Values("queued", "resolving_targets", "processing", "completed", "failed").
			Default("queued"),
		field.Enum("scope").
			Values("selected", "current_filter", "all_mapped"),
		field.Enum("operation").
			Values("add", "extend", "remove"),
		field.Int("provider_id"),
		field.String("group_id"),
		field.Int("validity_days").Optional(),
		field.Int("days").Optional(),
		field.String("filter_query").Optional(),
		field.JSON("target_user_ids", []int{}).Optional(),
		field.JSON("requested_user_ids", []int{}).Optional(),
		field.Int("total_count").Default(0),
		field.Int("processed_count").Default(0),
		field.Int("success_count").Default(0),
		field.Int("skipped_count").Default(0),
		field.Int("failed_count").Default(0),
		field.JSON("results", []map[string]any{}).Optional(),
		field.String("last_error").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AdminSubscriptionJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
		index.Fields("created_at"),
	}
}
```

- [x] **Step 2: Generate Ent code**

Run:

```bash
cd backend && go generate ./ent
```

Expected: Ent generates `AdminSubscriptionJob` code and updates migration schema without errors.

- [x] **Step 3: Verify generated code compiles**

Run:

```bash
cd backend && go test ./ent/...
```

Expected: generated Ent packages compile.

## Task 3: Backend Job Service

**Files:**
- Create: `backend/internal/adminsubscription/job.go`
- Create: `backend/internal/adminsubscription/job_test.go`

- [x] **Step 1: Write failing service tests**

Create tests covering:

1. `StartJob` snapshots selected local users and returns a queued job without relay mutation.
2. `RunJob` skips unmapped local users and continues later users.
3. `RunJob` records relay mutation errors as per-user failed rows.
4. `RunJob` completes with correct processed/success/skipped/failed counters.
5. `StartJob` rejects more than 500 targets before creating a job.

Run:

```bash
cd backend && go test ./internal/adminsubscription
```

Expected before implementation: compile failure because the package/service does not exist.

- [x] **Step 2: Implement request/result types**

Create exported service types:

```go
type StartJobRequest struct {
	Scope        string
	UserIDs      []int
	FilterQuery  string
	Operation    string
	ProviderID   int
	GroupID      string
	ValidityDays int
	Days         int
}

type ResultRow struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	RelayUserID *int   `json:"relay_user_id,omitempty"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}
```

- [x] **Step 3: Implement `StartJob`**

`StartJob(ctx, req)` validates the same scope/operation/provider/group/day rules as the current handler, resolves and snapshots targets using local `users`, enforces the 500-target cap, creates an `AdminSubscriptionJob` row with `status=queued`, `phase=queued`, counters initialized, and returns the job.

- [x] **Step 4: Implement `RunJob`**

`RunJob(ctx, jobID, provider)` reloads the job, sets `running/processing`, iterates target users in snapshot order, applies the existing relay operation, appends one result row per target, updates counters after each row, and marks `completed/completed` unless a whole-job setup error occurs.

- [x] **Step 5: Implement read helpers**

Add `GetJob(ctx, id)` and `GetLatestJob(ctx)` returning Ent jobs. `GetLatestJob` returns the latest queued/running job first, otherwise the newest job.

- [x] **Step 6: Run service tests**

Run:

```bash
cd backend && go test ./internal/adminsubscription
```

Expected: PASS.

## Task 4: Backend HTTP API

**Files:**
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/admin_users_subscription_test.go`
- Modify: `backend/cmd/server/main.go` if router dependency changes require server wiring

- [x] **Step 1: Write failing handler tests**

Add tests:

1. `POST /admin/users/subscription-jobs` returns `job_id`, `status=queued`, and does not wait for relay mutation.
2. `GET /admin/users/subscription-jobs/:id` returns progress counters and result rows.
3. `GET /admin/users/subscription-jobs/latest` returns the latest job.

Run:

```bash
cd backend && go test ./internal/handler -run 'AdminUsers.*Subscription.*Job|SubscriptionJob'
```

Expected before handler implementation: route/handler failures.

- [x] **Step 2: Add job handlers**

Add methods on `AdminUsersHandler`:

```go
func (h *AdminUsersHandler) StartSubscriptionJob(c *gin.Context)
func (h *AdminUsersHandler) GetSubscriptionJob(c *gin.Context)
func (h *AdminUsersHandler) GetLatestSubscriptionJob(c *gin.Context)
```

`StartSubscriptionJob` decodes the existing request shape, starts the job, resolves the provider, launches `go service.RunJob(context.Background(), job.ID, provider)`, and returns job metadata immediately.

- [x] **Step 3: Wire routes**

Under `/api/v1/admin/users`:

```go
adminUsersGroup.POST("/subscription-jobs", adminUsersHandler.StartSubscriptionJob)
adminUsersGroup.GET("/subscription-jobs/latest", adminUsersHandler.GetLatestSubscriptionJob)
adminUsersGroup.GET("/subscription-jobs/:id", adminUsersHandler.GetSubscriptionJob)
```

- [x] **Step 4: Preserve the old synchronous endpoint**

Keep `POST /admin/users/subscriptions/batch` wired to `ManageSubscriptions` for compatibility. The frontend will stop calling it.

- [x] **Step 5: Run handler tests**

Run:

```bash
cd backend && go test ./internal/handler -run 'AdminUsers.*Subscription.*Job|SubscriptionJob'
```

Expected: PASS.

## Task 5: Frontend API and Types

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/adminUsers.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`

- [x] **Step 1: Add failing API tests**

In `frontend/src/__tests__/api-modules.test.ts`, assert:

```ts
await startAdminUserSubscriptionJob(payload)
expect(mockClient.post).toHaveBeenCalledWith('/admin/users/subscription-jobs', payload)

await getAdminUserSubscriptionJob(12)
expect(mockClient.get).toHaveBeenCalledWith('/admin/users/subscription-jobs/12')

await getLatestAdminUserSubscriptionJob()
expect(mockClient.get).toHaveBeenCalledWith('/admin/users/subscription-jobs/latest')
```

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts
```

Expected before implementation: import/function failures.

- [x] **Step 2: Add frontend types**

Add:

```ts
export type AdminSubscriptionJobStatus = 'queued' | 'running' | 'completed' | 'failed' | 'abandoned'
export type AdminSubscriptionJobPhase = 'queued' | 'resolving_targets' | 'processing' | 'completed' | 'failed'

export interface AdminSubscriptionJob {
  id: number
  status: AdminSubscriptionJobStatus
  phase: AdminSubscriptionJobPhase
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  provider_id: number
  group_id: string
  total_count: number
  processed_count: number
  success_count: number
  skipped_count: number
  failed_count: number
  results: AdminManageSubscriptionsResultRow[]
  last_error?: string | null
}
```

- [x] **Step 3: Add API functions**

In `frontend/src/api/adminUsers.ts`, add:

```ts
export function startAdminUserSubscriptionJob(data: AdminManageSubscriptionsRequest) {
  return client.post<ApiResponse<AdminSubscriptionJob>>('/admin/users/subscription-jobs', data)
}

export function getAdminUserSubscriptionJob(id: number) {
  return client.get<ApiResponse<AdminSubscriptionJob>>(`/admin/users/subscription-jobs/${id}`)
}

export function getLatestAdminUserSubscriptionJob() {
  return client.get<ApiResponse<AdminSubscriptionJob | null>>('/admin/users/subscription-jobs/latest')
}
```

- [x] **Step 4: Run API tests**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts
```

Expected: PASS.

## Task 6: Frontend Polling UI

**Files:**
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/admin-users-view.test.ts`

- [x] **Step 1: Write failing view tests**

Update existing subscription tests so submit starts a job instead of waiting for sync results. Add a polling test:

1. Click Apply.
2. Expect `startAdminUserSubscriptionJob` called with the current payload.
3. Mock `getAdminUserSubscriptionJob` first as running, then completed.
4. Expect the UI to show progress and final result summary.

Run:

```bash
cd frontend && pnpm test -- admin-users-view.test.ts
```

Expected before implementation: tests fail because the view still calls `manageAdminUserSubscriptions`.

- [x] **Step 2: Replace sync submit with job submit**

In `AdminUsersView.vue`, replace `manageAdminUserSubscriptions` usage with `startAdminUserSubscriptionJob`, store `subscriptionJob`, set `loading=true`, and start polling every 1500ms.

- [x] **Step 3: Add polling lifecycle**

Add:

```ts
const subscriptionJob = ref<AdminSubscriptionJob | null>(null)
const subscriptionJobPollTimer = ref<number | null>(null)
```

Clear the poll timer in `onBeforeUnmount`.

- [x] **Step 4: Recover latest job on mount**

Call `getLatestAdminUserSubscriptionJob()` on mount. If the latest job is queued/running, resume polling and show it in the panel.

- [x] **Step 5: Render progress**

Render processed/total and success/skipped/failed counts from `subscriptionJob`. Keep the existing per-user result cards, but source them from `subscriptionJob.results` once a job exists.

- [x] **Step 6: Run view tests**

Run:

```bash
cd frontend && pnpm test -- admin-users-view.test.ts
```

Expected: PASS.

## Task 7: Full Verification

**Files:**
- All changed backend/frontend/docs files

- [x] **Step 1: Run backend targeted tests**

Run:

```bash
cd backend && go test ./internal/adminsubscription ./internal/handler
```

Expected: PASS.

- [x] **Step 2: Run frontend targeted tests**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts admin-users-view.test.ts
```

Expected: PASS.

- [x] **Step 3: Run default backend tests if time allows**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS. If DB-dependent tests need local Postgres, use:

```bash
AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./...
```

- [x] **Step 4: Run frontend default tests**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [x] **Step 5: Check worktree**

Run:

```bash
git status --short --branch
git diff --check
```

Expected: only intentional implementation/doc changes; no whitespace errors.
