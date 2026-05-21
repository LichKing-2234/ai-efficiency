# User Page CLI Self-Serve Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a login-protected `/user` page for regular developers that shows profile info, provider-aware CLI setup guidance, managed API key self-serve actions, and lightweight verify-result review.

**Architecture:** Backend adds a focused `usersetup` service plus user-scoped `/api/v1/user/providers` and managed-key mutation endpoints, without changing the existing CLI-facing `/api/v1/providers` contract. Frontend adds a new `/user` route, account-page navigation, a dedicated `UserView`, and small supporting API/util modules so provider selection, managed-key state, and verify-result review stay out of the router and auth store.

**Tech Stack:** Go (Gin, Ent), Vue 3 (`<script setup>`, Vite, TailwindCSS, Pinia), Vitest, Go test

**Spec:** `docs/superpowers/specs/2026-05-21-user-page-cli-self-serve-design.md`

---

## File Structure

### Backend — Create

| File | Responsibility |
| --- | --- |
| `backend/internal/usersetup/service.go` | Query enabled providers for the current user, resolve relay identity, select the canonical managed key, create/regenerate managed keys, and shape user-page DTOs |
| `backend/internal/usersetup/service_test.go` | Service-level tests for `missing`, `existing_hidden`, canonical key selection, create, regenerate, and relay-user edge cases |
| `backend/internal/handler/user_setup.go` | HTTP handlers for `/api/v1/user/providers`, `/api/v1/user/providers/:id/managed-key`, and `/api/v1/user/providers/:id/managed-key/regenerate` |
| `backend/internal/handler/user_setup_test.go` | Handler tests for auth, user scoping, managed-key mutations, and error translation |

### Backend — Modify

| File | Responsibility |
| --- | --- |
| `backend/internal/handler/router.go` | Register the new protected `/user/*` endpoints |
| `backend/internal/handler/provider.go` | Optionally extract shared provider cache / relay-provider resolution helpers if `usersetup` reuses them cleanly; do not change `/api/v1/providers` behavior |
| `docs/architecture.md` | Add the `/user` page and note that it is the ordinary developer self-serve surface for CLI setup and managed keys |

### Frontend — Create

| File | Responsibility |
| --- | --- |
| `frontend/src/api/user.ts` | Typed helpers for `/user/providers`, create-key, and regenerate-key endpoints |
| `frontend/src/views/UserView.vue` | `/user` account page with profile summary, provider switcher, CLI setup checklist, managed key card, and verify review area |
| `frontend/src/utils/userSetupReview.ts` | Pure functions for lightweight verify-result review and command string generation |
| `frontend/src/__tests__/api-user.test.ts` | API helper tests for the new user endpoints |
| `frontend/src/__tests__/user-setup-review.test.ts` | Unit tests for verify-result review logic and provider-driven command generation |
| `frontend/src/__tests__/user-view.test.ts` | View tests for route rendering, provider switching, managed-key states, reveal/copy/regenerate behavior, and verify review |

### Frontend — Modify

| File | Responsibility |
| --- | --- |
| `frontend/src/router/index.ts` | Register `/user` route |
| `frontend/src/components/AppSidebar.vue` | Make the footer user block link into `/user` while preserving logout behavior |
| `frontend/src/types/index.ts` | Add DTOs for user-provider summary, managed-key state, and verify review status |
| `frontend/src/__tests__/router.test.ts` | Assert `/user` route exists and stays protected |
| `frontend/src/__tests__/app-sidebar.test.ts` | Assert footer account link navigates to `/user` |
| `frontend/src/__tests__/api-modules.test.ts` | Include the new user API module in the aggregate API smoke tests so the central API suite stays complete |

---

### Task 1: Backend User Setup Service

**Files:**
- Create: `backend/internal/usersetup/service.go`
- Test: `backend/internal/usersetup/service_test.go`

- [x] **Step 1: Write the failing service tests**

