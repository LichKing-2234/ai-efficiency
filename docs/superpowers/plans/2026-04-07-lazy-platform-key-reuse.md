# Lazy Platform Key Reuse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop creating a new sub2api key on every `ae-cli start` by lazily resolving per-platform keys (`openai`, `anthropic`) on first tool use, reusing active keys by name and platform when possible and creating only as a fallback.

**Architecture:** Split bootstrap from credential issuance. Backend session bootstrap will create session metadata only; a new protected session-scoped credential endpoint will resolve provider credentials lazily per platform using reuse-first rules. The local proxy will fetch and cache credentials per platform on first OpenAI/Anthropic request, so `Codex` and `Claude` can use different sub2api keys within the same session without leaking upstream credentials into tool-facing env.

**Tech Stack:** Go (`gin`, `ent`, existing relay/sessionbootstrap services, ae-cli local proxy), existing backend auth middleware, existing ae-cli runtime/proxy/client tests.

**Status:** ✅ 已完成（2026-04-12）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前代码和最新 spec 重写执行计划。

**Source Of Truth:** 已实现行为以当前代码、`docs/architecture.md` 和相关最新 spec 为准。本文保留实施切片与验收轨迹。

> **Updated:** 2026-04-12 — 基于代码审查回填状态与 checkbox。

---

## File Structure

### New files

- `backend/internal/sessionbootstrap/key_selection.go`
  Pure helper logic for selecting an existing key by platform + name fallback rules.
- `backend/internal/sessionbootstrap/key_selection_test.go`
  Unit tests for the key selection rules.
- `ae-cli/internal/proxy/credentials.go`
  Lazy backend credential fetch + per-platform in-memory cache for the local proxy.
- `ae-cli/internal/proxy/credentials_test.go`
  Unit tests for lazy fetch, cache hits, and backend error behavior.

### Modified files

- `backend/internal/relay/types.go`
  Expand relay API key metadata to include the fields needed for reuse selection (`Key`, `LastUsedAt`, `CreatedAt`, `Group`, `Group.Platform`).
- `backend/internal/relay/sub2api.go`
  Decode enriched admin user API key responses from sub2api.
- `backend/internal/relay/sub2api_test.go`
  Verify `ListUserAPIKeys` parses group/platform/last-used metadata.
- `backend/internal/sessionbootstrap/service.go`
  Remove bootstrap-time key creation and add a new session-scoped credential resolver method that reuses or creates per-platform keys.
- `backend/internal/sessionbootstrap/service_test.go`
  Cover no-longer-create-on-bootstrap, reuse selection, username/email-prefix fallback, and create-on-miss.
- `backend/internal/handler/session.go`
  Add a new protected route for `GET /api/v1/sessions/:id/provider-credentials?platform=...`.
- `backend/internal/handler/router.go`
  Register the new session credential route.
- `backend/internal/handler/session_bootstrap_http_test.go`
  Update bootstrap HTTP expectations and add provider-credential HTTP tests.
- `ae-cli/internal/client/client.go`
  Add a client method for the new session credential endpoint and update bootstrap response expectations if fields change.
- `ae-cli/internal/client/client_test.go`
  Verify the new client request/response behavior.
- `ae-cli/internal/proxy/config.go`
  Remove single global upstream credential assumptions from runtime config if no longer needed.
- `ae-cli/internal/proxy/openai.go`
  Resolve and cache the `openai` platform credential on demand before forwarding.
- `ae-cli/internal/proxy/anthropic.go`
  Resolve and cache the `anthropic` platform credential on demand before forwarding.
- `ae-cli/internal/proxy/server_test.go`
  Update proxy tests to use backend credential resolution instead of static provider key/url.
- `ae-cli/internal/session/manager.go`
  Stop deriving proxy upstream credentials from bootstrap env bundle; start proxy with backend access only.
- `ae-cli/internal/session/session_test.go`
  Update runtime/env expectations so bootstrap no longer injects upstream provider secrets.

### Existing files to read before implementation

- `backend/internal/sessionbootstrap/service.go`
- `backend/internal/sessionbootstrap/service_test.go`
- `backend/internal/relay/types.go`
- `backend/internal/relay/sub2api.go`
- `backend/internal/relay/sub2api_test.go`
- `backend/internal/handler/session.go`
- `backend/internal/handler/router.go`
- `backend/internal/handler/session_bootstrap_http_test.go`
- `ae-cli/internal/client/client.go`
- `ae-cli/internal/client/client_test.go`
- `ae-cli/internal/proxy/openai.go`
- `ae-cli/internal/proxy/anthropic.go`
- `ae-cli/internal/proxy/server_test.go`
- `ae-cli/internal/session/manager.go`
- `ae-cli/internal/session/session_test.go`

