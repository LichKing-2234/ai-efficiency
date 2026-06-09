# Global Tool Usage Events Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global `/events` page that shows backend-ingested `tool_usage_events` with summary cards, filtered event rows, and an admin-capable detail drawer.

**Architecture:** Backend adds read-only event query endpoints for summary, list, and detail, with strict user scoping and admin-only raw-field access. Frontend adds a new `/events` route, a dedicated events API module, and an `EventsView` page that loads summary and list separately, then fetches detail lazily into a right-side drawer.

**Tech Stack:** Go (Gin, Ent), Vue 3 (`<script setup>`, Vite, TailwindCSS, Pinia auth store), Vitest, Go test

**Spec:** `docs/superpowers/specs/2026-05-21-global-tool-usage-events-page-design.md`

**Status:** Current `/events` contract verified on 2026-06-06: implementation/spec alignment completed, focused automated tests passed, and manual browser-level verification completed against an isolated local backend/frontend. Historical commit checklist items remain unchecked because this replay did not create commits.

**Known Remaining Gaps:** None for the current `/events` UI/API verification. This replay intentionally used a temp local backend (`127.0.0.1:8081`) and temp `ae-cli` HOME because the operator's real CLI config points at production.

---

## File Structure

### Backend — Create

| File | Responsibility |
| --- | --- |
| `backend/internal/toolusage/query.go` | Event summary/list/detail read models, filters, user scoping, commit -> PR reverse lookup |
| `backend/internal/toolusage/query_test.go` | Query-layer tests for scoping, filtering, binding status, and detail redaction |
| `backend/internal/handler/events.go` | HTTP handlers for `/api/v1/events`, `/api/v1/events/summary`, `/api/v1/events/:id` |
| `backend/internal/handler/events_test.go` | Handler tests for auth, filtering, and admin/user response shape |

### Backend — Modify

| File | Responsibility |
| --- | --- |
| `backend/internal/handler/router.go` | Register the new protected `/events` routes |

### Frontend — Create

| File | Responsibility |
| --- | --- |
| `frontend/src/api/events.ts` | Events summary/list/detail HTTP API helpers |
| `frontend/src/views/events/EventsView.vue` | `/events` page with summary cards, filters, event table, and right drawer |
| `frontend/src/__tests__/api-events.test.ts` | API helper tests |
| `frontend/src/__tests__/events-view.test.ts` | Page-level tests for loading, filters, drawer, and admin/user differences |

### Frontend — Modify

| File | Responsibility |
| --- | --- |
| `frontend/src/router/index.ts` | Register `/events` route |
| `frontend/src/types/index.ts` | Add events summary/list/detail DTOs |
| `frontend/src/components/AppSidebar.vue` | Add navigation entry for `/events` |

### Docs — Modify

| File | Responsibility |
| --- | --- |
| `docs/architecture.md` | Reflect the new global `/events` surface and backend event read APIs |

---

### Task 1: Backend Event Query Service

**Files:**
- Create: `backend/internal/toolusage/query.go`
- Test: `backend/internal/toolusage/query_test.go`

- [x] **Step 1: Write the failing query-layer tests**