```go
func TestListProvidersReturnsMissingWhenNoManagedKeyExists(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("encrypted").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	relayID := 101
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: user.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(resp.Providers))
	}
	if resp.Providers[0].ManagedKey.State != "missing" {
		t.Fatalf("managed key state = %q, want missing", resp.Providers[0].ManagedKey.State)
	}
}

func TestListProvidersReturnsExistingHiddenForLatestActiveManagedKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("encrypted").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SetRelayUserID(relayID).
		SaveX(ctx)

	oldTime := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 10, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: oldTime},
				{ID: 11, UserID: int64(relayID), Name: "manual-key", Status: "active", CreatedAt: newTime},
				{ID: 12, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: newTime},
			},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: user.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	got := resp.Providers[0].ManagedKey
	if got.State != "existing_hidden" {
		t.Fatalf("managed key state = %q, want existing_hidden", got.State)
	}
	if got.APIKeyID != 12 {
		t.Fatalf("api key id = %d, want 12", got.APIKeyID)
	}
}

func TestCreateManagedKeyRejectsExistingManagedKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("encrypted").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 22, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now()},
			},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}))

	_, err := svc.CreateManagedKey(ctx, usersetup.CreateManagedKeyRequest{UserID: user.ID, ProviderID: provider.ID})
	if !errors.Is(err, usersetup.ErrManagedKeyAlreadyExists) {
		t.Fatalf("CreateManagedKey() error = %v, want ErrManagedKeyAlreadyExists", err)
	}
}

func TestRegenerateManagedKeyRevokesAllActiveManagedKeysAndCreatesANewOne(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("encrypted").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("sub2api_sso").
		SetRole("user").
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 30, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now().Add(-time.Hour)},
				{ID: 31, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now()},
			},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{ID: 99, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active"},
			Secret: "sk-new-managed-key",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}))

	got, err := svc.RegenerateManagedKey(ctx, usersetup.RegenerateManagedKeyRequest{UserID: user.ID, ProviderID: provider.ID})
	if err != nil {
		t.Fatalf("RegenerateManagedKey() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]int64{30, 31}, fakeRelay.revokedKeyIDs); diff != "" {
		t.Fatalf("revoked ids mismatch (-want +got):\n%s", diff)
	}
	if got.Secret != "sk-new-managed-key" {
		t.Fatalf("secret = %q, want sk-new-managed-key", got.Secret)
	}
}
```

- [x] **Step 2: Run the new service tests to verify they fail**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/usersetup -run 'TestListProvidersReturnsMissingWhenNoManagedKeyExists|TestListProvidersReturnsExistingHiddenForLatestActiveManagedKey|TestCreateManagedKeyRejectsExistingManagedKey|TestRegenerateManagedKeyRevokesAllActiveManagedKeysAndCreatesANewOne' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/usersetup
... no Go files in .../backend/internal/usersetup
```

- [x] **Step 3: Implement the service and DTOs**

```go
type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ManagedKeySummary struct {
	State      string     `json:"state"`
	APIKeyID   int64      `json:"api_key_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type ProviderSummary struct {
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name"`
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	IsPrimary    bool              `json:"is_primary"`
	ManagedKey   ManagedKeySummary `json:"managed_key"`
}