### Key selection rules locked in by this plan

For a requested platform (`openai` for Codex, `anthropic` for Claude):

1. Only consider keys with `status == active`.
2. Only consider keys whose `group.platform` matches the requested platform.
3. Prefer `name == username`.
4. If none, prefer `name == email_prefix` where `email_prefix` is the substring before `@`.
5. If multiple remain in the same priority tier, choose the one with the most recent `last_used_at`.
6. If `last_used_at` is nil for all candidates, choose the most recent `created_at`.
7. If no match exists, create a new key using the preferred name:
   - `username` if non-empty
   - otherwise `email_prefix`

The selected key secret must never be returned in `bootstrap env_bundle`.

---

### Task 1: Expand Relay Key Metadata And Add Pure Selection Logic

**Files:**
- Create: `backend/internal/sessionbootstrap/key_selection.go`
- Create: `backend/internal/sessionbootstrap/key_selection_test.go`
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Test: `backend/internal/sessionbootstrap/key_selection_test.go`
- Test: `backend/internal/relay/sub2api_test.go`

- [x] **Step 1: Write the failing key selection tests**

Create `backend/internal/sessionbootstrap/key_selection_test.go`:

```go
package sessionbootstrap

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestSelectReusableKeyPrefersUsernameThenLastUsed(t *testing.T) {
	now := time.Now()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-30 * time.Minute)

	keys := []relay.APIKey{
		{
			ID:     1,
			Name:   "alice",
			Status: "active",
			Group:  &relay.Group{Platform: "openai"},
			LastUsedAt: &older,
		},
		{
			ID:     2,
			Name:   "alice",
			Status: "active",
			Group:  &relay.Group{Platform: "openai"},
			LastUsedAt: &newer,
		},
		{
			ID:     3,
			Name:   "alice",
			Status: "active",
			Group:  &relay.Group{Platform: "anthropic"},
			LastUsedAt: &now,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got == nil || got.ID != 2 {
		t.Fatalf("selected key = %+v, want id=2", got)
	}
}

func TestSelectReusableKeyFallsBackToEmailPrefix(t *testing.T) {
	now := time.Now()
	keys := []relay.APIKey{
		{
			ID:     10,
			Name:   "alice",
			Status: "disabled",
			Group:  &relay.Group{Platform: "openai"},
			LastUsedAt: &now,
		},
		{
			ID:     11,
			Name:   "a.smith",
			Status: "active",
			Group:  &relay.Group{Platform: "openai"},
			LastUsedAt: &now,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "a.smith")
	if got == nil || got.ID != 11 {
		t.Fatalf("selected key = %+v, want id=11", got)
	}
}

func TestSelectReusableKeyReturnsNilWhenPlatformDoesNotMatch(t *testing.T) {
	keys := []relay.APIKey{
		{
			ID:     20,
			Name:   "alice",
			Status: "active",
			Group:  &relay.Group{Platform: "anthropic"},
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got != nil {
		t.Fatalf("selected key = %+v, want nil", got)
	}
}
```

Add a parsing test in `backend/internal/relay/sub2api_test.go`:

```go
func TestListUserAPIKeysDecodesGroupPlatformAndLastUsed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{
					"id":          1,
					"user_id":     7,
					"key":         "sk-existing-openai",
					"name":        "alice",
					"status":      "active",
					"created_at":  "2026-04-07T10:00:00Z",
					"last_used_at":"2026-04-07T11:00:00Z",
					"group": map[string]any{
						"id":       42,
						"platform": "openai",
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	keys, err := p.ListUserAPIKeys(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListUserAPIKeys() unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].Key != "sk-existing-openai" {
		t.Fatalf("key secret = %q, want %q", keys[0].Key, "sk-existing-openai")
	}
	if keys[0].Group == nil || keys[0].Group.Platform != "openai" {
		t.Fatalf("group platform = %+v, want openai", keys[0].Group)
	}
	if keys[0].LastUsedAt == nil || keys[0].LastUsedAt.IsZero() {
		t.Fatalf("expected last_used_at to be decoded")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./internal/sessionbootstrap -run 'TestSelectReusableKey' -count=1
go test ./internal/relay -run 'TestListUserAPIKeysDecodesGroupPlatformAndLastUsed$' -count=1
```

