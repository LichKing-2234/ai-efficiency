# Events Page Filters and Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/events` usable for historical event browsing with explicit time filters, server-side pagination, and an admin-only searchable user selector.

**Architecture:** Reuse the existing `/events` list and summary query path, adding a small admin-only `/events/users` endpoint that searches local users who already have `tool_usage_events`. The frontend keeps `user_id` as an internal selected value while presenting email/username search to admins, and keeps regular users scoped by the existing backend authorization rules.

**Tech Stack:** Go backend with Gin + Ent, Vue 3 `<script setup lang="ts">`, Axios API wrappers, Pinia auth store, Vitest frontend tests, Go unit/handler tests.

**Status:** Implemented and verified on 2026-05-25. Backend focused tests, frontend tests, frontend build, and local Playwright smoke passed. The final backend user search implementation uses `tool_usage_events` aggregation instead of eager-loading all user events.

---

## File Map

- Modify: `backend/internal/toolusage/query.go`
  - Add `EventUserSearchRequest`, `EventUserOption`, and `SearchEventUsers`.
  - Query only users with `tool_usage_events`, aggregate `event_count` and `latest_event_at`, and apply email/username search.
- Modify: `backend/internal/handler/events.go`
  - Add `Users` handler for `GET /api/v1/events/users`.
  - Parse `q` and clamped `limit`.
  - Enforce admin-only access.
- Modify: `backend/internal/handler/router.go`
  - Register `GET /api/v1/events/users` before `GET /api/v1/events/:id`.
- Modify: `backend/internal/toolusage/query_test.go`
  - Add service tests for event-user search ordering, filtering, limit clamp, and ignoring users without events.
- Modify: `backend/internal/handler/events_test.go`
  - Add handler tests for admin success and regular-user `403` on `/events/users`.
- Modify: `frontend/src/types/index.ts`
  - Add `ToolUsageEventUserOption`.
- Modify: `frontend/src/api/events.ts`
  - Add `searchEventUsers`.
- Modify: `frontend/src/views/events/EventsView.vue`
  - Change default range to 7 days.
  - Add datetime inputs, clear range, admin-only user selector, page size, previous/next pagination.
  - Reset offset on filter changes.
- Modify: `frontend/src/__tests__/api-events.test.ts`
  - Cover `searchEventUsers`.
- Modify: `frontend/src/__tests__/events-view.test.ts`
  - Cover 7-day default range, admin user selector behavior, regular-user hidden selector, pagination query params, and filter reset.
- Reference only: `docs/superpowers/specs/2026-05-21-global-tool-usage-events-page-design.md`
  - Current design contract already updated.

---

### Task 1: Backend Event User Search Service

**Files:**
- Modify: `backend/internal/toolusage/query.go`
- Test: `backend/internal/toolusage/query_test.go`

- [x] **Step 1: Write failing service tests**

Append these tests to `backend/internal/toolusage/query_test.go`:

```go
func TestSearchEventUsersReturnsOnlyUsersWithEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	noEventScope := seedToolUsageScope(t, client)

	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetRole(user.RoleAdmin).
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetRole(user.RoleUser).
		ExecX(ctx)
	client.User.UpdateOneID(noEventScope.UserID).
		SetUsername("carol").
		SetEmail("carol@example.net").
		ExecX(ctx)

	aliceLatest := time.Now().Add(-2 * time.Minute).UTC()
	bobLatest := time.Now().Add(-10 * time.Minute).UTC()
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-1").
		SetDedupeKey("alice-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-1 * time.Second)).
		SetObservedEndAt(aliceLatest).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-2").
		SetDedupeKey("alice-2").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-2 * time.Second)).
		SetObservedEndAt(aliceLatest.Add(-1 * time.Second)).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID(bobScope.WorkspaceID).
		SetRepoConfigID(bobScope.RepoConfigID).
		SetUserID(bobScope.UserID).
		SetToolSessionID("bob-session-1").
		SetDedupeKey("bob-1").
		SetUsageUnit(toolusageevent.UsageUnitCredit).
		SetObservedStartAt(bobLatest.Add(-1 * time.Second)).
		SetObservedEndAt(bobLatest).
		SaveX(ctx)

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Limit: 20})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users=%d, want 2: %+v", len(users), users)
	}
	if users[0].Email != "alice@example.com" || users[0].EventCount != 2 {
		t.Fatalf("first user=%+v, want alice with 2 events", users[0])
	}
	if users[1].Email != "bob@example.org" || users[1].EventCount != 1 {
		t.Fatalf("second user=%+v, want bob with 1 event", users[1])
	}
}

func TestSearchEventUsersFiltersByEmailOrUsernameAndClampsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		ExecX(ctx)

	for i, scope := range []TestToolUsageScope{aliceScope, bobScope} {
		observedAt := time.Now().Add(time.Duration(-i) * time.Minute).UTC()
		client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID(scope.WorkspaceID + "-session").
			SetDedupeKey(scope.WorkspaceID + "-dedupe").
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetObservedStartAt(observedAt.Add(-1 * time.Second)).
			SetObservedEndAt(observedAt).
			SaveX(ctx)
	}

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "EXAMPLE.ORG", Limit: 100})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users=%d, want 1: %+v", len(users), users)
	}
	if users[0].Username != "bob" || users[0].Email != "bob@example.org" {
		t.Fatalf("user=%+v, want bob", users[0])
	}

	users, err = svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "ali", Limit: 0})
	if err != nil {
		t.Fatalf("SearchEventUsers username: %v", err)
	}
	if len(users) != 1 || users[0].Email != "alice@example.com" {
		t.Fatalf("users=%+v, want alice", users)
	}
}
```