type CreateManagedKeyResult struct {
	APIKeyID int64  `json:"api_key_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Secret   string `json:"secret"`
}

func (s *Service) ListProviders(ctx context.Context, req ListProvidersRequest) (*ListProvidersResponse, error) {
	// 1. Load the local user by req.UserID.
	// 2. Ensure the user has RelayUserID or return a user-readable empty-provider response.
	// 3. Query enabled relay providers ordered primary-first, then ID.
	// 4. Resolve relay.Provider per row, list keys, filter to active ae-cli-auto keys.
	// 5. Select the newest active managed key by CreatedAt desc (fallback ID desc).
	// 6. Return state "missing" or "existing_hidden"; never return secret.
}

func (s *Service) CreateManagedKey(ctx context.Context, req CreateManagedKeyRequest) (*CreateManagedKeyResult, error) {
	// 1. Load local user and provider.
	// 2. Resolve relay provider and list existing active ae-cli-auto keys.
	// 3. Return ErrManagedKeyAlreadyExists if one exists.
	// 4. Create a new ae-cli-auto key and return the secret once.
}

func (s *Service) RegenerateManagedKey(ctx context.Context, req RegenerateManagedKeyRequest) (*CreateManagedKeyResult, error) {
	// 1. Load local user and provider.
	// 2. Resolve relay provider and revoke every active ae-cli-auto key.
	// 3. Create a single new ae-cli-auto key.
	// 4. Return the new secret once.
}
```

- [x] **Step 4: Run the service package tests and a small backend sweep**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/usersetup -count=1 && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/usersetup ./internal/relay -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/usersetup
ok      github.com/ai-efficiency/backend/internal/relay
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usersetup/service.go backend/internal/usersetup/service_test.go
git commit -m "feat(backend): add user setup service"
```

---

### Task 2: Backend User Setup HTTP API

**Files:**
- Create: `backend/internal/handler/user_setup.go`
- Test: `backend/internal/handler/user_setup_test.go`
- Modify: `backend/internal/handler/router.go`

- [ ] **Step 1: Write failing handler tests for list, create, and regenerate**

```go
func TestUserProvidersRequiresAuth(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequestWithToken(env, http.MethodGet, "/api/v1/user/providers", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestUserProvidersReturnsCurrentUserProviders(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		listResponse: &usersetup.ListProvidersResponse{
			Providers: []usersetup.ProviderSummary{
				{
					ID:           7,
					Name:         "sub2api-prod",
					DisplayName:  "sub2api Production",
					BaseURL:      "https://relay.example.com",
					DefaultModel: "claude-sonnet-4-20250514",
					IsPrimary:    true,
					ManagedKey:   usersetup.ManagedKeySummary{State: "existing_hidden", APIKeyID: 44, Name: "ae-cli-auto", Status: "active"},
				},
			},
		},
	}
	router := gin.New()
	router.GET("/api/v1/user/providers", auth.RequireAuth(env.authSvc), NewUserSetupHandler(stub).ListProviders)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/providers", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastListReq.UserID != env.userID {
		t.Fatalf("user id = %d, want %d", stub.lastListReq.UserID, env.userID)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response should not contain secret: %s", w.Body.String())
	}
}

func TestCreateManagedKeyReturnsSecretOnce(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		createResult: &usersetup.CreateManagedKeyResult{
			APIKeyID: 77,
			Name:     "ae-cli-auto",
			Status:   "active",
			Secret:   "sk-new",
		},
	}
	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key", auth.RequireAuth(env.authSvc), NewUserSetupHandler(stub).CreateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastCreateReq.ProviderID != 7 {
		t.Fatalf("provider id = %d, want 7", stub.lastCreateReq.ProviderID)
	}
	if !strings.Contains(w.Body.String(), "sk-new") {
		t.Fatalf("response missing secret: %s", w.Body.String())
	}
}

func TestCreateManagedKeyConflictsWhenManagedKeyExists(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{createErr: usersetup.ErrManagedKeyAlreadyExists}
	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key", auth.RequireAuth(env.authSvc), NewUserSetupHandler(stub).CreateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestRegenerateManagedKeyTranslatesProviderIDAndReturnsSecret(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		regenerateResult: &usersetup.CreateManagedKeyResult{
			APIKeyID: 88,
			Name:     "ae-cli-auto",
			Status:   "active",
			Secret:   "sk-regen",
		},
	}
	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key/regenerate", auth.RequireAuth(env.authSvc), NewUserSetupHandler(stub).RegenerateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastRegenerateReq.UserID != env.userID || stub.lastRegenerateReq.ProviderID != 7 {
		t.Fatalf("got request %#v, want user=%d provider=7", stub.lastRegenerateReq, env.userID)
	}
}
```

- [ ] **Step 2: Run the handler tests to verify they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestUserProvidersRequiresAuth|TestUserProvidersReturnsCurrentUserProviders|TestCreateManagedKeyReturnsSecretOnce|TestCreateManagedKeyConflictsWhenManagedKeyExists|TestRegenerateManagedKeyTranslatesProviderIDAndReturnsSecret' -count=1
```