Expected:

```text
FAIL because selectReusableKey does not exist
FAIL because relay.APIKey lacks Key/Group/LastUsedAt fields
```

- [x] **Step 3: Implement the minimal relay metadata and selector**

Modify `backend/internal/relay/types.go`:

```go
type Group struct {
	ID       int64  `json:"id"`
	Platform string `json:"platform"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Group      *Group     `json:"group,omitempty"`
}
```

Create `backend/internal/sessionbootstrap/key_selection.go`:

```go
package sessionbootstrap

import (
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func selectReusableKey(keys []relay.APIKey, platform, username, emailPrefix string) *relay.APIKey {
	type scored struct {
		key      relay.APIKey
		priority int
	}

	var candidates []scored
	for _, key := range keys {
		if strings.TrimSpace(key.Status) != "active" {
			continue
		}
		if key.Group == nil || strings.TrimSpace(key.Group.Platform) != strings.TrimSpace(platform) {
			continue
		}

		name := strings.TrimSpace(key.Name)
		switch {
		case name != "" && name == strings.TrimSpace(username):
			candidates = append(candidates, scored{key: key, priority: 0})
		case name != "" && name == strings.TrimSpace(emailPrefix):
			candidates = append(candidates, scored{key: key, priority: 1})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		leftUsed := time.Time{}
		rightUsed := time.Time{}
		if candidates[i].key.LastUsedAt != nil {
			leftUsed = candidates[i].key.LastUsedAt.UTC()
		}
		if candidates[j].key.LastUsedAt != nil {
			rightUsed = candidates[j].key.LastUsedAt.UTC()
		}
		if !leftUsed.Equal(rightUsed) {
			return leftUsed.After(rightUsed)
		}
		return candidates[i].key.CreatedAt.After(candidates[j].key.CreatedAt)
	})

	selected := candidates[0].key
	return &selected
}

func preferredKeyName(username, email string) string {
	username = strings.TrimSpace(username)
	if username != "" {
		return username
	}
	email = strings.TrimSpace(email)
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	return email
}
```

Update `backend/internal/relay/sub2api.go` decode structs in `ListUserAPIKeys()` to match the enriched fields.

- [x] **Step 4: Run the tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./internal/sessionbootstrap -run 'TestSelectReusableKey' -count=1
go test ./internal/relay -run 'TestListUserAPIKeysDecodesGroupPlatformAndLastUsed$' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/sessionbootstrap	...
ok  	github.com/ai-efficiency/backend/internal/relay	...
```

- [x] **Step 5: Commit**

```bash
git add backend/internal/sessionbootstrap/key_selection.go \
        backend/internal/sessionbootstrap/key_selection_test.go \
        backend/internal/relay/types.go \
        backend/internal/relay/sub2api.go \
        backend/internal/relay/sub2api_test.go
git commit -m "feat(backend): add platform-aware relay key selection"
```

### Task 2: Backend Session Credential Endpoint And Reuse-First Resolution

**Files:**
- Modify: `backend/internal/sessionbootstrap/service.go`
- Modify: `backend/internal/sessionbootstrap/service_test.go`
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/session_bootstrap_http_test.go`
- Test: `backend/internal/sessionbootstrap/service_test.go`
- Test: `backend/internal/handler/session_bootstrap_http_test.go`

- [x] **Step 1: Write the failing backend service tests**

Add these tests to `backend/internal/sessionbootstrap/service_test.go`:

```go
func TestBootstrapNoLongerCreatesRelayKeyOrEnvSecrets(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("g-repo").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)

	rp := &fakeRelayProvider{}
	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if rp.lastCreateUserAPIKeyUserID != 0 {
		t.Fatalf("unexpected CreateUserAPIKey call for userID=%d", rp.lastCreateUserAPIKeyUserID)
	}
	if _, ok := resp.EnvBundle["OPENAI_API_KEY"]; ok {
		t.Fatalf("bootstrap env must not include OPENAI_API_KEY: %+v", resp.EnvBundle)
	}
	if _, ok := resp.EnvBundle["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("bootstrap env must not include ANTHROPIC_API_KEY: %+v", resp.EnvBundle)
	}
}

