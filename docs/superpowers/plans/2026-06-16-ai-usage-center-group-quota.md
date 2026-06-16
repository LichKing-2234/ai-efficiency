# AI 使用中心 Group Quota Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the homepage `AI 使用中心` usage snapshot with a degradable primary-provider group quota section, then remove confirmed dead standalone usage-page and stale home-copy leftovers.

**Architecture:** Keep the homepage on a single `GET /api/v1/user/usage/dashboard` snapshot. Add a relay-side optional group quota capability plus handler-side merge logic that filters to primary-provider groups with reusable keys, returns `ok | empty | unavailable`, and lets the embedded `UserUsageDashboard` render quota cards above the existing stats and charts without affecting the standalone usage API path.

**Tech Stack:** Go, Gin, Ent, Vue 3, TypeScript, TailwindCSS, Vitest, Vue Test Utils, Markdown docs.

---

**Status:** Implemented with runtime adjustment on 2026-06-16. During execution, upstream inspection showed that sub2api does not expose a user-facing group-quota endpoint, so the shipped implementation derives homepage group cards from existing `ListUserAPIKeys` data plus bound group limits instead of adding a new relay quota endpoint. Verification passed with `cd frontend && pnpm test` and `cd backend && go test ./...`.

**Replay Status:** Historical execution draft. The checkbox tasks below were superseded in-place once the runtime data-source adjustment was confirmed; keep them as execution history rather than as the final source of truth for the implemented path.

## Scope Boundary

Included:

- Extend the AE-side usage snapshot types with `group_quotas`.
- Add an optional relay quota extension for user group quotas without changing the required `relay.Provider` interface.
- Merge primary-provider eligible groups plus quota data inside `backend/internal/handler/user_usage.go`.
- Render the new quota section inside the embedded homepage usage dashboard only.
- Default missing quota units to USD and render infinite quota with `∞`.
- Remove dead `/user/usage` wrapper residue and stale, unreferenced home/i18n keys that are no longer used by the current homepage.
- Update spec/architecture docs to reflect the new homepage snapshot shape.

Excluded:

- Any change to `/user` setup-page behavior or contract.
- Any change to relay direct database access patterns or `sub2api` source code.
- Any multi-currency UI, currency switching, or provider-wide/platform-wide quota aggregation.
- Any new public route beyond the existing `/api/v1/user/usage/dashboard`.

## File Map

Backend contract and tests:

- Modify: `backend/internal/relay/types.go`
  Add group quota request/response types and extend `UserUsageDashboardResponse`.
- Modify: `backend/internal/relay/provider.go`
  Add an optional `UserGroupQuotaProvider` extension interface.
- Modify: `backend/internal/relay/sub2api.go`
  Add sub2api quota adapter logic and helper decode structs.
- Modify: `backend/internal/relay/sub2api_test.go`
  Add relay adapter quota parsing and failure tests.
- Modify: `backend/internal/handler/user_usage.go`
  Merge usage snapshot plus degradable quota section for the homepage.
- Modify: `backend/internal/handler/user_usage_test.go`
  Add handler tests for `ok`, `empty`, and `unavailable` quota states.

Frontend contract, UI, and tests:

- Modify: `frontend/src/types/index.ts`
  Add `UserUsageGroupQuota*` frontend types and extend `UserUsageDashboardSnapshot`.
- Create: `frontend/src/components/user/usage/UsageGroupQuotaSection.vue`
  Render quota cards and unavailable-state message.
- Modify: `frontend/src/components/user/usage/UserUsageDashboard.vue`
  Insert the quota section above stats cards and pass through snapshot data.
- Modify: `frontend/src/i18n.ts`
  Add bilingual quota strings and remove dead unreferenced keys in scope.
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
  Add homepage quota rendering and degradation tests.
- Delete: `frontend/src/views/user/UsageView.vue`
  Remove dead wrapper component no longer used by routing.
- Delete: `frontend/src/__tests__/user-usage-view.test.ts`
  Remove tests that only exercise the deleted wrapper route.
- Modify: `frontend/src/__tests__/router.test.ts`
  Keep explicit regression that `/user/usage` route does not exist.
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`
  Keep explicit regression that sidebar exposes no `/user/usage` link.
- Modify: `frontend/src/__tests__/user-usage-api.test.ts`
  Keep API contract green after snapshot type extension.

Docs:

- Modify: `docs/architecture.md`
  Document that the homepage snapshot now includes degradable group quotas.
- Modify: `docs/superpowers/specs/2026-06-06-user-usage-trend-design.md`
  Add a follow-up note or related-spec link pointing to the new group quota design.

## Task 1: Extend Relay Types and Adapter With Group Quota Support

**Files:**
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api.go`
- Test: `backend/internal/relay/sub2api_test.go`