```go
func TestListEventsScopesRegularUserToOwnRows(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, dialect.SQLite, "file:toolusage-list-own?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleAdmin).
		SaveX(ctx)
	viewer := client.User.Create().
		SetUsername("viewer").
		SetEmail("viewer@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleViewer).
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetName("ai-efficiency").
		SetFullName("LichKing-2234/ai-efficiency").
		SetCloneURL("https://github.com/LichKing-2234/ai-efficiency.git").
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetUserID(viewer.ID).
		SetToolSessionID("sess-viewer").
		SetDedupeKey("viewer-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(time.Now().Add(-5 * time.Minute)).
		SetObservedEndAt(time.Now().Add(-5 * time.Minute)).
		ExecX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-2").
		SetRepoConfigID(repo.ID).
		SetUserID(admin.ID).
		SetToolSessionID("sess-admin").
		SetDedupeKey("admin-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(time.Now().Add(-4 * time.Minute)).
		SetObservedEndAt(time.Now().Add(-4 * time.Minute)).
		ExecX(ctx)

	svc := toolusage.NewQueryService(client)
	rows, total, err := svc.ListEvents(ctx, toolusage.ListEventsRequest{
		ActorUserID: viewer.ID,
		ActorRole:   "viewer",
		From:        time.Now().Add(-24 * time.Hour),
		To:          time.Now(),
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("got total=%d rows=%d, want 1/1", total, len(rows))
	}
	if rows[0].ToolSessionID != "sess-viewer" {
		t.Fatalf("row session=%q, want sess-viewer", rows[0].ToolSessionID)
	}
}

func TestGetEventDetailRedactsRawFieldsForRegularUser(t *testing.T) {
	// Seed one event with raw_source_path/raw_payload, then assert
	// non-admin detail hides those fields while admin detail returns them.
}
```

- [x] **Step 2: Run query tests to verify they fail**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestListEventsScopesRegularUserToOwnRows|TestGetEventDetailRedactsRawFieldsForRegularUser' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/toolusage
... undefined: toolusage.NewQueryService
```

- [x] **Step 3: Implement the query service and DTOs**

```go
type SummaryResponse struct {
	TotalEvents  int            `json:"total_events"`
	BoundEvents  int            `json:"bound_events"`
	UnboundEvents int           `json:"unbound_events"`
	ToolCounts   []ToolCountDTO `json:"tool_counts"`
}

type EventListRow struct {
	ID                int       `json:"id"`
	Tool              string    `json:"tool"`
	RepoID            int       `json:"repo_id"`
	RepoName          string    `json:"repo_name"`
	Username          string    `json:"username,omitempty"`
	ToolSessionID     string    `json:"tool_session_id"`
	ToolEventID       string    `json:"tool_event_id,omitempty"`
	DedupeKey         string    `json:"dedupe_key"`
	ObservedEndAt     time.Time `json:"observed_end_at"`
	RequestCount      int       `json:"request_count"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens"`
	ReasoningTokens   int64     `json:"reasoning_tokens"`
	CreditUsage       float64   `json:"credit_usage"`
	CommitCheckpointID *int     `json:"commit_checkpoint_id,omitempty"`
	CommitSHA         string    `json:"commit_sha,omitempty"`
	SourceBasename    string    `json:"source_basename"`
	BindingStatus     string    `json:"binding_status"`
}

func NewQueryService(entClient *ent.Client) *QueryService { ... }

func (s *QueryService) ListEvents(ctx context.Context, req ListEventsRequest) ([]EventListRow, int, error) {
	// 1. Apply actor scoping: non-admin forced to req.ActorUserID
	// 2. Apply filters: time range, tool, repo, binding status, q
	// 3. Join repo and optional checkpoint
	// 4. Build lightweight rows without raw payload
}

func (s *QueryService) GetEventDetail(ctx context.Context, req GetEventDetailRequest) (*EventDetail, error) {
	// 1. Load event with repo + optional checkpoint
	// 2. Enforce actor scoping
	// 3. Reverse lookup matched PRs by commit_sha via pr_commit_usage_snapshots
	// 4. Redact raw_source_path/raw_payload for non-admin
}
```

- [x] **Step 4: Run backend query tests and a package sweep**

Run:

```bash
cd backend && go test ./internal/toolusage -count=1 && go test ./internal/toolusage ./internal/prusage -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/toolusage
ok      github.com/ai-efficiency/backend/internal/prusage
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/toolusage/query.go backend/internal/toolusage/query_test.go
git commit -m "feat(backend): add tool usage event query service"
```

---

### Task 2: Backend Events HTTP API

**Files:**
- Create: `backend/internal/handler/events.go`
- Test: `backend/internal/handler/events_test.go`
- Modify: `backend/internal/handler/router.go`

- [x] **Step 1: Write failing handler tests for summary, list, and detail**

```go
func TestEventsListRequiresAuthAndScopesNonAdmin(t *testing.T) {
	// 1. Build router with auth middleware + events handler
	// 2. Issue request as regular user with ?user_id=<other>
	// 3. Assert response still only contains own events
}