func TestResolveProviderCredentialReusesUsernameMatchBeforeCreating(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("42").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	now := time.Now()
	rp := &fakeRelayProvider{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:         900,
					UserID:     userID,
					Key:        "sk-existing-openai",
					Name:       "alice",
					Status:     "active",
					LastUsedAt: ptrTime(now),
					CreatedAt:  now.Add(-time.Hour),
					Group:      &relay.Group{ID: 42, Platform: "openai"},
				},
			}, nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "42", 2*time.Hour)
	cred, err := svc.ResolveProviderCredential(ctx, u.ID, sid, "openai")
	if err != nil {
		t.Fatalf("ResolveProviderCredential: %v", err)
	}

	if cred.APIKeyID != 900 {
		t.Fatalf("api_key_id = %d, want %d", cred.APIKeyID, 900)
	}
	if cred.APIKey != "sk-existing-openai" {
		t.Fatalf("api_key = %q, want %q", cred.APIKey, "sk-existing-openai")
	}
	if rp.lastCreateUserAPIKeyUserID != 0 {
		t.Fatalf("unexpected CreateUserAPIKey call: %d", rp.lastCreateUserAPIKeyUserID)
	}
}

func TestResolveProviderCredentialFallsBackToEmailPrefixThenCreates(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("77").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("a.smith@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	createCalls := 0
	rp := &fakeRelayProvider{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:        901,
					UserID:    userID,
					Key:       "sk-existing-other-platform",
					Name:      "alice",
					Status:    "active",
					CreatedAt: time.Now(),
					Group:     &relay.Group{ID: 77, Platform: "openai"},
				},
			}, nil
		},
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			createCalls++
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{
					ID:        999,
					UserID:    userID,
					Name:      req.Name,
					Status:    "active",
					CreatedAt: time.Now(),
				},
				Secret: "sk-created-anthropic",
			}, nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "77", 2*time.Hour)
	cred, err := svc.ResolveProviderCredential(ctx, u.ID, sid, "anthropic")
	if err != nil {
		t.Fatalf("ResolveProviderCredential: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", createCalls)
	}
	if cred.APIKeyID != 999 {
		t.Fatalf("api_key_id = %d, want %d", cred.APIKeyID, 999)
	}
	if rp.lastCreateUserAPIKeyReq.Name != "alice" {
		t.Fatalf("created key name = %q, want %q", rp.lastCreateUserAPIKeyReq.Name, "alice")
	}
}
```

- [x] **Step 2: Run the service tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./internal/sessionbootstrap -run 'Test(BootstrapNoLongerCreatesRelayKeyOrEnvSecrets|ResolveProviderCredentialReusesUsernameMatchBeforeCreating|ResolveProviderCredentialFallsBackToEmailPrefixThenCreates)$' -count=1
```

Expected:

```text
FAIL because Bootstrap still creates a key
FAIL because ResolveProviderCredential does not exist
```

- [x] **Step 3: Implement backend service changes**

In `backend/internal/sessionbootstrap/service.go`:

1. Add a response type for lazy credentials:

```go
type ProviderCredentialResponse struct {
	ProviderName string `json:"provider_name"`
	Platform     string `json:"platform"`
	APIKeyID     int64  `json:"api_key_id"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url"`
}
```

2. Remove bootstrap-time key creation from `Bootstrap()`:

```go
runtimeRef := fmt.Sprintf("runtime/%s", sessionID.String())

create := s.entClient.Session.Create().
	SetID(sessionID).
	SetRepoConfigID(rc.ID).
	SetBranch(req.BranchSnapshot).
	SetRelayUserID(int(relayUserID)).
	SetProviderName(binding.ProviderName).
	SetRuntimeRef(runtimeRef).
	SetLastSeenAt(now).
	SetStartedAt(now).
	SetInitialWorkspaceRoot(req.WorkspaceRoot).
	SetInitialGitDir(req.GitDir).
	SetInitialGitCommonDir(req.GitCommonDir)
