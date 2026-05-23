# User Page Group-First Credential Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current `/user` page `provider + platform` approximation with a `provider + group` credential self-serve flow that only shows the current relay user's allowed groups.

**Architecture:** Keep `/user` as a login-protected regular-user page, but make group the first-class object under each provider. Backend must extend the relay adapter to return the current relay user's `allowed_groups`, build `providers[].groups[]` responses, and scope create/regenerate to `provider + group`; frontend must replace platform pills with group selection while keeping the current task-first onboarding shell.

**Tech Stack:** Go (Gin, Ent), Vue 3 (`<script setup>`, Vite, TailwindCSS, Pinia), Vitest, Go test, Docker Compose dev stack

**Spec:** `docs/superpowers/specs/2026-05-21-user-page-cli-self-serve-design.md`

**Status:** Group-first implementation is largely landed in code and verified by backend/frontend tests. Live docker-dev `/api/v1/user/providers` now returns `groups[]`, but the list is currently empty because the upstream relay user payload available in this environment does not yet provide `allowed_groups`; that upstream fact source remains the current blocker for a populated UI. Separately, the admin Settings relay management UI is now aligned to DB-backed multi-`RelayProvider` CRUD via `/api/v1/admin/providers`; `/api/v1/settings/llm*` remains compatibility/runtime-edit surface only. The 2026-05-22 API key visibility follow-up now aligns `/user` with sub2api-style behavior: existing user-owned keys are partially masked on screen and copy the full key when the relay list response includes `key`. The 2026-05-23 provider-test follow-up adds `/api/v1/user/providers/:id/test` and a `/user` page test form so regular users can test their own active API key for the selected group's platform with a caller-supplied model; the old admin Relay Providers test button and `/api/v1/admin/providers/:id/test` route are intentionally removed.

## Follow-up: API Key Visibility Alignment

- [x] Return the existing relay API key value in `/api/v1/user/providers` group credential summaries when `ListUserAPIKeys` includes it.
- [x] Show API keys partially masked in `/user` while keeping the full key available to the Copy action.
- [x] Update the current `/user` contract docs to replace the older one-time-only reveal/copy assumption.

## Follow-up: User Provider Test Migration

- [x] Add a logged-in user route at `POST /api/v1/user/providers/:id/test`.
- [x] Cover the route with a handler test proving a non-admin user tests their own platform-matched API key.
- [x] Add `testUserProvider` to the frontend user API client.
- [x] Add a `/user` page test form that uses the selected group's platform and user-supplied model/prompt.
- [x] Remove the admin Settings Relay Providers test button/dialog and `/api/v1/admin/providers/:id/test` route.
- [x] Update the current `/user` contract docs and architecture notes for the user-scoped provider test flow.

---

## File Structure

### Backend — Modify

| File | Responsibility |
| --- | --- |
| `backend/internal/relay/types.go` | Extend relay user/group DTOs to represent user-visible allowed groups cleanly |
| `backend/internal/relay/provider.go` | Add a relay adapter capability for fetching the current user's allowed groups |
| `backend/internal/relay/sub2api.go` | Implement `allowed_groups` lookup from relay user facts instead of provider-wide active groups |
| `backend/internal/relay/sub2api_test.go` | Cover relay user `allowed_groups` decoding and group-list shaping |
| `backend/internal/usersetup/selection.go` | Replace platform-scoped helpers with group-scoped reusable-key helpers |
| `backend/internal/usersetup/service.go` | Build `providers[].groups[]`, scope create/regenerate to `provider + group`, and stop folding groups by platform |
| `backend/internal/usersetup/service_test.go` | Cover allowed-group filtering, group-scoped list/create/regenerate, and same-name key reuse |
| `backend/internal/handler/user_setup.go` | Replace `/platforms/:platform/credential` routes with `/groups/:group_id/credential` routes |
| `backend/internal/handler/user_setup_test.go` | Update handler tests for group route params and response shape |
| `backend/internal/handler/router.go` | Register `/api/v1/user/providers/:id/groups/:group_id/credential*` endpoints |
| `backend/internal/handler/handler_test.go` | Keep router and auth integration aligned with the revised route surface |
| `backend/internal/handler/auth.go` | Preserve DB-backed `/auth/me` behavior while `/user` moves to group-first |
| `backend/cmd/server/main.go` | Keep startup bootstrap wiring intact while `/user` contract changes |
| `backend/cmd/server/relay_bootstrap.go` | Preserve first-start config-to-DB primary provider bootstrap |
| `backend/cmd/server/relay_bootstrap_test.go` | Lock in startup bootstrap behavior |
| `backend/internal/config/config_test.go` | Preserve the docker-dev config behavior needed for relay auth password decryptability |

### Frontend — Modify