func TestEventDetailReturnsRawFieldsOnlyForAdmin(t *testing.T) {
	// Seed one event with raw fields, call detail as viewer/admin, assert response difference.
}
```

- [x] **Step 2: Run handler tests to verify they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestEventsListRequiresAuthAndScopesNonAdmin|TestEventDetailReturnsRawFieldsOnlyForAdmin' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/handler
... undefined: NewEventsHandler
```

- [x] **Step 3: Implement events handler and wire routes**

```go
type EventsHandler struct {
	service *toolusage.QueryService
}

func NewEventsHandler(service *toolusage.QueryService) *EventsHandler { ... }

func (h *EventsHandler) Summary(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil { pkg.Error(c, http.StatusUnauthorized, "unauthorized"); return }
	req := parseEventsQuery(c, uc)
	summary, err := h.service.GetSummary(c.Request.Context(), req)
	...
	pkg.Success(c, summary)
}

func (h *EventsHandler) List(c *gin.Context) { ... }
func (h *EventsHandler) Get(c *gin.Context) { ... }
```

Router registration:

```go
eventsHandler := NewEventsHandler(toolusage.NewQueryService(entClient))

eventsGroup := protected.Group("/events")
{
	eventsGroup.GET("/summary", eventsHandler.Summary)
	eventsGroup.GET("", eventsHandler.List)
	eventsGroup.GET("/:id", eventsHandler.Get)
}
```

- [x] **Step 4: Run handler tests and targeted backend integration tests**

Run:

```bash
cd backend && go test ./internal/handler -count=1 && go test ./... -run 'TestEvents|TestPRUsage' -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/handler
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/events.go backend/internal/handler/events_test.go backend/internal/handler/router.go
git commit -m "feat(backend): add events read APIs"
```

---

### Task 3: Frontend Events API, Types, and Route

**Files:**
- Create: `frontend/src/api/events.ts`
- Create: `frontend/src/__tests__/api-events.test.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/AppSidebar.vue`

- [x] **Step 1: Write failing frontend API and route tests**

```ts
it('listEvents calls GET /events with query params', async () => {
  mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
  await listEvents({ tool: 'claude', limit: 20 })
  expect(mockClient.get).toHaveBeenCalledWith('/events', { params: { tool: 'claude', limit: 20 } })
})

it('registers the /events route', async () => {
  const routes = router.getRoutes().map((route) => route.path)
  expect(routes).toContain('/events')
})
```

- [x] **Step 2: Run the failing frontend tests**

Run:

```bash
cd frontend && pnpm exec vitest run src/__tests__/api-events.test.ts src/__tests__/router.test.ts
```

Expected:

```text
FAIL  src/__tests__/api-events.test.ts
Error: Cannot find module '@/api/events'
```

- [x] **Step 3: Implement DTOs, API helpers, route, and nav entry**

```ts
export interface ToolUsageEventRow {
  id: number
  tool: string
  repo_id: number
  repo_name: string
  username?: string
  tool_session_id: string
  tool_event_id?: string | null
  dedupe_key: string
  observed_end_at: string
  request_count: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  reasoning_tokens: number
  credit_usage: number
  commit_checkpoint_id?: number | null
  commit_sha?: string | null
  source_basename: string
  binding_status: 'bound' | 'unbound'
}

export function listEvents(params: Record<string, unknown>) {
  return client.get<ApiResponse<PagedResponse<ToolUsageEventRow>>>('/events', { params })
}

export function getEventSummary(params: Record<string, unknown>) {
  return client.get<ApiResponse<ToolUsageEventSummary>>('/events/summary', { params })
}

export function getEventDetail(id: number) {
  return client.get<ApiResponse<ToolUsageEventDetail>>(`/events/${id}`)
}
```

Router entry:

```ts
{
  path: '/events',
  name: 'Events',
  component: () => import('@/views/events/EventsView.vue'),
}
```