```

3. Shrink `envBundle` to metadata only:

```go
envBundle := map[string]string{
	"AE_SESSION_ID":    sessionID.String(),
	"AE_RUNTIME_REF":   runtimeRef,
	"AE_PROVIDER_NAME": binding.ProviderName,
	"AE_ENV_VERSION":   "1",
}
```

4. Add a new method:

```go
func (s *Service) ResolveProviderCredential(ctx context.Context, localUserID int, sessionID uuid.UUID, platform string) (*ProviderCredentialResponse, error) {
	if strings.TrimSpace(platform) == "" {
		return nil, fmt.Errorf("resolve provider credential: platform is required")
	}
	if s.entClient == nil || s.relayProvider == nil {
		return nil, fmt.Errorf("resolve provider credential: service not configured")
	}

	sess, err := s.entClient.Session.Query().
		Where(session.IDEQ(sessionID)).
		WithRepoConfig().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: get session: %w", err)
	}
	if sess.UserID == nil || *sess.UserID != localUserID {
		return nil, fmt.Errorf("resolve provider credential: session not found")
	}

	u, err := s.entClient.User.Get(ctx, localUserID)
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: get user: %w", err)
	}
	if u.RelayUserID == nil {
		return nil, fmt.Errorf("resolve provider credential: relay user is not bound")
	}

	keys, err := s.relayProvider.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: list api keys: %w", err)
	}

	emailPrefix := ""
	if at := strings.Index(strings.TrimSpace(u.Email), "@"); at > 0 {
		emailPrefix = strings.TrimSpace(u.Email)[:at]
	}
	selected := selectReusableKey(keys, platform, strings.TrimSpace(u.Username), emailPrefix)
	if selected == nil {
		binding, err := s.resolveRouteBinding(ctx, sess.Edges.RepoConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: %w", err)
		}
		createCtx, err := s.contextWithRelayCredentials(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: %w", err)
		}
		created, err := s.relayProvider.CreateUserAPIKey(createCtx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{
			Name:    preferredKeyName(strings.TrimSpace(u.Username), strings.TrimSpace(u.Email)),
			GroupID: binding.GroupID,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: create api key: %w", err)
		}
		selected = &created.APIKey
		selected.Key = created.Secret
	}

	return &ProviderCredentialResponse{
		ProviderName: s.relayProvider.Name(),
		Platform:     platform,
		APIKeyID:     selected.ID,
		APIKey:       selected.Key,
		BaseURL:      s.providerBaseURL,
	}, nil
}
```

Update `fakeRelayProvider` in `service_test.go` to support `listUserAPIKeysFn`.

- [x] **Step 4: Add the HTTP endpoint and failing HTTP tests**

Add a focused HTTP test in `backend/internal/handler/session_bootstrap_http_test.go`:

```go
func TestSessionProviderCredentialHTTP_ReusesExistingOpenAIKey(t *testing.T) {
	env := setupBootstrapHTTPTestEnv(t)
	ctx := context.Background()

	sp := env.client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := env.client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("42").
		SaveX(ctx)

	u := env.client.User.Query().OnlyX(ctx)
	sid := uuid.New()
	env.client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	rp := &fakeRelayProviderForBootstrap{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:        900,
					UserID:    userID,
					Key:       "sk-existing-openai",
					Name:      "admin",
					Status:    "active",
					CreatedAt: time.Now(),
					Group:     &relay.Group{ID: 42, Platform: "openai"},
				},
			}, nil
		},
	}
	bootstrapSvc := sessionbootstrap.NewService(env.client, rp, nil, "sub2api", "http://relay.local/v1", "42", 2*time.Hour)
	env.router = SetupRouter(env.client, env.authSvc, nil, nil, webhook.NewHandler(env.client, nil, zap.NewNop()), nil, nil, nil, nil, nil, "0000000000000000000000000000000000000000000000000000000000000000", middleware.CORS(nil), nil, nil, nil, bootstrapSvc, nil)

	w := doRequest(env, "GET", "/api/v1/sessions/"+sid.String()+"/provider-credentials?platform=openai", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["api_key_id"] != float64(900) {
		t.Fatalf("api_key_id = %v, want 900", data["api_key_id"])
	}
	if data["api_key"] != "sk-existing-openai" {
		t.Fatalf("api_key = %v, want sk-existing-openai", data["api_key"])
	}
}
```

Register the route in `backend/internal/handler/router.go`:

```go
protected.GET("/sessions/:id/provider-credentials", sessionHandler.ProviderCredential)
```

Implement in `backend/internal/handler/session.go`:

```go
func (h *SessionHandler) ProviderCredential(c *gin.Context) {
	if h.bootstrapSvc == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "bootstrap service not configured")
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if platform == "" {
		pkg.Error(c, http.StatusBadRequest, "platform is required")
		return
	}
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp, err := h.bootstrapSvc.ResolveProviderCredential(c.Request.Context(), uc.UserID, sessionID, platform)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, resp)
}
```

- [x] **Step 5: Run backend tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./internal/sessionbootstrap -run 'Test(BootstrapNoLongerCreatesRelayKeyOrEnvSecrets|ResolveProviderCredentialReusesUsernameMatchBeforeCreating|ResolveProviderCredentialFallsBackToEmailPrefixThenCreates)$' -count=1
go test ./internal/handler -run 'TestSessionProviderCredentialHTTP_ReusesExistingOpenAIKey$' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/sessionbootstrap	...
ok  	github.com/ai-efficiency/backend/internal/handler	...
```