- [ ] **Step 1: Write failing relay tests for quota response parsing**

Add these tests to `backend/internal/relay/sub2api_test.go` after the existing `TestGetUserUsageDashboard` coverage so the new adapter behavior is specified first:

```go
func TestGetUserGroupQuotasParsesFiniteAndUnlimitedGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"token": "test-jwt-token",
				"user": map[string]any{"id": 7, "email": "alice@example.com"},
			},
		})
	})
	mux.HandleFunc("/api/v1/user/group-quotas", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		if got := r.URL.Query()["group_id"]; diff := cmp.Diff([]string{"42", "43"}, got); diff != "" {
			t.Fatalf("group ids mismatch (-want +got):\n%s", diff)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"unit_label": "USD",
				"groups": []map[string]any{
					{"group_id": "42", "used_amount": 32.4, "quota_amount": 100.0, "is_unlimited": false},
					{"group_id": "43", "used_amount": 18.2, "quota_amount": nil, "is_unlimited": true},
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewSub2APIProvider(server.URL, "admin-key", "test-model", zap.NewNop())
	quotaProvider, ok := p.(interface {
		GetUserGroupQuotas(context.Context, string, string, UserGroupQuotaRequest) (*UserGroupQuotaResponse, error)
	})
	if !ok {
		t.Fatal("provider does not implement GetUserGroupQuotas")
	}

	got, err := quotaProvider.GetUserGroupQuotas(context.Background(), "alice@example.com", "test-password", UserGroupQuotaRequest{
		GroupIDs:  []string{"42", "43"},
		Timezone:  "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUserGroupQuotas() unexpected error: %v", err)
	}
	if got.UnitLabel != "USD" {
		t.Fatalf("unit label = %q, want USD", got.UnitLabel)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(got.Groups))
	}
	if got.Groups[0].GroupID != "42" || got.Groups[0].QuotaAmount == nil || *got.Groups[0].QuotaAmount != 100 {
		t.Fatalf("unexpected first group: %+v", got.Groups[0])
	}
	if !got.Groups[1].IsUnlimited || got.Groups[1].QuotaAmount != nil {
		t.Fatalf("unexpected second group: %+v", got.Groups[1])
	}
}

func TestGetUserGroupQuotasReturnsInvalidCredentialsOnUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"token": "test-jwt-token",
				"user": map[string]any{"id": 7, "email": "alice@example.com"},
			},
		})
	})
	mux.HandleFunc("/api/v1/user/group-quotas", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "unauthorized"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewSub2APIProvider(server.URL, "admin-key", "test-model", zap.NewNop())
	quotaProvider := p.(interface {
		GetUserGroupQuotas(context.Context, string, string, UserGroupQuotaRequest) (*UserGroupQuotaResponse, error)
	})

	_, err := quotaProvider.GetUserGroupQuotas(context.Background(), "alice@example.com", "test-password", UserGroupQuotaRequest{
		GroupIDs: []string{"42"},
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
```

- [ ] **Step 2: Run the focused relay tests to confirm failure**

Run: `cd backend && go test ./internal/relay -run 'TestGetUserGroupQuotas|TestGetUserUsageDashboard'`

Expected: FAIL because `UserGroupQuotaRequest`, `UserGroupQuotaResponse`, and `GetUserGroupQuotas` do not exist yet.

- [ ] **Step 3: Add relay-side group quota types in `backend/internal/relay/types.go`**

Insert these types after `UserUsageModelStat`:

```go
type UserGroupQuotaRequest struct {
	GroupIDs []string `json:"group_ids"`
	Timezone string   `json:"timezone"`
}

type UserGroupQuotaItem struct {
	GroupID     string   `json:"group_id"`
	UsedAmount  *float64 `json:"used_amount,omitempty"`
	QuotaAmount *float64 `json:"quota_amount,omitempty"`
	IsUnlimited bool     `json:"is_unlimited"`
}

type UserGroupQuotaResponse struct {
	UnitLabel string               `json:"unit_label"`
	Groups    []UserGroupQuotaItem `json:"groups"`
}

type UserUsageGroupQuotaState struct {
	Status    string                          `json:"status"`
	UnitLabel string                          `json:"unit_label,omitempty"`
	Message   string                          `json:"message,omitempty"`
	Groups    []UserUsageGroupQuotaGroupItem  `json:"groups"`
}

type UserUsageGroupQuotaGroupItem struct {
	GroupID     string   `json:"group_id"`
	GroupName   string   `json:"group_name"`
	Platform    string   `json:"platform"`
	UsedAmount  *float64 `json:"used_amount,omitempty"`
	QuotaAmount *float64 `json:"quota_amount,omitempty"`
	IsUnlimited bool     `json:"is_unlimited"`
}
```