- [x] **Step 4: Run the API/route tests**

Run:

```bash
cd frontend && pnpm exec vitest run src/__tests__/api-events.test.ts src/__tests__/router.test.ts
```

Expected:

```text
✓ src/__tests__/api-events.test.ts
✓ src/__tests__/router.test.ts
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/events.ts frontend/src/__tests__/api-events.test.ts frontend/src/types/index.ts frontend/src/router/index.ts frontend/src/components/AppSidebar.vue
git commit -m "feat(frontend): add events route and API client"
```

---

### Task 4: Events Page UI and Drawer

**Files:**
- Create: `frontend/src/views/events/EventsView.vue`
- Create: `frontend/src/__tests__/events-view.test.ts`

- [x] **Step 1: Write failing view tests**

```ts
it('loads summary and event rows on mount with a 24h default range', async () => {
  eventsApi.getEventSummary.mockResolvedValue({ data: { data: { total_events: 2, bound_events: 1, unbound_events: 1, tool_counts: [] } } })
  eventsApi.listEvents.mockResolvedValue({ data: { data: { items: [sampleRow], total: 1, page: 0, page_size: 20 } } })

  render(EventsView)
  expect(await screen.findByText('Total Events')).toBeInTheDocument()
  expect(await screen.findByText(sampleRow.source_basename)).toBeInTheDocument()
})

it('opens the detail drawer and hides raw payload for non-admin', async () => {
  authStore.isAdmin = false
  eventsApi.getEventDetail.mockResolvedValue({ data: { data: sampleDetailWithoutRaw } })
  render(EventsView)
  await user.click(await screen.findByText(sampleRow.source_basename))
  expect(await screen.findByText('Binding')).toBeInTheDocument()
  expect(screen.queryByText('Raw Payload')).toBeNull()
})
```

- [x] **Step 2: Run the failing page tests**

Run:

```bash
cd frontend && pnpm exec vitest run src/__tests__/events-view.test.ts
```

Expected:

```text
FAIL  src/__tests__/events-view.test.ts
Error: Cannot find module '@/views/events/EventsView.vue'
```

- [x] **Step 3: Implement the `/events` page**

```vue
<script setup lang="ts">
const filters = reactive({
  from: defaultFrom24h(),
  to: defaultNow(),
  tool: '',
  repo_id: '',
  binding_status: '',
  user_id: '',
  q: '',
  limit: 20,
  offset: 0,
})

const summary = ref<ToolUsageEventSummary | null>(null)
const rows = ref<ToolUsageEventRow[]>([])
const selectedEventId = ref<number | null>(null)
const selectedEvent = ref<ToolUsageEventDetail | null>(null)

async function loadPage() {
  const [summaryRes, listRes] = await Promise.all([
    getEventSummary(toQuery(filters)),
    listEvents(toQuery(filters)),
  ])
  summary.value = summaryRes.data.data ?? null
  rows.value = listRes.data.data?.items ?? []
}

async function openDetail(id: number) {
  selectedEventId.value = id
  const res = await getEventDetail(id)
  selectedEvent.value = res.data.data ?? null
}
</script>
```

Template sections:

```vue
<div class="grid gap-4 sm:grid-cols-4">
  <SummaryCard title="Total Events" :value="summary?.total_events" />
  <SummaryCard title="Bound to Commit" :value="summary?.bound_events" />
  <SummaryCard title="Unbound" :value="summary?.unbound_events" />
  <SummaryCard title="Tools" :value="summary?.tool_counts.length" />
</div>

<table>
  <tr v-for="row in rows" :key="row.id" @click="openDetail(row.id)">
    <td>{{ formatDate(row.observed_end_at) }}</td>
    <td>{{ row.tool }}</td>
    <td>{{ row.source_basename }}</td>
    <td>{{ row.repo_name }}</td>
    <td>{{ row.binding_status }}</td>
    <td>{{ shortSha(row.commit_sha) }}</td>
  </tr>
</table>
```