- [x] **Step 6: Commit**

```bash
git add backend/internal/sessionbootstrap/key_selection.go \
        backend/internal/sessionbootstrap/key_selection_test.go \
        backend/internal/sessionbootstrap/service.go \
        backend/internal/sessionbootstrap/service_test.go \
        backend/internal/relay/types.go \
        backend/internal/relay/sub2api.go \
        backend/internal/relay/sub2api_test.go \
        backend/internal/handler/session.go \
        backend/internal/handler/router.go \
        backend/internal/handler/session_bootstrap_http_test.go
git commit -m "feat(backend): lazily reuse platform-specific relay keys"
```

### Task 3: Refactor ae-cli Proxy To Lazily Fetch And Cache Credentials

**Files:**
- Create: `ae-cli/internal/proxy/credentials.go`
- Create: `ae-cli/internal/proxy/credentials_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/proxy/config.go`
- Modify: `ae-cli/internal/proxy/openai.go`
- Modify: `ae-cli/internal/proxy/anthropic.go`
- Modify: `ae-cli/internal/proxy/server_test.go`
- Modify: `ae-cli/internal/session/manager.go`
- Modify: `ae-cli/internal/session/session_test.go`
- Test: `ae-cli/internal/proxy/credentials_test.go`
- Test: `ae-cli/internal/client/client_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/session/session_test.go`

- [x] **Step 1: Write the failing ae-cli tests**

Add a backend client test in `ae-cli/internal/client/client_test.go`:

```go
func TestGetSessionProviderCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess-1/provider-credentials" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("platform"); got != "openai" {
			t.Fatalf("platform query = %q, want openai", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"provider_name": "sub2api",
				"platform":      "openai",
				"api_key_id":    900,
				"api_key":       "sk-existing-openai",
				"base_url":      "http://relay.local/v1",
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	cred, err := c.GetSessionProviderCredential(context.Background(), "sess-1", "openai")
	if err != nil {
		t.Fatalf("GetSessionProviderCredential: %v", err)
	}
	if cred.APIKeyID != 900 || cred.APIKey != "sk-existing-openai" {
		t.Fatalf("credential = %+v", cred)
	}
}
```

Create `ae-cli/internal/proxy/credentials_test.go`:

```go
package proxy

import (
	"context"
	"testing"
)

type fakeCredentialClient struct {
	calls int
	lastPlatform string
}

func (f *fakeCredentialClient) GetSessionProviderCredential(_ context.Context, sessionID, platform string) (*ProviderCredential, error) {
	f.calls++
	f.lastPlatform = platform
	return &ProviderCredential{
		ProviderName: "sub2api",
		Platform: platform,
		APIKeyID: 900,
		APIKey: "sk-"+platform,
		BaseURL: "http://relay.local/v1",
	}, nil
}

func TestCredentialCacheFetchesOncePerPlatform(t *testing.T) {
	cache := newCredentialCache(&fakeCredentialClient{})
	first, err := cache.Get(context.Background(), "sess-1", "openai")
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	second, err := cache.Get(context.Background(), "sess-1", "openai")
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if first.APIKey != second.APIKey {
		t.Fatalf("cached credentials mismatch: %+v %+v", first, second)
	}
}
```

Add a proxy test in `ae-cli/internal/proxy/server_test.go`:

```go
func TestProxyOpenAILazilyFetchesCredentialFromBackend(t *testing.T) {
	var backendCalls int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions/sess-openai/provider-credentials" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		backendCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"provider_name": "sub2api",
				"platform": "openai",
				"api_key_id": 900,
				"api_key": "sk-existing-openai",
				"base_url": upstream.URL,
			},
		})
	}))
	defer backend.Close()

	// start proxy with BackendURL/BackendToken only; no ProviderURL/ProviderKey
}
```

