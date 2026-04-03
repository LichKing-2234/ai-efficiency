# Session Visibility And Filters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix dashboard `active_sessions` so it counts only active sessions, and make the Sessions page role-aware: admins can see all sessions while non-admin users only see their own sessions, with practical session filters.

**Architecture:** Keep the API shape stable where possible. The backend will tighten the dashboard aggregation and centralize session visibility rules in the existing session handler so both list and detail endpoints apply the same admin/non-admin policy. The frontend will extend the current Sessions page controls to send explicit filters without changing the underlying page structure.

**Tech Stack:** Go (`gin`, `ent`, existing handler tests), Vue 3 (`<script setup>`, Pinia, Vitest), existing `frontend/src/api/session.ts` client.

---

## File Structure

### Modified files

- `backend/internal/handler/efficiency.go`
  Fix `active_sessions` to count only `status = active`.
- `backend/internal/handler/session.go`
  Add role-aware session visibility and new list filters; make `GET /sessions/:id` respect the same visibility policy so admin can open any listed session.
- `backend/internal/handler/handler_test.go`
  Add small test helpers for issuing tokens / request contexts for both admin and non-admin users.
- `backend/internal/handler/handler_extended_test.go`
  Add dashboard aggregation tests and session list visibility/filter tests.
- `backend/internal/handler/session_detail_http_test.go`
  Add detail access tests proving admin can read any session and non-admin cannot read others' sessions.
- `frontend/src/api/session.ts`
  Extend `listSessions()` param type with new filters.
- `frontend/src/types/index.ts`
  Add a typed filter payload for session list requests.
- `frontend/src/views/sessions/SessionListView.vue`
  Add branch / repo / ownership filters and admin-only visibility controls.
- `frontend/src/__tests__/api-modules.test.ts`
  Add assertions for the new session list query params.
- `frontend/src/__tests__/session-list-view.test.ts`
  Add UI tests for admin-only filters and request param wiring.

### Existing files to read before implementation

- `backend/internal/handler/efficiency.go`
- `backend/internal/handler/session.go`
- `backend/internal/handler/handler_test.go`
- `backend/internal/handler/handler_extended_test.go`
- `backend/internal/handler/session_detail_http_test.go`
- `frontend/src/views/sessions/SessionListView.vue`
- `frontend/src/api/session.ts`
- `frontend/src/types/index.ts`
- `frontend/src/stores/auth.ts`

### Filter contract for this plan

To stay YAGNI and still solve the current usability gap, implement only these new filters:

- `repo_query`
  Case-insensitive substring match against `repo_configs.full_name` and `repo_configs.clone_url`
- `branch`
  Case-insensitive substring match against `sessions.branch`
- `owner_scope`
  Values:
  - `all` — admin only, includes owned and unowned sessions
  - `mine` — current user’s sessions
  - `unowned` — admin only, `user_sessions IS NULL`

Keep existing filters working:

- `status`
- `repo_id`
- `page`
- `page_size`

Default behavior:

- admin: `owner_scope=all`
- non-admin: forced to `mine` regardless of query string

---

### Task 1: Fix Dashboard Active Session Counting

**Files:**
- Modify: `backend/internal/handler/efficiency.go`
- Modify: `backend/internal/handler/handler_extended_test.go`
- Test: `backend/internal/handler/handler_extended_test.go`

- [ ] **Step 1: Write the failing dashboard aggregation test**

Add this test near the existing dashboard tests in `backend/internal/handler/handler_extended_test.go`:

```go
func TestDashboardCountsOnlyActiveSessions(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	_, err := env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440100")).
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetBranch("main").
		SetStartedAt(time.Now()).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create active session: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440101")).
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetBranch("release").
		SetStartedAt(time.Now()).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		t.Fatalf("create completed session: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/efficiency/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	activeSessions := int(data["active_sessions"].(float64))
	if activeSessions != 1 {
		t.Fatalf("active_sessions = %d, want %d", activeSessions, 1)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestDashboardCountsOnlyActiveSessions$' -count=1
```

Expected:

```text
FAIL ... active_sessions = 2, want 1
```

- [ ] **Step 3: Implement the minimal dashboard fix**

Change `backend/internal/handler/efficiency.go`:

```go
activeSessions, _ := h.entClient.Session.Query().
	Where(session.StatusEQ(session.StatusActive)).
	Count(ctx)
```

Also add the missing import:

```go
"github.com/ai-efficiency/backend/ent/session"
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestDashboardCountsOnlyActiveSessions$' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/handler	...
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/efficiency.go \
        backend/internal/handler/handler_extended_test.go
git commit -m "fix(backend): count only active sessions on dashboard"
```

### Task 2: Add Role-Aware Session Visibility And Backend Filters

**Files:**
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/handler_test.go`
- Modify: `backend/internal/handler/handler_extended_test.go`
- Modify: `backend/internal/handler/session_detail_http_test.go`
- Test: `backend/internal/handler/handler_extended_test.go`
- Test: `backend/internal/handler/session_detail_http_test.go`

- [ ] **Step 1: Write failing backend tests for admin visibility and filters**

In `backend/internal/handler/handler_test.go`, add a helper that can mint a token for any user:

```go
func issueTokenForUser(t *testing.T, env *testEnv, id int, username, role string) string {
	t.Helper()
	pair, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       id,
		Username: username,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return pair.AccessToken
}

func doRequestWithToken(env *testEnv, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	env.router.ServeHTTP(w, req)
	return w
}
```

Then add these tests in `backend/internal/handler/handler_extended_test.go`:

```go
func TestSessionListAdminCanSeeOwnedAndUnownedSessions(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	member, err := env.client.User.Create().
		SetUsername("member").
		SetEmail("member@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440110")).
		SetRepoConfigID(repoID).
		SetUserID(member.ID).
		SetBranch("feat/member").
		SetStartedAt(time.Now()).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440111")).
		SetRepoConfigID(repoID).
		SetBranch("feat/unowned").
		SetStartedAt(time.Now()).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		t.Fatalf("create unowned session: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 2 {
		t.Fatalf("total = %d, want %d", total, 2)
	}
}

func TestSessionListNonAdminOnlySeesOwnSessionsEvenIfOwnerScopeAll(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	member, err := env.client.User.Create().
		SetUsername("member").
		SetEmail("member@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440120")).
		SetRepoConfigID(repoID).
		SetUserID(member.ID).
		SetBranch("feat/member").
		SetStartedAt(time.Now()).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440121")).
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetBranch("feat/admin").
		SetStartedAt(time.Now()).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	memberToken := issueTokenForUser(t, env, member.ID, member.Username, "user")
	w := doRequestWithToken(env, "GET", "/api/v1/sessions?owner_scope=all", nil, memberToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 1 {
		t.Fatalf("total = %d, want %d", total, 1)
	}
}