Then extend `UserUsageDashboardResponse`:

```go
type UserUsageDashboardResponse struct {
	Configured  bool                     `json:"configured"`
	Range       UserUsageDashboardRange  `json:"range"`
	Stats       *UserUsageDashboardStats `json:"stats"`
	Trend       []UserUsageTrendPoint    `json:"trend"`
	Models      []UserUsageModelStat     `json:"models"`
	GroupQuotas UserUsageGroupQuotaState `json:"group_quotas"`
}
```

- [ ] **Step 4: Add the optional quota extension interface in `backend/internal/relay/provider.go`**

Append this interface below `PlatformModelLister`:

```go
type UserGroupQuotaProvider interface {
	GetUserGroupQuotas(ctx context.Context, login, password string, req UserGroupQuotaRequest) (*UserGroupQuotaResponse, error)
}
```

- [ ] **Step 5: Implement the sub2api quota adapter in `backend/internal/relay/sub2api.go`**

Add a decode envelope and adapter method near the existing usage dashboard helpers:

```go
type userGroupQuotaEnvelope struct {
	UnitLabel string               `json:"unit_label"`
	Groups    []UserGroupQuotaItem `json:"groups"`
}

func (s *sub2apiRelay) GetUserGroupQuotas(ctx context.Context, login, password string, req UserGroupQuotaRequest) (*UserGroupQuotaResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for user group quotas: %w", err)
	}

	query := url.Values{}
	for _, groupID := range req.GroupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		query.Add("group_id", groupID)
	}
	if tz := strings.TrimSpace(req.Timezone); tz != "" {
		query.Set("timezone", tz)
	}

	var envelope userGroupQuotaEnvelope
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/user/group-quotas", query, &envelope); err != nil {
		return nil, fmt.Errorf("relay: user group quotas: %w", err)
	}
	return &UserGroupQuotaResponse{
		UnitLabel: strings.TrimSpace(envelope.UnitLabel),
		Groups:    envelope.Groups,
	}, nil
}
```

- [ ] **Step 6: Run the relay tests and make them pass**

Run: `cd backend && go test ./internal/relay -run 'TestGetUserGroupQuotas|TestGetUserUsageDashboard'`

Expected: PASS

- [ ] **Step 7: Commit the relay contract work**

Run:

```bash
git add backend/internal/relay/types.go backend/internal/relay/provider.go backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go
git commit -m "feat(relay): add user group quota adapter"
```

## Task 2: Merge Group Quotas Into the Homepage Usage Snapshot

**Files:**
- Modify: `backend/internal/handler/user_usage.go`
- Modify: `backend/internal/handler/user_usage_test.go`
- Modify: `backend/internal/usersetup/service.go`

- [ ] **Step 1: Write failing handler tests for `ok`, `empty`, and `unavailable` quota states**

Extend `backend/internal/handler/user_usage_test.go` with a richer stub and these tests:

```go
type userUsageQuotaStub struct {
	userUsageRelayStub
	quotaResponse *relay.UserGroupQuotaResponse
	quotaErr      error
	gotQuotaReq   relay.UserGroupQuotaRequest
}

func (s *userUsageQuotaStub) GetUserGroupQuotas(ctx context.Context, login, password string, req relay.UserGroupQuotaRequest) (*relay.UserGroupQuotaResponse, error) {
	s.gotLogin = login
	s.gotPassword = password
	s.gotQuotaReq = req
	if s.quotaErr != nil {
		return nil, s.quotaErr
	}
	return s.quotaResponse, nil
}

func TestUserUsageDashboardIncludesEligibleGroupQuotas(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	provider, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageQuotaStub{
		userUsageRelayStub: userUsageRelayStub{
			response: &relay.UserUsageDashboardResponse{
				Configured: true,
				Range: relay.UserUsageDashboardRange{StartDate: "2026-06-01", EndDate: "2026-06-06", Granularity: "day", Timezone: "Asia/Shanghai"},
				Trend: []relay.UserUsageTrendPoint{},
				Models: []relay.UserUsageModelStat{},
			},
		},
		quotaResponse: &relay.UserGroupQuotaResponse{
			UnitLabel: "USD",
			Groups: []relay.UserGroupQuotaItem{
				{GroupID: "42", UsedAmount: float64Ptr(32.4), QuotaAmount: float64Ptr(100), IsUnlimited: false},
				{GroupID: "44", UsedAmount: float64Ptr(10), QuotaAmount: nil, IsUnlimited: true},
			},
		},
	}

	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	// Add a reusable-key eligible group by stubbing the provider methods the
	// handler's usersetup service already relies on:
	stub.Provider = &fakeQuotaListProvider{
		apiKeys: []relay.APIKey{
			{
				ID:     44,
				UserID: 77,
				Key:    "sk-test-42",
				Name:   "admin",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			{
				ID:     45,
				UserID: 77,
				Key:    "sk-test-missing",
				Name:   "admin",
				Status: "inactive",
				Group:  &relay.Group{ID: 43, Name: "Group Hidden", Platform: "anthropic"},
			},
		},
		allowedGroups: []relay.Group{
			{ID: 42, Name: "Group Alpha", Platform: "openai"},
			{ID: 43, Name: "Group Hidden", Platform: "anthropic"},
		},
	}

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_quotas":{"status":"ok"`) {
		t.Fatalf("missing ok group quotas: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_name":"Group Alpha"`) {
		t.Fatalf("missing eligible group: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"group_name":"Group Hidden"`) {
		t.Fatalf("unexpected missing-credential group in response: %s", w.Body.String())
	}
	if diff := cmp.Diff([]string{"42"}, stub.gotQuotaReq.GroupIDs); diff != "" {
		t.Fatalf("quota request group ids mismatch (-want +got):\n%s", diff)
	}
}
```

Add a local helper stub in the same test file to satisfy `ListUserAPIKeys` and `ListAllowedGroupsForUser`:

```go
type fakeQuotaListProvider struct {
	relay.Provider
	apiKeys       []relay.APIKey
	allowedGroups []relay.Group
}

func (f *fakeQuotaListProvider) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	return f.apiKeys, nil
}