- [x] **Step 4: Run page tests and the focused frontend suite**

Run:

```bash
cd frontend && pnpm exec vitest run src/__tests__/events-view.test.ts src/__tests__/api-events.test.ts src/__tests__/router.test.ts
```

Expected:

```text
✓ src/__tests__/events-view.test.ts
✓ src/__tests__/api-events.test.ts
✓ src/__tests__/router.test.ts
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/events/EventsView.vue frontend/src/__tests__/events-view.test.ts
git commit -m "feat(frontend): add global events page"
```

---

### Task 5: Docs Alignment and Final Verification

**Files:**
- Modify: `docs/architecture.md`

- [x] **Step 1: Update architecture doc for the `/events` surface**

```md
- Add `/events` as a new protected global page for browsing backend-ingested `tool_usage_events`.
- Document that the page is event-level, not PR/commit aggregate-level.
- Clarify that raw event fields are admin-only while regular users are scoped to their own rows.
```

- [x] **Step 2: Run end-to-end targeted verification**

Run:

```bash
cd backend && go test ./internal/toolusage ./internal/handler -count=1
cd ../frontend && pnpm exec vitest run src/__tests__/api-events.test.ts src/__tests__/events-view.test.ts src/__tests__/router.test.ts
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/toolusage
ok      github.com/ai-efficiency/backend/internal/handler
✓ 3 frontend test files passed
```

- [x] **Step 3: Manual local UI verification**

Run:

```bash
docker compose -f deploy/docker-compose.dev.yml ps
open http://localhost:18081/events
```

Expected:

```text
backend/postgres/redis are healthy
/events loads with 24h summary cards and event rows
```

Replay evidence from 2026-06-06:

- Browser plugin was unavailable (`agent.browsers.list() == []`), so Playwright CLI was used as the browser-level fallback.
- Isolated local runtime used backend `http://127.0.0.1:8081`, frontend `http://127.0.0.1:5173`, temp DB `ae_contract_20260606220415`, and temp `ae-cli` HOME `/tmp/ae_contract_20260606220415/cli-home`.
- `/events` desktop verification showed summary cards `total=2`, `bound=1`, `unbound=1`, `tools=1`, two Codex rows, and a detail drawer with commit `1123456789abcdef0123456789abcdef01234567`.
- `/events` mobile verification at `390x844` showed the responsive card list, collapsed filters, the same two records, and no console errors.
- Homepage `/` verification showed the embedded "My usage" surface in its configured=false setup state, with `/api/v1/user/usage/dashboard` returning `HTTP 200`.
- Isolated `ae-cli` smoke used temp login state only: `ae-cli init --hooks repo --force`, a real git commit, and `ae-cli sync` created event `id=3` for `smoke-org/cli-smoke-20260606221301`, bound to checkpoint `commit_checkpoint_id=2` and commit `f25de454900a2a086bb1f484570ebd692a9d0e90`.
- Screenshots: `output/playwright/events-detail-20260606.png`, `output/playwright/events-mobile-20260606.png`, `output/playwright/home-usage-20260606.png`.

- [x] **Step 4: Commit docs and verification adjustments**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): add global events page runtime"
```

---

## Spec Coverage Check

- Global `/events` route: covered by Task 3 and Task 4
- Summary + event table layout: covered by Task 4
- Admin-all vs user-own scoping: covered by Task 1 and Task 2
- `tool + source basename` rows: covered by Task 1 and Task 4
- Right drawer detail: covered by Task 4
- Full raw fields for admin only: covered by Task 1 and Task 2
- `PR` best-effort reverse lookup in detail only: covered by Task 1
- Default `24h` window: covered by Task 4
- Architecture doc alignment: covered by Task 5

## Self-Review

- No placeholder sections remain.
- The plan stays within the first-version scope and does not pull in local queue/spool visibility.
- `PR` is consistently treated as a best-effort derived detail field, not a list column.
- Backend DTOs are intentionally split into summary/list/detail to avoid returning `raw_payload` in the first page load.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-21-global-tool-usage-events-page.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