func TestSessionListFiltersByOwnerScopeRepoQueryAndBranch(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	alphaRepo := createTestRepo(t, env.client)
	betaRepo, err := env.client.RepoConfig.Create().
		SetScmProviderID(1).
		SetName("beta").
		SetFullName("org/beta").
		SetCloneURL("https://github.com/org/beta.git").
		SetDefaultBranch("main").
		Save(ctx)
	if err != nil {
		t.Fatalf("create beta repo: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440130")).
		SetRepoConfigID(alphaRepo).
		SetBranch("feat/search-me").
		SetStartedAt(time.Now()).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		t.Fatalf("create alpha unowned session: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440131")).
		SetRepoConfigID(betaRepo.ID).
		SetUserID(env.userID).
		SetBranch("fix/other").
		SetStartedAt(time.Now()).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create beta owned session: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/sessions?owner_scope=unowned&repo_query=alpha&branch=search", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 1 {
		t.Fatalf("total = %d, want %d", total, 1)
	}
}
```

Add this detail access test in `backend/internal/handler/session_detail_http_test.go`:

```go
func TestSessionDetailAdminCanReadOtherUsersSessionButUserCannot(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token := strings.TrimPrefix(header, "Bearer ")
			if claims, err := authSvc.ParseToken(token); err == nil {
				c.Set("user_id", claims.UserID)
				c.Set("user", claims)
			}
		}
	})
	r.GET("/sessions/:id", h.Get)

	ctx := t.Context()
	adminUser := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		SaveX(ctx)
	memberUser := client.User.Create().
		SetUsername("member").
		SetEmail("member@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SaveX(ctx)
	otherUser := client.User.Create().
		SetUsername("other").
		SetEmail("other@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SaveX(ctx)
	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sessionID := uuid.New()
	client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repo.ID).
		SetUserID(memberUser.ID).
		SetBranch("feat/member").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)

	adminPair, _ := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: adminUser.ID, Username: adminUser.Username, Role: "admin"})
	memberPair, _ := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: memberUser.ID, Username: memberUser.Username, Role: "user"})
	otherPair, _ := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: otherUser.ID, Username: otherUser.Username, Role: "user"})

	adminReq := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID.String(), nil)
	adminReq.Header.Set("Authorization", "Bearer "+adminPair.AccessToken)
	adminW := httptest.NewRecorder()
	r.ServeHTTP(adminW, adminReq)
	if adminW.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200, body=%s", adminW.Code, adminW.Body.String())
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID.String(), nil)
	otherReq.Header.Set("Authorization", "Bearer "+memberPair.AccessToken)
	otherW := httptest.NewRecorder()
	r.ServeHTTP(otherW, otherReq)
	if otherW.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200, body=%s", otherW.Code, otherW.Body.String())
	}

	deniedReq := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID.String(), nil)
	deniedReq.Header.Set("Authorization", "Bearer "+otherPair.AccessToken)
	deniedW := httptest.NewRecorder()
	r.ServeHTTP(deniedW, deniedReq)
	if deniedW.Code != http.StatusNotFound {
		t.Fatalf("other user status = %d, want 404, body=%s", deniedW.Code, deniedW.Body.String())
	}
}
```

- [ ] **Step 2: Run the backend tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'Test(SessionListAdminCanSeeOwnedAndUnownedSessions|SessionListNonAdminOnlySeesOwnSessionsEvenIfOwnerScopeAll|SessionListFiltersByOwnerScopeRepoQueryAndBranch|SessionDetailAdminCanReadOtherUsersSessionButUserCannot)$' -count=1
```

Expected:

```text
FAIL ... admin list only returns owned sessions
FAIL ... owner_scope / repo_query / branch ignored
FAIL ... admin detail path returns 404 for another user's session
```

- [ ] **Step 3: Implement backend visibility helpers and filters**

In `backend/internal/handler/session.go`, add small helpers near the top of the file:

```go
func isAdminUser(c *gin.Context) bool {
	uc := auth.GetUserContext(c)
	return uc != nil && uc.Role == "admin"
}

func requestedOwnerScope(c *gin.Context) string {
	scope := c.DefaultQuery("owner_scope", "")
	if scope == "" {
		if isAdminUser(c) {
			return "all"
		}
		return "mine"
	}
	if !isAdminUser(c) {
		return "mine"
	}
	switch scope {
	case "all", "mine", "unowned":
		return scope
	default:
		return "all"
	}
}
```

Update the list query construction:

```go
if uc := auth.GetUserContext(c); uc != nil {
	switch requestedOwnerScope(c) {
	case "mine":
		query = query.Where(session.HasUserWith(user.IDEQ(uc.UserID)))
	case "unowned":
		query = query.Where(session.Not(session.HasUser()))
	case "all":
		// admin sees all
	}
}

if branch := strings.TrimSpace(c.Query("branch")); branch != "" {
	query = query.Where(session.BranchContainsFold(branch))
}

if repoQuery := strings.TrimSpace(c.Query("repo_query")); repoQuery != "" {
	query = query.Where(session.HasRepoConfigWith(
		repoconfig.Or(
			repoconfig.FullNameContainsFold(repoQuery),
			repoconfig.CloneURLContainsFold(repoQuery),
		),
	))
}
```

Update the detail query policy so admin can open any session:

```go
query := h.entClient.Session.Query().
	Where(session.IDEQ(id))

if uc := auth.GetUserContext(c); uc != nil && uc.Role != "admin" {
	query = query.Where(session.HasUserWith(user.IDEQ(uc.UserID)))
}
```

Add missing imports:

```go
"strings"
```