- [x] **Step 2: Run service tests and verify RED**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestSearchEventUsers' -count=1
```

Expected: fail to compile because `SearchEventUsers`, `EventUserSearchRequest`, and `EventUserOption` do not exist.

- [x] **Step 3: Implement search DTOs and service**

In `backend/internal/toolusage/query.go`, add imports:

```go
	"entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent/user"
```

Add these types after `GetEventDetailRequest`:

```go
type EventUserSearchRequest struct {
	Q     string
	Limit int
}

type EventUserOption struct {
	ID            int       `json:"id"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	EventCount    int       `json:"event_count"`
	LatestEventAt time.Time `json:"latest_event_at"`
}
```

Add this method near the other query methods:

```go
func (s *QueryService) SearchEventUsers(ctx context.Context, req EventUserSearchRequest) ([]EventUserOption, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("search event users: ent client is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	q := strings.ToLower(strings.TrimSpace(req.Q))
	query := s.entClient.User.Query().
		Where(user.HasToolUsageEvents()).
		WithToolUsageEvents().
		Limit(limit).
		Order(func(selector *sql.Selector) {
			t := sql.Table(toolusageevent.Table)
			selector.LeftJoin(t).On(selector.C(user.FieldID), t.C(toolusageevent.FieldUserID))
			selector.GroupBy(selector.C(user.FieldID))
			selector.OrderBy(sql.Desc(sql.Max(t.C(toolusageevent.FieldObservedEndAt))))
		})
	if q != "" {
		like := "%" + q + "%"
		query.Where(user.Or(
			user.EmailContainsFold(q),
			user.UsernameContainsFold(q),
			func(selector *sql.Selector) {
				selector.Where(sql.Like(sql.Lower(selector.C(user.FieldEmail)), like))
			},
			func(selector *sql.Selector) {
				selector.Where(sql.Like(sql.Lower(selector.C(user.FieldUsername)), like))
			},
		))
	}

	users, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("search event users: %w", err)
	}

	out := make([]EventUserOption, 0, len(users))
	for _, u := range users {
		eventCount := len(u.Edges.ToolUsageEvents)
		var latest time.Time
		for _, ev := range u.Edges.ToolUsageEvents {
			if ev.ObservedEndAt.After(latest) {
				latest = ev.ObservedEndAt
			}
		}
		out = append(out, EventUserOption{
			ID:            u.ID,
			Username:      u.Username,
			Email:         u.Email,
			Role:          string(u.Role),
			EventCount:    eventCount,
			LatestEventAt: latest,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LatestEventAt.After(out[j].LatestEventAt)
	})
	return out, nil
}
```

If Ent generates a duplicate `sql` import conflict because `backend/internal/handler/events.go` already imports `sql`, keep this import only in `query.go`; it is a separate package.

- [x] **Step 4: Run service tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestSearchEventUsers' -count=1
```

Expected: PASS.

- [x] **Step 5: Run full toolusage package tests**

Run:

```bash
cd backend && go test ./internal/toolusage -count=1
```

Expected: PASS.

- [x] **Step 6: Update this plan checkbox**

Mark Task 1 steps complete in `docs/superpowers/plans/2026-05-25-events-page-filters-pagination.md`.

---

### Task 2: Backend Event User Search HTTP Endpoint

**Files:**
- Modify: `backend/internal/handler/events.go`
- Modify: `backend/internal/handler/router.go`
- Test: `backend/internal/handler/events_test.go`

- [x] **Step 1: Write failing handler tests**

Append these tests to `backend/internal/handler/events_test.go`:

```go
func TestEventsUsersSearchAdminOnly(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	userResp := doFullRequestWithToken(env, http.MethodGet, "/api/v1/events/users?q=cov&limit=20", nil, nonAdminToken)
	if userResp.Code != http.StatusForbidden {
		t.Fatalf("regular user status = %d, want 403, body=%s", userResp.Code, userResp.Body.String())
	}
}

func TestEventsUsersSearchReturnsUsersWithEventsForAdmin(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	seedEventsFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/events/users?q=cov&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	data := parseFullResponse(t, w)["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("users=%d, want 1: %+v", len(data), data)
	}
	row := data[0].(map[string]interface{})
	if row["username"] != "covuser" {
		t.Fatalf("username=%v, want covuser", row["username"])
	}
	if int(row["event_count"].(float64)) != 2 {
		t.Fatalf("event_count=%v, want 2", row["event_count"])
	}
	if row["latest_event_at"] == "" {
		t.Fatalf("latest_event_at is empty: %+v", row)
	}
}
```

- [x] **Step 2: Run handler tests and verify RED**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestEventsUsersSearch' -count=1
```

Expected: fail with 404 for `/api/v1/events/users` or missing handler method.

- [x] **Step 3: Implement `EventsHandler.Users`**

In `backend/internal/handler/events.go`, add this method after `Get`:

```go
func (h *EventsHandler) Users(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isAdminRole(uc.Role) {
		pkg.Error(c, http.StatusForbidden, "admin access required")
		return
	}
	limit := parseOptionalInt(c.DefaultQuery("limit", "20"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	users, err := h.service.SearchEventUsers(c.Request.Context(), toolusage.EventUserSearchRequest{
		Q:     strings.TrimSpace(c.Query("q")),
		Limit: limit,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, users)
}
```

- [x] **Step 4: Register route before `/:id`**

In `backend/internal/handler/router.go`, change the events group to:

```go
	eventsGroup := protected.Group("/events")
	{
		eventsGroup.GET("/summary", eventsHandler.Summary)
		eventsGroup.GET("/users", eventsHandler.Users)
		eventsGroup.GET("", eventsHandler.List)
		eventsGroup.GET("/:id", eventsHandler.Get)
	}
```

- [x] **Step 5: Run handler tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestEventsUsersSearch' -count=1
```

Expected: PASS.

- [x] **Step 6: Run focused event handler tests**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestEvents' -count=1
```

Expected: PASS.

- [x] **Step 7: Update this plan checkbox**

Mark Task 2 steps complete in this plan.

---

### Task 3: Frontend API Types for Event User Search

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/events.ts`
- Test: `frontend/src/__tests__/api-events.test.ts`

- [x] **Step 1: Write failing API test**

In `frontend/src/__tests__/api-events.test.ts`, update the import:

```ts
import { getEventSummary, getEventDetail, listEvents, searchEventUsers } from '@/api/events'
```

Append this test inside `describe('events API', () => { ... })`:

```ts
  it('searchEventUsers calls GET /events/users with query params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await searchEventUsers({ q: 'alice@example.com', limit: 20 })
    expect(mockClient.get).toHaveBeenCalledWith('/events/users', {
      params: { q: 'alice@example.com', limit: 20 },
    })
  })