| File | Responsibility |
| --- | --- |
| `frontend/src/api/user.ts` | Replace platform-scoped endpoints with group-scoped endpoints |
| `frontend/src/types/index.ts` | Replace `platforms[]` DTOs with `groups[]` DTOs |
| `frontend/src/views/UserView.vue` | Replace platform selection UI with group selection UI and update copy accordingly |
| `frontend/src/utils/userSetupReview.ts` | Keep current discover/review helpers while clarifying discover's temporary provider-only shape |
| `frontend/src/__tests__/api-user.test.ts` | Update API helper tests for group endpoints |
| `frontend/src/__tests__/api-modules.test.ts` | Keep aggregate API coverage aligned |
| `frontend/src/__tests__/user-view.test.ts` | Cover provider switching, group switching, reveal/copy, and group-scoped create/regenerate |
| `frontend/src/__tests__/router.test.ts` | Keep `/user` route coverage intact |
| `frontend/src/__tests__/app-sidebar.test.ts` | Keep footer navigation coverage intact |

### Docs — Modify

| File | Responsibility |
| --- | --- |
| `docs/architecture.md` | Reflect `/user` as provider-first, group-second self-serve using `allowed_groups` |
| `docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md` | This live plan; update checkboxes as each real step completes |

---

### Task 1: Extend the Relay Adapter with Allowed-Groups Support

**Files:**
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`

- [ ] **Step 1: Write failing relay adapter tests for allowed-groups decoding**

```go
func TestGetUserIncludesAllowedGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "admin",
				"allowed_groups": []any{
					map[string]any{"id": 6, "name": "Group Alpha", "platform": "openai"},
					map[string]any{"id": 10, "name": "Group Delta", "platform": "gemini"},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.GetUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
		{ID: 10, Name: "Group Delta", Platform: "gemini"},
	}, user.AllowedGroups); diff != "" {
		t.Fatalf("allowed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestListAllowedGroupsForUserUsesRelayUserFacts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "admin",
				"allowed_groups": []any{
					map[string]any{"id": 5, "name": "Group Gamma", "platform": "anthropic"},
					map[string]any{"id": 6, "name": "Group Alpha", "platform": "openai"},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	lister, ok := p.(interface {
		ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error)
	})
	if !ok {
		t.Fatal("provider does not implement ListAllowedGroupsForUser")
	}
	groups, err := lister.ListAllowedGroupsForUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAllowedGroupsForUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 5, Name: "Group Gamma", Platform: "anthropic"},
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
	}, groups); diff != "" {
		t.Fatalf("allowed groups mismatch (-want +got):\n%s", diff)
	}
}
```

- [ ] **Step 2: Run the relay tests to verify they fail**

Run:

```bash
cd backend && go test ./internal/relay -run 'TestGetUserIncludesAllowedGroups|TestListAllowedGroupsForUserUsesRelayUserFacts' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/relay
... user.AllowedGroups undefined
```

- [ ] **Step 3: Implement the minimal relay DTO and adapter support**

```go
type User struct {
	ID            int64   `json:"id"`
	Email         string  `json:"email"`
	Username      string  `json:"username"`
	Role          string  `json:"role"`
	Concurrency   int     `json:"concurrency,omitempty"`
	AllowedGroups []Group `json:"allowed_groups,omitempty"`
}

type allowedGroupLister interface {
	ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]Group, error)
}