Expected:

```text
FAIL    github.com/ai-efficiency/backend/internal/handler
... undefined: NewUserSetupHandler
```

- [ ] **Step 3: Implement the handler and wire the routes**

```go
type userSetupService interface {
	ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error)
	CreateManagedKey(ctx context.Context, req usersetup.CreateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error)
	RegenerateManagedKey(ctx context.Context, req usersetup.RegenerateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error)
}

type UserSetupHandler struct {
	service userSetupService
}

func (h *UserSetupHandler) ListProviders(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp, err := h.service.ListProviders(c.Request.Context(), usersetup.ListProvidersRequest{UserID: uc.UserID})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, gin.H{"providers": resp.Providers, "message": resp.Message})
}

func (h *UserSetupHandler) CreateManagedKey(c *gin.Context) {
	// Parse provider ID from :id, call service with uc.UserID,
	// translate ErrManagedKeyAlreadyExists to HTTP 409.
}

func (h *UserSetupHandler) RegenerateManagedKey(c *gin.Context) {
	// Parse provider ID, call service, return the new key envelope.
}
```

Register routes inside the protected `/api/v1` group:

```go
userGroup := protected.Group("/user")
{
	userGroup.GET("/providers", userSetupHandler.ListProviders)
	userGroup.POST("/providers/:id/managed-key", userSetupHandler.CreateManagedKey)
	userGroup.POST("/providers/:id/managed-key/regenerate", userSetupHandler.RegenerateManagedKey)
}
```

- [ ] **Step 4: Run the handler tests and a router sweep**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestUserProvidersRequiresAuth|TestUserProvidersReturnsCurrentUserProviders|TestCreateManagedKeyReturnsSecretOnce|TestCreateManagedKeyConflictsWhenManagedKeyExists|TestRegenerateManagedKeyTranslatesProviderIDAndReturnsSecret' -count=1 && go test ./internal/handler -run 'TestAuthMeWithValidToken|TestListProvidersForUserWithValidToken' -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/handler
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/user_setup.go backend/internal/handler/user_setup_test.go backend/internal/handler/router.go
git commit -m "feat(backend): add user setup endpoints"
```

---

### Task 3: Frontend API, Types, and Pure Review Logic

**Files:**
- Create: `frontend/src/api/user.ts`
- Create: `frontend/src/utils/userSetupReview.ts`
- Create: `frontend/src/__tests__/api-user.test.ts`
- Create: `frontend/src/__tests__/user-setup-review.test.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`

- [ ] **Step 1: Write failing API and utility tests**

```ts
it('getUserProviders calls GET /user/providers', async () => {
  mockClient.get.mockResolvedValue({ data: { data: { providers: [] } } })
  await getUserProviders()
  expect(mockClient.get).toHaveBeenCalledWith('/user/providers')
})

it('createManagedKey posts to the provider-scoped endpoint', async () => {
  mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 7, secret: 'sk-new' } } })
  await createManagedKey(7)
  expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/managed-key')
})

it('reviewVerifyOutput flags discover output without the selected provider as needs_attention', () => {
  const result = reviewVerifyOutput({
    selectedProviderName: 'sub2api-prod',
    versionOutput: 'ae-cli version v0.2.0',
    discoverOutput: 'Would update ~/.codex/config.toml with provider staging',
    doctorOutput: 'all good',
  })
  expect(result.discover.status).toBe('needs_attention')
})

it('buildDiscoverCommand includes the selected provider and current origin', () => {
  expect(buildDiscoverCommand('https://ae.example.com', 'sub2api-prod')).toBe(
    'ae-cli --server https://ae.example.com discover --provider sub2api-prod'
  )
})
```

- [ ] **Step 2: Run the frontend unit tests to verify they fail**

Run:

```bash
cd frontend && pnpm test -- --run src/__tests__/api-user.test.ts src/__tests__/user-setup-review.test.ts
```

Expected:

```text
FAIL  src/__tests__/api-user.test.ts
Error: Failed to resolve import "@/api/user"
```

- [ ] **Step 3: Implement DTOs, API helpers, and pure review functions**

```ts
export interface ManagedKeySummary {
  state: 'missing' | 'existing_hidden'
  api_key_id?: number
  name?: string
  status?: string
  created_at?: string | null
  last_used_at?: string | null
}