```

- [x] **Step 2: Run API test and verify RED**

Run:

```bash
cd frontend && pnpm vitest --run src/__tests__/api-events.test.ts
```

Expected: fail because `searchEventUsers` is not exported.

- [x] **Step 3: Add frontend type**

In `frontend/src/types/index.ts`, add this after `ToolUsageEventSummary`:

```ts
export interface ToolUsageEventUserOption {
  id: number
  username: string
  email: string
  role: string
  event_count: number
  latest_event_at: string
}
```

- [x] **Step 4: Add API wrapper**

In `frontend/src/api/events.ts`, update imports:

```ts
  ToolUsageEventUserOption,
```

Add this function:

```ts
export function searchEventUsers(params?: Record<string, unknown>) {
  return client.get<ApiResponse<ToolUsageEventUserOption[]>>('/events/users', { params })
}
```

- [x] **Step 5: Run API test and verify GREEN**

Run:

```bash
cd frontend && pnpm vitest --run src/__tests__/api-events.test.ts
```

Expected: PASS.

- [x] **Step 6: Update this plan checkbox**

Mark Task 3 steps complete in this plan.

---

### Task 4: Frontend Events Page Filters, User Selector, and Pagination

**Files:**
- Modify: `frontend/src/views/events/EventsView.vue`
- Test: `frontend/src/__tests__/events-view.test.ts`

- [x] **Step 1: Write failing frontend tests**

In `frontend/src/__tests__/events-view.test.ts`, update the API mock:

```ts
vi.mock('@/api/events', () => ({
  getEventSummary: vi.fn(),
  listEvents: vi.fn(),
  getEventDetail: vi.fn(),
  searchEventUsers: vi.fn(),
}))
```

In `mountEvents`, update the import and default mock:

```ts
  const { getEventSummary, listEvents, getEventDetail, searchEventUsers } = await import('@/api/events')
  ;(searchEventUsers as any).mockResolvedValue({
    data: {
      data: [
        {
          id: 2,
          username: 'alice',
          email: 'alice@example.com',
          role: 'admin',
          event_count: 3,
          latest_event_at: '2026-05-22T03:29:57Z',
        },
      ],
    },
  })