func (f *fakeQuotaListProvider) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error) {
	return f.allowedGroups, nil
}
```

Also add two more tests:

- `TestUserUsageDashboardReturnsEmptyGroupQuotasWhenNoReusableKeysExist`
- `TestUserUsageDashboardReturnsUnavailableGroupQuotasWhenProviderDoesNotSupportQuota`

Required assertions across the three tests:

- response contains `"group_quotas":{"status":"ok"...}`
- response contains `"group_name":"Group Alpha"`
- response does not contain any group with missing credential state
- missing quota provider support yields `"status":"unavailable"`
- no eligible groups yields `"status":"empty"`
- when unit is empty, response contains `"unit_label":"USD"`

- [ ] **Step 2: Run the focused handler tests to verify failure**

Run: `cd backend && go test ./internal/handler -run 'TestUserUsageDashboard'`

Expected: FAIL because `group_quotas` is not merged into the response and the handler has no quota-awareness.

- [ ] **Step 3: Add an internal helper in `backend/internal/usersetup/service.go` to filter primary-provider groups with reusable keys**

Add this method near `ListProviders` so handler code can reuse provider summary rules without calling the HTTP handler:

```go
func (s *Service) ListPrimaryProviderGroups(ctx context.Context, req ListProvidersRequest) (*ProviderSummary, error) {
	resp, err := s.ListProviders(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, provider := range resp.Providers {
		if provider.IsPrimary {
			copyProvider := provider
			return &copyProvider, nil
		}
	}
	return nil, nil
}
```

- [ ] **Step 4: Extend `backend/internal/handler/user_usage.go` with quota merge helpers**

Add a `usersetup.Service` dependency or helper construction path and implement:

```go
func defaultUnavailableGroupQuotaState(message string) relay.UserUsageGroupQuotaState {
	return relay.UserUsageGroupQuotaState{
		Status:  "unavailable",
		Message: strings.TrimSpace(message),
		Groups:  []relay.UserUsageGroupQuotaGroupItem{},
	}
}

func defaultEmptyGroupQuotaState() relay.UserUsageGroupQuotaState {
	return relay.UserUsageGroupQuotaState{
		Status: "empty",
		Groups: []relay.UserUsageGroupQuotaGroupItem{},
	}
}

func defaultConfiguredFalseGroupQuotaState() relay.UserUsageGroupQuotaState {
	return relay.UserUsageGroupQuotaState{
		Status: "empty",
		Groups: []relay.UserUsageGroupQuotaGroupItem{},
	}
}
```

Then update `Dashboard` so:

- the `configured:false` branch returns `GroupQuotas: defaultConfiguredFalseGroupQuotaState()`
- after `GetUserUsageDashboard`, it loads the primary provider summary through `usersetup.NewService(...)`
- filters to `group.Credential.State == "existing_hidden"`
- if none exist, sets `snapshot.GroupQuotas = defaultEmptyGroupQuotaState()`
- if provider does not satisfy `relay.UserGroupQuotaProvider`, sets `snapshot.GroupQuotas = defaultUnavailableGroupQuotaState("Group quotas are temporarily unavailable.")`
- if quota lookup succeeds, merges by `group_id` and defaults blank unit to `USD`
- if quota lookup fails, keeps the main snapshot and sets unavailable state instead of returning 5xx

- [ ] **Step 5: Add a focused merge helper in `backend/internal/handler/user_usage.go`**

Implement a pure helper so tests can stay readable:

```go
func mergeHomepageGroupQuotas(provider *usersetup.ProviderSummary, quota *relay.UserGroupQuotaResponse) relay.UserUsageGroupQuotaState {
	if provider == nil {
		return defaultEmptyGroupQuotaState()
	}
	eligible := make([]usersetup.GroupCredentialSummary, 0, len(provider.Groups))
	for _, group := range provider.Groups {
		if strings.TrimSpace(group.Credential.State) == "existing_hidden" {
			eligible = append(eligible, group)
		}
	}
	if len(eligible) == 0 {
		return defaultEmptyGroupQuotaState()
	}

	byID := make(map[string]relay.UserGroupQuotaItem, len(quota.Groups))
	for _, item := range quota.Groups {
		byID[strings.TrimSpace(item.GroupID)] = item
	}

	out := relay.UserUsageGroupQuotaState{
		Status:    "ok",
		UnitLabel: firstNonEmptyString(strings.TrimSpace(quota.UnitLabel), "USD"),
		Groups:    make([]relay.UserUsageGroupQuotaGroupItem, 0, len(eligible)),
	}
	for _, group := range eligible {
		item := byID[strings.TrimSpace(group.GroupID)]
		out.Groups = append(out.Groups, relay.UserUsageGroupQuotaGroupItem{
			GroupID:     group.GroupID,
			GroupName:   group.GroupName,
			Platform:    group.Platform,
			UsedAmount:  item.UsedAmount,
			QuotaAmount: item.QuotaAmount,
			IsUnlimited: item.IsUnlimited || item.QuotaAmount == nil,
		})
	}
	return out
}
```

- [ ] **Step 6: Run handler tests until green**

Run: `cd backend && go test ./internal/handler -run 'TestUserUsageDashboard'`

Expected: PASS

- [ ] **Step 7: Run the broader backend safety net**

Run: `cd backend && go test ./internal/handler ./internal/usersetup ./internal/relay`

Expected: PASS

- [ ] **Step 8: Commit the handler merge work**

Run:

```bash
git add backend/internal/handler/user_usage.go backend/internal/handler/user_usage_test.go backend/internal/usersetup/service.go
git commit -m "feat(backend): merge homepage group quotas into usage snapshot"
```

## Task 3: Render Group Quotas in the Homepage Usage Dashboard

**Files:**
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/components/user/usage/UsageGroupQuotaSection.vue`
- Modify: `frontend/src/components/user/usage/UserUsageDashboard.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
- Modify: `frontend/src/__tests__/user-usage-api.test.ts`

- [ ] **Step 1: Write failing homepage UI tests for quota states**

Add these tests to `frontend/src/__tests__/dashboard-view.test.ts` using the existing `usageSnapshot` fixture extended with `group_quotas`:

```ts
const usageSnapshotWithQuotas = {
  ...usageSnapshot,
  group_quotas: {
    status: 'ok',
    unit_label: 'USD',
    message: '',
    groups: [
      { group_id: '42', group_name: 'Group Alpha', platform: 'openai', used_amount: 32.4, quota_amount: 100, is_unlimited: false },
      { group_id: '43', group_name: 'Group Beta', platform: 'anthropic', used_amount: 18.2, quota_amount: null, is_unlimited: true },
    ],
  },
}

it('renders homepage group quota cards above the usage stats', async () => {
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getUserProviders as any).mockResolvedValue({
    data: { data: { providers: [{ id: 1, name: 'prod', display_name: 'Production', base_url: 'https://relay.example.com', default_model: 'gpt-5.4', is_primary: true, groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }] }] } },
  })
  ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshotWithQuotas } })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('Group Quotas')
  expect(wrapper.text()).toContain('Group Alpha')
  expect(wrapper.text()).toContain('$32.40 / $100.00')
  expect(wrapper.text()).toContain('$18.20 / ∞')
  expect(wrapper.text()).toContain('My Usage')
})