export interface UserProviderSummary {
  id: number
  name: string
  display_name: string
  base_url: string
  default_model: string
  is_primary: boolean
  managed_key: ManagedKeySummary
}

export interface ManagedKeyMutationResult {
  api_key_id: number
  name: string
  status: string
  secret: string
}

export function getUserProviders() {
  return client.get<ApiResponse<{ providers: UserProviderSummary[]; message?: string }>>('/user/providers')
}

export function createManagedKey(providerId: number) {
  return client.post<ApiResponse<ManagedKeyMutationResult>>(`/user/providers/${providerId}/managed-key`)
}

export function regenerateManagedKey(providerId: number) {
  return client.post<ApiResponse<ManagedKeyMutationResult>>(`/user/providers/${providerId}/managed-key/regenerate`)
}

export function reviewVerifyOutput(input: ReviewInput): ReviewSummary {
  // 1. Mark version "looks_good" when output includes "ae-cli".
  // 2. Mark discover "looks_good" when output includes the selected provider and
  //    one of ~/.codex/config.toml, ~/.ae-cli/env.sh, ~/.claude/settings.json.
  // 3. Mark doctor "needs_attention" when output contains "error", "failed", or "unauthorized" (case-insensitive).
  // 4. Otherwise return "cannot_determine" with explicit guidance.
}

export function buildLoginCommand(origin: string) {
  return `ae-cli --server ${origin} login`
}

export function buildDiscoverCommand(origin: string, providerName: string) {
  return `ae-cli --server ${origin} discover --provider ${providerName}`
}
```

- [ ] **Step 4: Run the focused tests and the aggregate API smoke test**

Run:

```bash
cd frontend && pnpm test -- --run src/__tests__/api-user.test.ts src/__tests__/user-setup-review.test.ts src/__tests__/api-modules.test.ts
```

Expected:

```text
✓ src/__tests__/api-user.test.ts
✓ src/__tests__/user-setup-review.test.ts
✓ src/__tests__/api-modules.test.ts
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/user.ts frontend/src/utils/userSetupReview.ts frontend/src/types/index.ts frontend/src/__tests__/api-user.test.ts frontend/src/__tests__/user-setup-review.test.ts frontend/src/__tests__/api-modules.test.ts
git commit -m "feat(frontend): add user setup client utilities"
```

---

### Task 4: Frontend `/user` Page and Navigation

**Files:**
- Create: `frontend/src/views/UserView.vue`
- Test: `frontend/src/__tests__/user-view.test.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/AppSidebar.vue`
- Modify: `frontend/src/__tests__/router.test.ts`
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`

- [ ] **Step 1: Write failing route, sidebar, and page tests**

```ts
it('includes /user in the application router', () => {
  const userRoute = router.getRoutes().find((r) => r.name === 'User')
  expect(userRoute?.path).toBe('/user')
})

it('navigates to /user when clicking the footer account area', async () => {
  const wrapper = mount(AppSidebar, { global: { plugins: [pinia, router] } })
  await wrapper.get('[data-testid="sidebar-account-link"]').trigger('click')
  expect(router.currentRoute.value.path).toBe('/user')
})

it('loads profile and provider data, selects primary provider by default, and renders commands', async () => {
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          { id: 1, name: 'staging', display_name: 'Staging', base_url: 'https://staging.example.com', default_model: 'claude-sonnet', is_primary: false, managed_key: { state: 'missing' } },
          { id: 2, name: 'prod', display_name: 'Production', base_url: 'https://prod.example.com', default_model: 'claude-sonnet', is_primary: true, managed_key: { state: 'existing_hidden', api_key_id: 22 } },
        ],
      },
    },
  })
  const wrapper = await mountUserView()
  expect(wrapper.text()).toContain('alice@example.com')
  expect(wrapper.text()).toContain('Production')
  expect(wrapper.text()).toContain('ae-cli --server http://localhost')
})

it('reveals and copies a newly created secret only from session state', async () => {
  // Click Create Key, assert secret is hidden until Reveal, then Copy uses navigator.clipboard.writeText('sk-new').
})

it('shows regenerate confirmation for existing hidden keys', async () => {
  // Assert the warning text mentions rerunning discover after regeneration.
})
```