```

Update the return statement:

```ts
  return { wrapper, getEventSummary, listEvents, getEventDetail, searchEventUsers }
```

Replace the first test with this 7-day assertion:

```ts
  it('loads summary and event rows on mount with a 7-day default range', async () => {
    const { wrapper, getEventSummary, listEvents } = await mountEvents()

    expect(getEventSummary).toHaveBeenCalled()
    expect(listEvents).toHaveBeenCalled()
    const listParams = (listEvents as any).mock.calls[0][0]
    const from = new Date(listParams.from).getTime()
    const to = new Date(listParams.to).getTime()
    const days = Math.round((to - from) / (24 * 60 * 60 * 1000))
    expect(days).toBe(7)
    expect(listParams.limit).toBe(20)
    expect(listParams.offset).toBe(0)
    expect(wrapper.text()).toContain('Total Events')
    expect(wrapper.text()).toContain('detail.jsonl')
  })
```

Append these tests:

```ts
  it('shows admin-only searchable user selector and applies selected user id', async () => {
    const { wrapper, listEvents, searchEventUsers } = await mountEvents(true)

    const input = wrapper.get('[data-testid="event-user-search"]')
    await input.setValue('alice@example.com')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    expect(searchEventUsers).toHaveBeenCalledWith({ q: 'alice@example.com', limit: 20 })
    expect(wrapper.text()).toContain('alice@example.com')

    await wrapper.get('[data-testid="event-user-option-2"]').trigger('click')
    await flushPromises()

    const latestParams = (listEvents as any).mock.calls.at(-1)[0]
    expect(latestParams.user_id).toBe(2)
    expect(latestParams.offset).toBe(0)
  })

  it('hides user selector from regular users', async () => {
    const { wrapper } = await mountEvents(false)

    expect(wrapper.find('[data-testid="event-user-search"]').exists()).toBe(false)
  })

  it('updates pagination params for next page and page size changes', async () => {
    ;(await import('@/api/events')).listEvents.mockResolvedValue({
      data: { data: { items: [sampleRow], total: 45, page: 0, page_size: 20 } },
    })
    const { wrapper, listEvents } = await mountEvents()

    await wrapper.get('[data-testid="events-next-page"]').trigger('click')
    await flushPromises()
    expect((listEvents as any).mock.calls.at(-1)[0].offset).toBe(20)

    await wrapper.get('[data-testid="events-page-size"]').setValue('50')
    await flushPromises()
    const latestParams = (listEvents as any).mock.calls.at(-1)[0]
    expect(latestParams.limit).toBe(50)
    expect(latestParams.offset).toBe(0)
  })