it('hides the quota section when the snapshot reports empty quotas', async () => {
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
  ;(getUserUsageDashboard as any).mockResolvedValue({
    data: {
      data: {
        ...usageSnapshot,
        group_quotas: { status: 'empty', unit_label: '', message: '', groups: [] },
      },
    },
  })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).not.toContain('Group Quotas')
})

it('shows a lightweight unavailable message when quota loading degrades', async () => {
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
  ;(getUserUsageDashboard as any).mockResolvedValue({
    data: {
      data: {
        ...usageSnapshot,
        group_quotas: { status: 'unavailable', unit_label: 'USD', message: 'Group quotas are temporarily unavailable.', groups: [] },
      },
    },
  })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('Group Quotas')
  expect(wrapper.text()).toContain('temporarily unavailable')
  expect(wrapper.text()).toContain('Token Trend')
  expect(wrapper.text()).toContain('Model Distribution')
})
```

- [ ] **Step 2: Run the focused dashboard tests to confirm failure**

Run: `cd frontend && pnpm test -- src/__tests__/dashboard-view.test.ts src/__tests__/user-usage-api.test.ts`

Expected: FAIL because the frontend snapshot types and components do not know `group_quotas`.

- [ ] **Step 3: Extend the frontend usage snapshot types**

In `frontend/src/types/index.ts`, add:

```ts
export interface UserUsageGroupQuotaItem {
  group_id: string
  group_name: string
  platform: string
  used_amount?: number | null
  quota_amount?: number | null
  is_unlimited: boolean
}

export interface UserUsageGroupQuotaState {
  status: 'ok' | 'empty' | 'unavailable' | string
  unit_label?: string
  message?: string
  groups: UserUsageGroupQuotaItem[]
}
```

Then extend `UserUsageDashboardSnapshot`:

```ts
export interface UserUsageDashboardSnapshot {
  configured: boolean
  range: UserUsageDashboardRange
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  models: UserUsageModelStat[]
  group_quotas?: UserUsageGroupQuotaState
}
```

- [ ] **Step 4: Create `frontend/src/components/user/usage/UsageGroupQuotaSection.vue`**

Use this implementation as the initial minimal component:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { UserUsageGroupQuotaState } from '@/types'

const props = defineProps<{
  quotas?: UserUsageGroupQuotaState | null
}>()

const { t } = useI18n()

const shouldHide = computed(() => !props.quotas || props.quotas.status === 'empty' || props.quotas.groups.length === 0 && props.quotas.status !== 'unavailable')

function formatCurrency(amount: number | null | undefined, unitLabel?: string) {
  if (amount == null) return '--'
  if ((unitLabel ?? '').toUpperCase() === 'USD' || !unitLabel) return `$${amount.toFixed(2)}`
  return `${unitLabel} ${amount.toFixed(2)}`
}
</script>

<template>
  <section v-if="!shouldHide" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm" data-testid="usage-group-quotas">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.groupQuotasTitle') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.groupQuotasHelp') }}</p>
      </div>
    </div>

    <div v-if="props.quotas?.status === 'unavailable'" class="mt-4 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      {{ props.quotas?.message || t('usageDashboard.groupQuotasUnavailable') }}
    </div>

    <div v-else class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3">
      <article v-for="group in props.quotas?.groups ?? []" :key="group.group_id" class="rounded-lg border border-gray-200 bg-gray-50 p-4">
        <p class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ group.platform }}</p>
        <h3 class="mt-2 text-lg font-semibold text-gray-900">{{ group.group_name }}</h3>
        <p class="mt-2 text-xs font-medium uppercase text-gray-500">
          {{ group.is_unlimited ? t('usageDashboard.usedOverUnlimited') : t('usageDashboard.usedOverQuota') }}
        </p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">
          {{ formatCurrency(group.used_amount, props.quotas?.unit_label) }} / {{ group.is_unlimited ? '∞' : formatCurrency(group.quota_amount, props.quotas?.unit_label) }}
        </p>
      </article>
    </div>
  </section>
</template>
```