- [ ] **Step 2: Run the frontend tests to verify they fail**

Run:

```bash
cd frontend && pnpm test -- --run src/__tests__/router.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/user-view.test.ts
```

Expected:

```text
FAIL  src/__tests__/user-view.test.ts
Error: Failed to resolve import "@/views/UserView.vue"
```

- [ ] **Step 3: Implement the route, sidebar entry, and `UserView`**

```ts
{
  path: '/user',
  name: 'User',
  component: () => import('@/views/UserView.vue'),
}
```

```vue
<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { getUserProviders, createManagedKey, regenerateManagedKey } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import {
  buildDiscoverCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildDeviceLoginCommand,
  reviewVerifyOutput,
} from '@/utils/userSetupReview'

const auth = useAuthStore()
const providers = ref<UserProviderSummary[]>([])
const selectedProviderId = ref<number | null>(null)
const sessionSecrets = reactive<Record<number, string>>({})
const revealedProviderIds = reactive<Record<number, boolean>>({})
const verifyDrafts = reactive<Record<number, { version: string; discover: string; doctor: string }>>({})

const selectedProvider = computed(() => providers.value.find((p) => p.id === selectedProviderId.value) ?? null)
const currentOrigin = computed(() => window.location.origin)
const selectedSecret = computed(() => selectedProvider.value ? sessionSecrets[selectedProvider.value.id] ?? '' : '')
const canReveal = computed(() => !!selectedProvider.value && !!sessionSecrets[selectedProvider.value.id])
const revealValue = computed(() => (selectedProvider.value && revealedProviderIds[selectedProvider.value.id]) ? sessionSecrets[selectedProvider.value.id] : '')

function selectDefaultProvider(rows: UserProviderSummary[]) {
  const primary = rows.find((p) => p.is_primary)
  selectedProviderId.value = primary?.id ?? rows[0]?.id ?? null
}

async function handleCreateKey() {
  // Call createManagedKey(selectedProvider.id), store secret in sessionSecrets, keep provider managed_key as existing_hidden after reload only.
}

async function handleRegenerateKey() {
  // Require a confirm dialog, then call regenerateManagedKey and replace the session secret.
}

function handleReviewVerify() {
  // Use reviewVerifyOutput for the current provider draft and render three statuses plus a summary banner.
}
</script>
```

The rendered page must include:

1. `Profile Summary` with `username`, `email`, `role`, `auth_source`
2. Single-select provider chips or cards
3. Install/login/discover command blocks with copy buttons
4. Managed key card with state-specific CTA:
   - `Create Key` for `missing`
   - `Regenerate` for `existing_hidden`
   - `Reveal` / `Copy` only when a session secret exists
5. Verify textareas for version / discover dry-run / doctor and a `Review` button
6. A short FAQ footer

- [ ] **Step 4: Run the focused frontend tests and full unit suite**

Run:

```bash
cd frontend && pnpm test -- --run src/__tests__/router.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/user-view.test.ts && pnpm test
```

Expected:

```text
✓ src/__tests__/router.test.ts
✓ src/__tests__/app-sidebar.test.ts
✓ src/__tests__/user-view.test.ts
...
Test Files  ... passed
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/UserView.vue frontend/src/router/index.ts frontend/src/components/AppSidebar.vue frontend/src/__tests__/router.test.ts frontend/src/__tests__/app-sidebar.test.ts frontend/src/__tests__/user-view.test.ts
git commit -m "feat(frontend): add user setup page"
```

---

### Task 5: Architecture and Contract Documentation