Update `ae-cli/internal/session/session_test.go` to fail if start still leaves `OPENAI_API_KEY` in the runtime bundle.

- [x] **Step 2: Run the targeted tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/ae-cli
go test ./internal/client -run 'TestGetSessionProviderCredential$' -count=1
go test ./internal/proxy -run 'TestCredentialCacheFetchesOncePerPlatform|TestProxyOpenAILazilyFetchesCredentialFromBackend' -count=1
go test ./internal/session -run 'TestStartInTempGitRepo$' -count=1
```

Expected:

```text
FAIL because client method / credential cache / lazy proxy fetch do not exist yet
```

- [x] **Step 3: Implement minimal ae-cli lazy credential flow**

Add to `ae-cli/internal/client/client.go`:

```go
type ProviderCredential struct {
	ProviderName string `json:"provider_name"`
	Platform     string `json:"platform"`
	APIKeyID     int64  `json:"api_key_id"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url"`
}

func (c *Client) GetSessionProviderCredential(ctx context.Context, sessionID, platform string) (*ProviderCredential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/sessions/"+sessionID+"/provider-credentials?platform="+url.QueryEscape(platform), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data ProviderCredential `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &envelope.Data, nil
}
```

Create `ae-cli/internal/proxy/credentials.go`:

```go
package proxy