- [ ] **Step 5: Insert the quota section into `frontend/src/components/user/usage/UserUsageDashboard.vue`**

Add the import:

```ts
import UsageGroupQuotaSection from '@/components/user/usage/UsageGroupQuotaSection.vue'
```

Then insert the component before `UsageStatsCards`:

```vue
<div v-else class="space-y-6">
  <UsageGroupQuotaSection :quotas="currentSnapshot?.group_quotas ?? null" />
  <UsageStatsCards
    :stats="currentSnapshot?.stats ?? null"
    :trend="currentSnapshot?.trend ?? []"
    :range-label="snapshotRangeLabel"
    :hide-cost="props.homeMode"
  />
  <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
    <UsageTrendChart :data="currentSnapshot?.trend ?? []" :loading="loading" />
    <UsageModelChart :data="currentSnapshot?.models ?? []" :loading="loading" :hide-cost="props.homeMode" />
  </div>
</div>
```

- [ ] **Step 6: Add the new bilingual strings in `frontend/src/i18n.ts`**

Add these keys to both locales:

```ts
'usageDashboard.groupQuotasTitle': 'Group Quotas',
'usageDashboard.groupQuotasHelp': 'Primary-provider groups with reusable API keys.',
'usageDashboard.groupQuotasUnavailable': 'Group quotas are temporarily unavailable.',
'usageDashboard.usedOverQuota': 'Used / Quota',
'usageDashboard.usedOverUnlimited': 'Used / ∞',
```

Chinese:

```ts
'usageDashboard.groupQuotasTitle': 'Group 配额',
'usageDashboard.groupQuotasHelp': '仅展示主 provider 下已有可复用 API key 的接入组。',
'usageDashboard.groupQuotasUnavailable': 'Group 配额暂时不可用。',
'usageDashboard.usedOverQuota': '已使用 / 配额',
'usageDashboard.usedOverUnlimited': '已使用 / ∞',
```

- [ ] **Step 7: Keep the API contract test green**

Update `frontend/src/__tests__/user-usage-api.test.ts` only if the typed fixture needs `group_quotas`; the request assertion must remain:

```ts
expect(mockClient.get).toHaveBeenCalledWith('/user/usage/dashboard', {
  params: {
    start_date: '2026-06-01',
    end_date: '2026-06-06',
    granularity: 'day',
    timezone: 'Asia/Shanghai',
  },
})
```

- [ ] **Step 8: Run the focused frontend tests and make them pass**

Run: `cd frontend && pnpm test -- src/__tests__/dashboard-view.test.ts src/__tests__/user-usage-api.test.ts`

Expected: PASS

- [ ] **Step 9: Commit the homepage quota UI**

Run:

```bash
git add frontend/src/types/index.ts frontend/src/components/user/usage/UsageGroupQuotaSection.vue frontend/src/components/user/usage/UserUsageDashboard.vue frontend/src/i18n.ts frontend/src/__tests__/dashboard-view.test.ts frontend/src/__tests__/user-usage-api.test.ts
git commit -m "feat(frontend): add homepage group quota section"
```

## Task 4: Remove Dead Standalone Usage Wrapper Residue and Update Docs