- [ ] **Step 4: Run the backend tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'Test(SessionListAdminCanSeeOwnedAndUnownedSessions|SessionListNonAdminOnlySeesOwnSessionsEvenIfOwnerScopeAll|SessionListFiltersByOwnerScopeRepoQueryAndBranch|SessionDetailAdminCanReadOtherUsersSessionButUserCannot)$' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/handler	...
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/session.go \
        backend/internal/handler/handler_test.go \
        backend/internal/handler/handler_extended_test.go \
        backend/internal/handler/session_detail_http_test.go
git commit -m "feat(backend): add role-aware session visibility filters"
```

### Task 3: Expose The New Session Filters In The Frontend

**Files:**
- Modify: `frontend/src/api/session.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/sessions/SessionListView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/session-list-view.test.ts`
- Test: `frontend/src/__tests__/api-modules.test.ts`
- Test: `frontend/src/__tests__/session-list-view.test.ts`

- [ ] **Step 1: Write the failing frontend tests**

In `frontend/src/__tests__/api-modules.test.ts`, extend the session API section with:

```ts
it('listSessions passes branch repo_query and owner_scope params', async () => {
  mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
  const { listSessions } = await import('@/api/session')

  await listSessions({
    page: 2,
    page_size: 50,
    status: 'completed',
    repo_query: 'alpha',
    branch: 'feat/search',
    owner_scope: 'unowned',
  })

  expect(mockClient.get).toHaveBeenCalledWith('/sessions', {
    params: {
      page: 2,
      page_size: 50,
      status: 'completed',
      repo_query: 'alpha',
      branch: 'feat/search',
      owner_scope: 'unowned',
    },
  })
})
```

In `frontend/src/__tests__/session-list-view.test.ts`, add:

```ts
it('admin can apply branch repo and ownership filters', async () => {
  const { listSessions } = await import('@/api/session')
  ;(listSessions as any).mockResolvedValue({
    data: { data: { items: [], total: 0, page: 1, page_size: 20 } },
  })

  const { useAuthStore } = await import('@/stores/auth')
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/sessions', name: 'SessionList', component: SessionListView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })

  await router.push('/sessions')
  await router.isReady()

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = {
    id: 1,
    username: 'admin',
    email: 'admin@test.com',
    role: 'admin',
    auth_source: 'sub2api_sso',
  }

  const wrapper = mount(SessionListView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()

  await wrapper.find('[data-test=\"repo-query\"]').setValue('alpha')
  await wrapper.find('[data-test=\"branch-filter\"]').setValue('feat/search')
  await wrapper.find('[data-test=\"owner-scope\"]').setValue('unowned')
  await wrapper.find('[data-test=\"apply-filters\"]').trigger('click')
  await flushPromises()

  expect(listSessions).toHaveBeenLastCalledWith(expect.objectContaining({
    repo_query: 'alpha',
    branch: 'feat/search',
    owner_scope: 'unowned',
  }))
})

it('non-admin does not render owner scope filter', async () => {
  const { listSessions } = await import('@/api/session')
  ;(listSessions as any).mockResolvedValue({
    data: { data: { items: [], total: 0, page: 1, page_size: 20 } },
  })

  const { useAuthStore } = await import('@/stores/auth')
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/sessions', name: 'SessionList', component: SessionListView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })

  await router.push('/sessions')
  await router.isReady()

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = {
    id: 2,
    username: 'member',
    email: 'member@test.com',
    role: 'user',
    auth_source: 'sub2api_sso',
  }

  const wrapper = mount(SessionListView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()

  expect(wrapper.find('[data-test=\"owner-scope\"]').exists()).toBe(false)
})
```

- [ ] **Step 2: Run the frontend tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test -- api-modules session-list-view
```

Expected:

```text
FAIL because listSessions type/signature does not accept the new params
FAIL because SessionListView has no repo/branch/admin ownership filter controls
```

- [ ] **Step 3: Add typed filter params and wire the UI**

In `frontend/src/types/index.ts`, add:

```ts
export interface SessionListParams {
  page?: number
  page_size?: number
  status?: string
  repo_id?: number
  repo_query?: string
  branch?: string
  owner_scope?: 'all' | 'mine' | 'unowned'
}
```

Update `frontend/src/api/session.ts`:

```ts
import type { ApiResponse, PagedResponse, Session, SessionListParams } from '@/types'

export function listSessions(params?: SessionListParams) {
  return client.get<ApiResponse<PagedResponse<Session>>>('/sessions', { params })
}
```

Update `frontend/src/views/sessions/SessionListView.vue` with explicit filter refs:

```ts
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import type { Session, SessionListParams } from '@/types'

const auth = useAuthStore()
const repoQuery = ref('')
const branchFilter = ref('')
const ownerScope = ref<'all' | 'mine' | 'unowned'>('all')

const isAdmin = computed(() => auth.user?.role === 'admin')

async function fetchSessions() {
  loading.value = true
  try {
    const params: SessionListParams = {
      page: page.value,
      page_size: pageSize,
    }
    if (statusFilter.value) params.status = statusFilter.value
    if (repoQuery.value.trim()) params.repo_query = repoQuery.value.trim()
    if (branchFilter.value.trim()) params.branch = branchFilter.value.trim()
    if (isAdmin.value) params.owner_scope = ownerScope.value
    const res = await listSessions(params)
    sessions.value = res.data.data?.items ?? []
    total.value = res.data.data?.total ?? 0
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 1
  fetchSessions()
}

function resetFilters() {
  statusFilter.value = ''
  repoQuery.value = ''
  branchFilter.value = ''
  ownerScope.value = 'all'
  page.value = 1
  fetchSessions()
}
```

Add filter controls in the template:

```vue
<input
  v-model="repoQuery"
  data-test="repo-query"
  type="text"
  class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
  placeholder="Filter by repo"
/>

<input
  v-model="branchFilter"
  data-test="branch-filter"
  type="text"
  class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
  placeholder="Filter by branch"
/>

<select
  v-if="isAdmin"
  v-model="ownerScope"
  data-test="owner-scope"
  class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
>
  <option value="all">All Owners</option>
  <option value="mine">My Sessions</option>
  <option value="unowned">Unowned</option>
</select>

<button
  data-test="apply-filters"
  class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
  @click="applyFilters"
>
  Apply
</button>

<button
  data-test="reset-filters"
  class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
  @click="resetFilters"
>
  Reset
</button>
```

- [ ] **Step 4: Run the frontend tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test -- api-modules session-list-view
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/session.ts \
        frontend/src/types/index.ts \
        frontend/src/views/sessions/SessionListView.vue \
        frontend/src/__tests__/api-modules.test.ts \
        frontend/src/__tests__/session-list-view.test.ts
git commit -m "feat(frontend): add session visibility filters"
```

### Task 4: Final Verification

**Files:**
- Test only: no new files

- [ ] **Step 1: Run focused backend handler tests**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'Test(DashboardCountsOnlyActiveSessions|SessionList|SessionDetailAdminCanReadOtherUsersSessionButUserCannot)' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/handler	...
```

- [ ] **Step 2: Run full backend test suite**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./...
```

Expected:

```text
ok  	.../backend/internal/handler
ok  	.../backend/internal/...
```

- [ ] **Step 3: Run focused frontend tests**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test -- dashboard-view api-modules session-list-view
```

Expected:

```text
PASS
```

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm build
```

Expected:

```text
✓ built in ...
```

- [ ] **Step 5: Manual spot-check against the local database-backed app**

Verify:

1. Dashboard `Active Sessions` matches `SELECT COUNT(*) FROM sessions WHERE status='active';`
2. Admin user can see:
   - owned sessions
   - unowned sessions
   - branch / repo / ownership filters working together
3. Non-admin user can see only their own sessions even if they try to pass `owner_scope=all`
4. Admin can open a session detail page for an unowned or another-user session

- [ ] **Step 6: Commit any final test-only adjustments**

```bash
git add backend/internal/handler/efficiency.go \
        backend/internal/handler/session.go \
        backend/internal/handler/handler_test.go \
        backend/internal/handler/handler_extended_test.go \
        backend/internal/handler/session_detail_http_test.go \
        frontend/src/api/session.ts \
        frontend/src/types/index.ts \
        frontend/src/views/sessions/SessionListView.vue \
        frontend/src/__tests__/api-modules.test.ts \
        frontend/src/__tests__/session-list-view.test.ts
git commit -m "test: finalize session visibility and filters coverage"
```