import (
	"context"
	"fmt"
	"sync"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type credentialFetcher interface {
	GetSessionProviderCredential(ctx context.Context, sessionID, platform string) (*client.ProviderCredential, error)
}

type credentialCache struct {
	mu      sync.Mutex
	entries map[string]*client.ProviderCredential
	fetcher credentialFetcher
}

func newCredentialCache(fetcher credentialFetcher) *credentialCache {
	return &credentialCache{
		entries: map[string]*client.ProviderCredential{},
		fetcher: fetcher,
	}
}

func (c *credentialCache) Get(ctx context.Context, sessionID, platform string) (*client.ProviderCredential, error) {
	c.mu.Lock()
	if cred, ok := c.entries[platform]; ok {
		c.mu.Unlock()
		return cred, nil
	}
	c.mu.Unlock()

	if c.fetcher == nil {
		return nil, fmt.Errorf("credential cache fetcher is not configured")
	}
	cred, err := c.fetcher.GetSessionProviderCredential(ctx, sessionID, platform)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[platform] = cred
	c.mu.Unlock()
	return cred, nil
}
```

Refactor `ae-cli/internal/proxy/config.go`:

```go
type RuntimeConfig struct {
	SessionID    string            `json:"session_id"`
	WorkspaceID  string            `json:"workspace_id,omitempty"`
	ListenAddr   string            `json:"listen_addr"`
	AuthToken    string            `json:"auth_token"`
	BackendURL   string            `json:"backend_url,omitempty"`
	BackendToken string            `json:"backend_token,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}
```

Refactor `ae-cli/internal/proxy/openai.go` and `anthropic.go` to fetch:

```go
cred, err := s.credentialCache.Get(r.Context(), s.cfg.SessionID, "openai")
if err != nil {
	http.Error(w, err.Error(), http.StatusBadGateway)
	return
}
upstreamURL := strings.TrimRight(cred.BaseURL, "/") + upstreamPath
upstreamReq.Header.Set("Authorization", "Bearer "+cred.APIKey)
```

And similarly for `"anthropic"` with `x-api-key`.

In `NewServer`, initialize:

```go
var fetcher credentialFetcher
if backendURL != "" && backendToken != "" {
	fetcher = client.New(backendURL, backendToken)
}
server := &Server{...}
server.credentialCache = newCredentialCache(fetcher)
```

In `ae-cli/internal/session/manager.go`, remove any dependency on `OPENAI_*` being present for runtime bootstrap and call `startLocalProxy(rt)` with backend-only config.

- [x] **Step 4: Run the ae-cli tests to verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/ae-cli
go test ./internal/client -run 'TestGetSessionProviderCredential$' -count=1
go test ./internal/proxy -run 'TestCredentialCacheFetchesOncePerPlatform|TestProxyOpenAILazilyFetchesCredentialFromBackend' -count=1
go test ./internal/session -run 'TestStartInTempGitRepo$' -count=1
```

Expected:

```text
ok  	.../internal/client
ok  	.../internal/proxy
ok  	.../internal/session
```

- [x] **Step 5: Commit**

```bash
git add ae-cli/internal/client/client.go \
        ae-cli/internal/client/client_test.go \
        ae-cli/internal/proxy/config.go \
        ae-cli/internal/proxy/credentials.go \
        ae-cli/internal/proxy/credentials_test.go \
        ae-cli/internal/proxy/openai.go \
        ae-cli/internal/proxy/anthropic.go \
        ae-cli/internal/proxy/server_test.go \
        ae-cli/internal/session/manager.go \
        ae-cli/internal/session/session_test.go
git commit -m "feat(ae-cli): lazily fetch platform relay credentials"
```

### Task 4: Remove Bootstrap-Time Key Creation And Final Verification

**Files:**
- Modify: `backend/internal/sessionbootstrap/service.go`
- Modify: `backend/internal/sessionbootstrap/service_test.go`
- Modify: `backend/internal/handler/session_bootstrap_http_test.go`
- Modify: `ae-cli/internal/session/session_test.go`
- Test: `backend/internal/sessionbootstrap/service_test.go`
- Test: `backend/internal/handler/session_bootstrap_http_test.go`
- Test: `backend/internal/relay/sub2api_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/session/session_test.go`

- [x] **Step 1: Update old bootstrap tests to the new contract**

Change old assertions in:

- `backend/internal/sessionbootstrap/service_test.go`
- `backend/internal/handler/session_bootstrap_http_test.go`

Replace:

```go
if got := resp.EnvBundle["OPENAI_API_KEY"]; got != "sk-session-555" {
	t.Fatalf("env OPENAI_API_KEY = %q, want %q", got, "sk-session-555")
}
```

With:

```go
if _, ok := resp.EnvBundle["OPENAI_API_KEY"]; ok {
	t.Fatalf("bootstrap env must not include OPENAI_API_KEY: %+v", resp.EnvBundle)
}
if _, ok := resp.EnvBundle["ANTHROPIC_API_KEY"]; ok {
	t.Fatalf("bootstrap env must not include ANTHROPIC_API_KEY: %+v", resp.EnvBundle)
}
```

- [x] **Step 2: Run focused full-stack regression tests**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./internal/sessionbootstrap ./internal/handler ./internal/relay -count=1

cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/ae-cli
go test ./internal/client ./internal/proxy ./internal/session -count=1
```

Expected:

```text
PASS
```

- [x] **Step 3: Run full project verification**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/backend
go test ./...

cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/ae-cli
go test ./...

cd /Users/admin/ai-efficiency/.worktrees/key-reuse-platform/frontend
pnpm test
pnpm build
```

Expected:

```text
all green
```

- [x] **Step 4: Manual spot-check**

Verify on a real session:

1. `ae-cli start` does **not** create a new sub2api key immediately.
2. Starting only `Codex` reuses or creates exactly one `openai` platform key.
3. Starting only `Claude` reuses or creates exactly one `anthropic` platform key.
4. Re-opening the same tool in the same session does not create another key.
5. Matching order is:
   - `platform`
   - `active`
   - `username`
   - `email prefix`
   - latest `last_used_at`
   - latest `created_at`

- [x] **Step 5: Commit any final test-only adjustments**

```bash
git add backend/internal/sessionbootstrap/service.go \
        backend/internal/sessionbootstrap/service_test.go \
        backend/internal/handler/session.go \
        backend/internal/handler/router.go \
        backend/internal/handler/session_bootstrap_http_test.go \
        backend/internal/relay/types.go \
        backend/internal/relay/sub2api.go \
        backend/internal/relay/sub2api_test.go \
        ae-cli/internal/client/client.go \
        ae-cli/internal/client/client_test.go \
        ae-cli/internal/proxy/config.go \
        ae-cli/internal/proxy/credentials.go \
        ae-cli/internal/proxy/credentials_test.go \
        ae-cli/internal/proxy/openai.go \
        ae-cli/internal/proxy/anthropic.go \
        ae-cli/internal/proxy/server_test.go \
        ae-cli/internal/session/manager.go \
        ae-cli/internal/session/session_test.go
git commit -m "test: finalize lazy platform key reuse coverage"
```