**Files:**
- Modify: `docs/architecture.md`
- Modify: `AGENTS.md` only if the implementation introduces a durable repo-level working rule (do not edit if no new rule is created)

- [ ] **Step 1: Write the failing documentation expectations as a checklist**

```md
- `docs/architecture.md` must mention `/user` as the ordinary developer self-serve page.
- The frontend section must no longer describe views only as dashboard/repos/oauth/admin settings.
- Runtime notes should mention that ordinary users can manage provider-aware CLI setup and managed keys from the embedded SPA.
```

- [ ] **Step 2: Update `docs/architecture.md`**

```md
| Views | `frontend/src/views` | Dashboard, repos, events, oauth, user self-serve, and admin/settings pages |
```

Add a short runtime note near the frontend/backend interaction section explaining that the embedded SPA now exposes a `/user` surface for install/login/discover guidance plus provider-scoped managed-key self-serve, while the CLI-facing `/api/v1/providers` contract remains separate.

- [ ] **Step 3: Run a lightweight verification pass**

Run:

```bash
git diff --check docs/architecture.md docs/superpowers/specs/2026-05-21-user-page-cli-self-serve-design.md
```

Expected:

```text
# no output
```

- [ ] **Step 4: Commit**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): add user setup surface"
```

---

### Task 6: End-to-End Verification

**Files:**
- No code changes required unless verification exposes defects

- [ ] **Step 1: Run the backend targeted tests**

Run:

```bash
cd backend && go test ./internal/usersetup ./internal/handler -count=1
```

Expected:

```text
ok      github.com/ai-efficiency/backend/internal/usersetup
ok      github.com/ai-efficiency/backend/internal/handler
```

- [ ] **Step 2: Run the frontend targeted and full unit tests**

Run:

```bash
cd frontend && pnpm test -- --run src/__tests__/api-user.test.ts src/__tests__/user-setup-review.test.ts src/__tests__/user-view.test.ts && pnpm test
```

Expected:

```text
✓ src/__tests__/api-user.test.ts
✓ src/__tests__/user-setup-review.test.ts
✓ src/__tests__/user-view.test.ts
...
```

- [ ] **Step 3: Run one manual local-browser verification**

Run:

```bash
cd frontend && pnpm build
```

Then start the local frontend with:

```bash
cd frontend && pnpm dev --host 127.0.0.1 --port 4173
```

and start the backend with the repo’s normal local development path if it is not already running. Manually confirm:

1. Login as a regular user
2. Open `/user`
3. Footer account area links into the page
4. Provider switching updates the discover command
5. `Create Key` or `Regenerate` returns a secret that stays hidden until `Reveal`
6. Refreshing the page removes reveal/copy availability for that secret

Expected:

```text
Manual browser check complete; session-only secret visibility behaves as designed.
```

- [ ] **Step 4: Run the repo-wide whitespace / status check**

Run:

```bash
git diff --check && git status --short
```

Expected:

```text
# no diff --check output
# only intended tracked files are modified
```

- [ ] **Step 5: Commit any verification-driven fixes**

```bash
git add backend frontend docs
git commit -m "test(user): verify user setup flow"
```

---

## Self-Review

### Spec Coverage

- `/user` route and sidebar entry: Task 4
- profile summary: Task 4
- provider list and selection: Tasks 1, 2, 4
- managed key `missing` / `existing_hidden` / client `session_visible` semantics: Tasks 1, 2, 3, 4
- create/regenerate endpoints: Tasks 1, 2
- install/login/discover command guidance: Tasks 3, 4
- lightweight verify-result review: Tasks 3, 4
- architecture doc update: Task 5
- automated + manual verification: Task 6

### Placeholder Scan

- No `TBD`, `TODO`, or “similar to above” placeholders remain.
- Every code-changing task names exact files and includes explicit commands.

### Type Consistency

- Backend keeps `managed_key.state` server-side as `missing | existing_hidden`.
- Frontend overlay state `session_visible` only exists in `UserView.vue` / `userSetupReview.ts`.
- Managed-key mutation responses consistently return `api_key_id`, `name`, `status`, and `secret`.