```

- [x] **Step 2: Run EventsView tests and verify RED**

Run:

```bash
cd frontend && pnpm vitest --run src/__tests__/events-view.test.ts
```

Expected: fail because controls and 7-day default do not exist.

- [x] **Step 3: Implement script state and helpers**

In `frontend/src/views/events/EventsView.vue`, update imports:

```ts
import { getEventDetail, getEventSummary, listEvents, searchEventUsers } from '@/api/events'
import type { ToolUsageEventDetail, ToolUsageEventRow, ToolUsageEventSummary, ToolUsageEventUserOption } from '@/types'
```

Replace filter state and default helpers with:

```ts
const userSearch = ref('')
const userOptions = ref<ToolUsageEventUserOption[]>([])
const selectedUser = ref<ToolUsageEventUserOption | null>(null)
const userSearchLoading = ref(false)

const filters = reactive({
  from: toDateTimeLocal(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000)),
  to: toDateTimeLocal(new Date()),
  tool: '',
  binding_status: '',
  q: '',
  limit: 20,
  offset: 0,
})

const currentPage = computed(() => Math.floor(filters.offset / filters.limit) + 1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.limit)))
const canGoPrev = computed(() => filters.offset > 0)
const canGoNext = computed(() => filters.offset + filters.limit < total.value)