**Files:**
- Delete: `frontend/src/views/user/UsageView.vue`
- Delete: `frontend/src/__tests__/user-usage-view.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`
- Modify: `frontend/src/i18n.ts`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-06-user-usage-trend-design.md`

- [ ] **Step 1: Confirm the wrapper route is dead before deletion**

Run:

```bash
rg -n "UsageView|/user/usage|nav\\.myUsageTrend" frontend/src frontend/src/__tests__
```

Expected:

- `/user/usage` only appears in tests that explicitly assert it is absent or in the wrapper-specific test file slated for deletion.
- `nav.myUsageTrend` appears only in `frontend/src/i18n.ts`.

- [ ] **Step 2: Delete the dead wrapper component and wrapper-only tests**

Run:

```bash
rm frontend/src/views/user/UsageView.vue
rm frontend/src/__tests__/user-usage-view.test.ts
```

Expected: both files are removed from git and no runtime route references remain.

- [ ] **Step 3: Prune dead i18n keys from `frontend/src/i18n.ts`**

Delete the unused keys confirmed by `rg`, including:

```ts
'nav.myUsageTrend'
'home.personalStatus'
'home.thisWeek'
'home.recentActivity'
'home.metricRepos'
'home.metricReposHelp'
'home.metricWorkflows'
'home.metricWorkflowsHelp'
'home.metricAiPrs'
'home.metricAiPrsHelp'
'home.metricTools'
'home.metricToolsHelp'
'home.metricToolsHelpNone'
'home.metricToolsHelpUnavailable'
'home.recentLoaded'
'home.statusAccount'
'home.statusCli'
'home.statusData'
'home.statusAccountReady'
'home.statusCliGuide'
'home.statusDataSeen'
'home.statusDataMissing'
'home.statusAiAccess'
'home.statusAiAccessReady'
'home.statusAiAccessMissing'
'home.statusReporting'
'home.statusReportingActive'
'home.statusReportingWaiting'
'home.statusRecentUsage'
'home.statusRecentUsageSeen'
'home.statusRecentUsageMissing'
'home.unknownRepository'
'home.eventTokens'
'home.eventRequests'
'home.nextSetupTitle'
'home.nextSetupText'
'home.nextRepoTitle'
'home.nextRepoText'
'home.nextRecordsTitle'
'home.nextRecordsText'
```

Only remove keys proven unused in the current codebase. Keep `home.subtitle`, `home.loading`, guide-card keys, and any remaining used `usageDashboard.*` strings.

- [ ] **Step 4: Re-run routing/sidebar regressions**

Run: `cd frontend && pnpm test -- src/__tests__/router.test.ts src/__tests__/app-sidebar.test.ts`

Expected: PASS, with `/user/usage` still absent and no sidebar link reintroduced.

- [ ] **Step 5: Update `docs/architecture.md`**

Add or edit the homepage description so it states:

```md
- `/` serves the lifecycle-aware AI Usage Center homepage.
- The homepage calls the AE-side usage snapshot endpoint once and renders:
  - guide-card lifecycle state
  - usage stats/trend/model data
  - a degradable primary-provider group quota section
- `/user` remains the AI setup and configuration surface; it does not own homepage quota rendering.
```

- [ ] **Step 6: Add a follow-up note to `docs/superpowers/specs/2026-06-06-user-usage-trend-design.md`**

Append a short note near the top or in a follow-up section:

```md
## Follow-up

Group-level homepage quota display is now defined by [2026-06-16-ai-usage-center-group-quota-design.md](./2026-06-16-ai-usage-center-group-quota-design.md). The original first-version non-goal around `platform quota` remains historical context for the initial dashboard scope.
```

- [ ] **Step 7: Run the full frontend and backend verification commands**

Run:

```bash
cd frontend && pnpm test
cd ../backend && go test ./...
```

Expected: PASS

If either suite has pre-existing failures unrelated to this work, record the exact failing test names in the final summary and do not check this step off until the result is understood.

- [ ] **Step 8: Commit the cleanup and docs updates**

Run:

```bash
git add frontend/src/i18n.ts frontend/src/__tests__/router.test.ts frontend/src/__tests__/app-sidebar.test.ts docs/architecture.md docs/superpowers/specs/2026-06-06-user-usage-trend-design.md
git add -u frontend/src/views/user/UsageView.vue frontend/src/__tests__/user-usage-view.test.ts
git commit -m "chore(docs): align homepage quota cleanup"
```

## Self-Review

Spec coverage checklist:

- Group quota lives in homepage snapshot: covered by Tasks 1-2.
- Primary-provider + reusable-key filtering: covered by Task 2.
- `ok | empty | unavailable` status semantics: covered by Tasks 2-3.
- Finite / infinite / missing-used rendering rules: covered by Task 3.
- USD fallback and `∞` display: covered by Tasks 2-3.
- Dead wrapper cleanup and stale i18n removal: covered by Task 4.
- Architecture/spec update requirements: covered by Task 4.

Placeholder scan:

- No `TODO`, `TBD`, or deferred implementation placeholders remain in task steps.
- Commands are explicit and tied to expected outputs.

Type consistency:

- Backend names use `UserGroupQuotaRequest`, `UserGroupQuotaResponse`, `UserUsageGroupQuotaState`, and `UserUsageGroupQuotaGroupItem`.
- Frontend names use `UserUsageGroupQuotaState` and `UserUsageGroupQuotaItem`.
- Snapshot property name stays `group_quotas` across backend JSON and frontend types.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-16-ai-usage-center-group-quota.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