func (s *sub2apiRelay) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]Group, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("relay: list allowed groups: %w", err)
	}
	if user == nil {
		return nil, nil
	}
	return user.AllowedGroups, nil
}
```

- [ ] **Step 4: Run the relay tests and package sweep**

Run:

```bash
cd backend && go test ./internal/relay -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/relay
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/relay/types.go backend/internal/relay/provider.go backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go
git commit -m "feat(relay): expose user allowed groups"
```

---

### Task 2: Rebuild Usersetup Service Around Provider Plus Group

**Files:**
- Modify: `backend/internal/usersetup/selection.go`
- Modify: `backend/internal/usersetup/service.go`
- Modify: `backend/internal/usersetup/service_test.go`

- [ ] **Step 1: Write failing service tests for group-first summaries**

```go
func TestListProvidersReturnsOnlyAllowedGroups(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://sub2api.agoraio.cn/").
		SetAdminURL("https://sub2api.agoraio.cn/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			1: {
				{ID: 20, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 6, Name: "Group Alpha", Platform: "openai"}, CreatedAt: time.Now()},
				{ID: 21, UserID: 1, Name: "other", Status: "active", Group: &relay.Group{ID: 99, Name: "Other", Platform: "openai"}, CreatedAt: time.Now()},
			},
		},
		listAllowedGroupsForUserFn: func(_ context.Context, userID int64) ([]relay.Group, error) {
			if userID != 1 {
				t.Fatalf("userID = %d, want 1", userID)
			}
			return []relay.Group{
				{ID: 5, Name: "Group Gamma", Platform: "anthropic"},
				{ID: 6, Name: "Group Alpha", Platform: "openai"},
			}, nil
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), "d98460dc58409c713d1586802217c23932d58c95479641e4b0fec1c740386696")

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: localUser.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"5", "6"}, groupIDs(resp.Providers[0].Groups)); diff != "" {
		t.Fatalf("group mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateGroupCredentialUsesSelectedGroupID(t *testing.T) {
	// create request must use req.GroupID directly, not platform default-group lookup
}

func TestRegenerateGroupCredentialOnlyTouchesSelectedGroup(t *testing.T) {
	// if two openai groups exist, regenerating one must not touch the other
}
```

- [ ] **Step 2: Run the service tests to verify they fail**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/usersetup -run 'TestListProvidersReturnsOnlyAllowedGroups|TestCreateGroupCredentialUsesSelectedGroupID|TestRegenerateGroupCredentialOnlyTouchesSelectedGroup' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/usersetup
... ProviderSummary.Groups undefined
```

- [ ] **Step 3: Implement the minimal group-first DTOs and logic**

```go
type GroupCredentialSummary struct {
	GroupID    string                 `json:"group_id"`
	GroupName  string                 `json:"group_name"`
	Platform   string                 `json:"platform"`
	Credential GroupCredentialState   `json:"credential"`
}

type ProviderSummary struct {
	ID           int                    `json:"id"`
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	BaseURL      string                 `json:"base_url"`
	DefaultModel string                 `json:"default_model"`
	IsPrimary    bool                   `json:"is_primary"`
	Groups       []GroupCredentialSummary `json:"groups"`
}

func (s *Service) summarizeGroups(ctx context.Context, rp relay.Provider, relayUserID int64, username, email string, keys []relay.APIKey) ([]GroupCredentialSummary, error) {
	// 1. Ask rp for ListAllowedGroupsForUser(relayUserID)
	// 2. Build summaries keyed by group ID, not platform
	// 3. Mark credential state using matching active keys within the exact group ID
}
```

- [ ] **Step 4: Run the usersetup tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/usersetup -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/usersetup
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usersetup/selection.go backend/internal/usersetup/service.go backend/internal/usersetup/service_test.go
git commit -m "feat(backend): add group-first user setup service"
```

---

### Task 3: Replace User API Routes with Group-Scoped Endpoints

**Files:**
- Modify: `backend/internal/handler/user_setup.go`
- Modify: `backend/internal/handler/user_setup_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/handler_test.go`

- [ ] **Step 1: Write failing handler tests for group routes**

```go
func TestUserProvidersReturnsGroupsPerProvider(t *testing.T) {
	// response should contain "groups" and never "platforms"
}

func TestCreateGroupCredentialTranslatesRouteParams(t *testing.T) {
	// POST /api/v1/user/providers/7/groups/42/credential
}

func TestRegenerateGroupCredentialTranslatesRouteParams(t *testing.T) {
	// POST /api/v1/user/providers/7/groups/42/credential/regenerate
}
```

- [ ] **Step 2: Run handler tests to verify they fail**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run 'TestUserProvidersReturnsGroupsPerProvider|TestCreateGroupCredentialTranslatesRouteParams|TestRegenerateGroupCredentialTranslatesRouteParams' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/handler
... CreateGroupCredential undefined
```

- [ ] **Step 3: Implement group-scoped handlers and routes**

```go
userGroup := protected.Group("/user")
{
	userGroup.GET("/providers", userSetupHandler.ListProviders)
	userGroup.POST("/providers/:id/groups/:group_id/credential", userSetupHandler.CreateGroupCredential)
	userGroup.POST("/providers/:id/groups/:group_id/credential/regenerate", userSetupHandler.RegenerateGroupCredential)
}
```

- [ ] **Step 4: Run handler tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/handler
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/user_setup.go backend/internal/handler/user_setup_test.go backend/internal/handler/router.go backend/internal/handler/handler_test.go
git commit -m "feat(backend): add group-scoped user setup routes"
```

---

### Task 4: Rebuild Frontend DTOs and UserView Around Groups

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/user.ts`
- Modify: `frontend/src/views/UserView.vue`
- Modify: `frontend/src/__tests__/api-user.test.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/user-view.test.ts`
- Modify: `frontend/src/utils/userSetupReview.ts`

- [ ] **Step 1: Write failing frontend tests for group-first DTOs and UI**

```ts
it('createGroupCredential posts to the provider-and-group endpoint', async () => {
  await createGroupCredential(7, '42')
  expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/groups/42/credential')
})

it('renders group pills instead of platform pills', async () => {
  const wrapper = await mountUserView()
  expect(wrapper.text()).toContain('Group Alpha')
  expect(wrapper.text()).toContain('Platform: openai')
})
```

- [ ] **Step 2: Run the targeted frontend tests to verify they fail**

Run:

```bash
cd frontend && pnpm test src/__tests__/api-user.test.ts src/__tests__/user-view.test.ts
```

Expected:

```text
FAIL  src/__tests__/user-view.test.ts
... expected text to contain Group Alpha
```

- [ ] **Step 3: Implement the minimal group-first frontend shape**

```ts
export interface UserGroupCredentialSummary {
  group_id: string
  group_name: string
  platform: string
  credential: {
    state: 'missing' | 'existing_hidden'
    api_key_id?: number
    name?: string
    status?: string
    created_at?: string | null
    last_used_at?: string | null
  }
}

export function createGroupCredential(providerId: number, groupId: string) {
  return client.post<ApiResponse<GroupCredentialMutationResult>>(
    `/user/providers/${providerId}/groups/${groupId}/credential`
  )
}
```

In `UserView.vue`, replace `selectedPlatformName` with `selectedGroupId`, render `selectedProvider.groups`, and show `group_name` plus `platform`.

- [ ] **Step 4: Run the targeted frontend tests**

Run:

```bash
cd frontend && pnpm test src/__tests__/api-user.test.ts src/__tests__/api-modules.test.ts src/__tests__/user-view.test.ts
```

Expected:

```text
PASS  src/__tests__/api-user.test.ts
PASS  src/__tests__/api-modules.test.ts
PASS  src/__tests__/user-view.test.ts
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/api/user.ts frontend/src/views/UserView.vue frontend/src/__tests__/api-user.test.ts frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/user-view.test.ts frontend/src/utils/userSetupReview.ts
git commit -m "feat(frontend): switch user setup ui to groups"
```

---

### Task 5: Update Architecture Docs and Record Current Status

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md`

- [ ] **Step 1: Update architecture wording**

```md
- The embedded SPA exposes a regular-user `/user` surface for profile summary, provider-aware CLI guidance, and provider-first, group-second credential self-serve.
- `/user` groups come from the current relay user's allowed groups, not provider-wide active group enumeration.
```

- [ ] **Step 2: Run a diff sanity check**

Run:

```bash
git diff --check docs/architecture.md docs/superpowers/specs/2026-05-21-user-page-cli-self-serve-design.md docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md
```

Expected:

```text
# no output
```

- [ ] **Step 3: Commit**

```bash
git add docs/architecture.md docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md
git commit -m "docs(architecture): document group-first user setup"
```

---

### Task 6: Run Verification and Docker-Dev Validation

**Files:**
- Modify: `docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md`

- [ ] **Step 1: Run backend tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/relay ./internal/usersetup ./internal/handler ./cmd/server -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/relay
ok      github.com/ai-efficiency/backend/internal/usersetup
ok      github.com/ai-efficiency/backend/internal/handler
ok      github.com/ai-efficiency/backend/cmd/server
```

- [ ] **Step 2: Run frontend tests**

Run:

```bash
cd frontend && pnpm test
```

Expected:

```text
Test Files  ... passed
Tests       ... passed
```

- [ ] **Step 3: Rebuild docker-dev backend**

Run:

```bash
AE_DEV_LOGIN_ENABLED=true docker compose -p ai-efficiency -f deploy/docker-compose.dev.yml up -d --build backend
```

Expected:

```text
... backend container recreated successfully
```

- [ ] **Step 4: Verify live API returns groups**

Run:

```bash
curl -s http://127.0.0.1:18081/api/v1/health/ready
```

Expected:

```json
{"status":"ready", ...}
```

Then verify with an authenticated request that `/api/v1/user/providers` returns `groups[]`, not `platforms[]`, and only the current relay user's allowed groups appear.

- [ ] **Step 5: Mark completed plan state and commit**

```md
**Status:** Group-first implementation verified with backend/frontend tests and docker-dev live API checks.
```

```bash
git add docs/superpowers/plans/2026-05-21-user-page-cli-self-serve.md
git commit -m "test(docs): record group-first user setup verification"
```

---

## Self-Review

- Spec coverage:
  - `/user` route and task-first shell: Task 4
  - provider-first, group-second IA: Tasks 2, 3, 4
  - `allowed_groups` source of truth: Task 1
  - group-scoped credential actions: Tasks 2 and 3
  - discover unchanged but documented as follow-up: Tasks 4 and 5
  - docker-dev verification: Task 6
- Placeholder scan:
  - No `TODO`/`TBD` markers remain.
  - Each task has concrete files, commands, and expected outcomes.
- Type consistency:
  - Backend uses `groups[]` and `GroupCredentialSummary`.
  - Frontend uses `createGroupCredential` / `regenerateGroupCredential`.
  - Group remains the selectable object; platform remains a group attribute only.