function toDateTimeLocal(date: Date) {
  const pad = (value: number) => String(value).padStart(2, '0')
  const yyyy = date.getFullYear()
  const mm = pad(date.getMonth() + 1)
  const dd = pad(date.getDate())
  const hh = pad(date.getHours())
  const min = pad(date.getMinutes())
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`
}

function fromDateTimeLocal(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}
```

Update `buildQuery`:

```ts
function buildQuery(includePagination = true) {
  const params: Record<string, unknown> = {}
  const from = fromDateTimeLocal(filters.from)
  const to = fromDateTimeLocal(filters.to)
  if (from) params.from = from
  if (to) params.to = to
  if (includePagination) {
    params.limit = filters.limit
    params.offset = filters.offset
  }
  if (filters.tool) params.tool = filters.tool
  if (filters.binding_status) params.binding_status = filters.binding_status
  if (filters.q) params.q = filters.q
  if (isAdmin.value && selectedUser.value) params.user_id = selectedUser.value.id
  return params
}
```

Update `loadPage` to use list pagination only:

```ts
const summaryParams = buildQuery(false)
const listParams = buildQuery(true)
const [summaryRes, listRes] = await Promise.all([
  getEventSummary(summaryParams),
  listEvents(listParams),
])
```

Add actions:

```ts
async function searchUsers() {
  if (!isAdmin.value) return
  userSearchLoading.value = true
  try {
    const res = await searchEventUsers({ q: userSearch.value, limit: 20 })
    userOptions.value = res.data.data ?? []
  } finally {
    userSearchLoading.value = false
  }
}

async function selectUser(user: ToolUsageEventUserOption) {
  selectedUser.value = user
  userOptions.value = []
  filters.offset = 0
  await loadPage()
}

async function clearSelectedUser() {
  selectedUser.value = null
  userSearch.value = ''
  userOptions.value = []
  filters.offset = 0
  await loadPage()
}

async function applyFilters() {
  filters.offset = 0
  await loadPage()
}

async function clearTimeRange() {
  filters.from = ''
  filters.to = ''
  filters.offset = 0
  await loadPage()
}

async function nextPage() {
  if (!canGoNext.value) return
  filters.offset += filters.limit
  await loadPage()
}

async function previousPage() {
  if (!canGoPrev.value) return
  filters.offset = Math.max(0, filters.offset - filters.limit)
  await loadPage()
}

async function changePageSize() {
  filters.offset = 0
  await loadPage()
}
```

- [x] **Step 4: Implement template controls**

In `frontend/src/views/events/EventsView.vue`, replace the filter card with a grid that includes:

```vue
<label class="text-xs font-medium uppercase tracking-wide text-gray-500">
  From
  <input
    v-model="filters.from"
    type="datetime-local"
    class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
  />
</label>
<label class="text-xs font-medium uppercase tracking-wide text-gray-500">
  To
  <input
    v-model="filters.to"
    type="datetime-local"
    class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
  />
</label>
```

Keep the existing `Tool`, `Binding`, and `Search` controls.

Add this admin-only user selector below the filter grid:

```vue
<div v-if="isAdmin" class="mt-3 border-t border-gray-100 pt-3">
  <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
    User
    <div class="mt-1 flex gap-2">
      <input
        v-model="userSearch"
        data-testid="event-user-search"
        class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
        placeholder="Search email or username"
      />
      <button
        data-testid="event-user-search-button"
        class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
        :disabled="userSearchLoading"
        @click="searchUsers"
      >
        {{ userSearchLoading ? 'Searching...' : 'Search' }}
      </button>
    </div>
  </label>
  <div v-if="selectedUser" class="mt-2 flex items-center justify-between rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-700">
    <span>{{ selectedUser.email }} · {{ selectedUser.username }} · {{ selectedUser.event_count }} events</span>
    <button class="text-xs font-medium text-gray-500 hover:text-gray-900" @click="clearSelectedUser">Clear</button>
  </div>
  <div v-if="userOptions.length > 0" class="mt-2 divide-y divide-gray-100 rounded-md border border-gray-200 bg-white">
    <button
      v-for="option in userOptions"
      :key="option.id"
      :data-testid="`event-user-option-${option.id}`"
      class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-50"
      @click="selectUser(option)"
    >
      <span class="font-medium text-gray-900">{{ option.email }}</span>
      <span class="ml-2 text-xs text-gray-500">{{ option.username }} · {{ option.role }} · {{ option.event_count }} events</span>
    </button>
  </div>
</div>
```

Add buttons:

```vue
<button class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50" @click="clearTimeRange">Clear Time</button>
<button class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white" @click="applyFilters">Apply Filters</button>
```

Add pagination controls near the table header:

```vue
<div class="flex items-center gap-2 text-xs text-gray-500">
  <select
    v-model.number="filters.limit"
    data-testid="events-page-size"
    class="rounded-md border border-gray-300 px-2 py-1 text-xs"
    @change="changePageSize"
  >
    <option :value="20">20</option>
    <option :value="50">50</option>
    <option :value="100">100</option>
  </select>
  <button
    data-testid="events-prev-page"
    class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
    :disabled="!canGoPrev || loading"
    @click="previousPage"
  >
    Prev
  </button>
  <span>Page {{ currentPage }} / {{ totalPages }}</span>
  <button
    data-testid="events-next-page"
    class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
    :disabled="!canGoNext || loading"
    @click="nextPage"
  >
    Next
  </button>
</div>
```

- [x] **Step 5: Run EventsView tests and verify GREEN**

Run:

```bash
cd frontend && pnpm vitest --run src/__tests__/events-view.test.ts
```

Expected: PASS.

- [x] **Step 6: Run frontend event API tests**

Run:

```bash
cd frontend && pnpm vitest --run src/__tests__/api-events.test.ts src/__tests__/events-view.test.ts
```

Expected: PASS.

- [x] **Step 7: Update this plan checkbox**

Mark Task 4 steps complete in this plan.

---

### Task 5: Full Verification

**Files:**
- Verify only.
- Update this plan checkboxes as commands pass.

- [x] **Step 1: Run backend focused tests**

Run:

```bash
cd backend && go test ./internal/toolusage ./internal/handler -count=1
```

Expected: PASS.

- [x] **Step 2: Run frontend tests**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [x] **Step 3: Run frontend build**

Run:

```bash
cd frontend && pnpm build
```

Expected: PASS.

- [x] **Step 4: Optional browser smoke if dev server is running**

The smoke was performed against a local dev backend on `127.0.0.1:18082` and Vite frontend on `127.0.0.1:5173`. Verified:

- Events appear by default when local DB has events in the last 7 days.
- Admin sees a user search box.
- Regular-user visibility is covered by automated frontend and handler tests.
- Next page and page size controls update the table.

Do not check this box unless the browser smoke was actually performed.

---

## Self-Review

- Spec coverage: time range, pagination, admin searchable user selector, ordinary-user scoping, backend user search endpoint, and tests are each covered by a task.
- Placeholder scan: no placeholder implementation steps remain.
- Type consistency: backend uses `EventUserSearchRequest` / `EventUserOption`; frontend uses `ToolUsageEventUserOption`; API path is consistently `/events/users`.
