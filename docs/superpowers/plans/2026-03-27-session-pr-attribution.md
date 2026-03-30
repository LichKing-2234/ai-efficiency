# Session / PR Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the v1 session/bootstrap, workspace marker/runtime, hook checkpoint capture, collector snapshots, and manual PR settlement chain so every attributable PR can be reconciled against exact relay usage logs.

**Architecture:** The implementation keeps the existing configured relay provider as the authoritative session-key issuer and primary-ledger reader, while expanding the backend data model so sessions, workspaces, checkpoints, rewrites, and attribution runs become first-class audit records. `ae-cli` owns local state (`/.ae/session.json`, `~/.ae-cli/runtime/<session-id>/`, shared git hooks, and local collector snapshots); the backend owns identity resolution, session key lifecycle, checkpoint ingestion, and PR settlement; the frontend only reads and triggers that chain. The v1 collector is implemented as reusable snapshot readers invoked by hook/flush/stop flows instead of a long-lived daemon, which is sufficient because the collected values are cumulative and settlement only needs checkpoint-aligned deltas.

**Tech Stack:** Go 1.24+/1.23+ (Cobra, Gin, Ent, zap, go-github), Vue 3 + Vite + TypeScript + Vitest, Git hooks, tmux, local JSON/JSONL parsing

**Spec:** `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md`

**Execution Note:** This stays one plan because bootstrap, checkpoints, and settlement all depend on the same identity/session schema. The tasks still land in independently testable slices so the chain can be verified incrementally.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `backend/internal/relay/provider.go` | Expand relay contract for username lookup/provisioning, session-key lifecycle, and exact usage logs |
| Modify | `backend/internal/relay/types.go` | Add request/response types for relay users, API keys, and usage logs |
| Modify | `backend/internal/relay/sub2api.go` | Implement new relay admin API calls |
| Modify | `backend/internal/relay/sub2api_test.go` | Cover the new relay admin API behavior |
| Create | `backend/internal/auth/relay_identity.go` | Resolve or provision relay users by username |
| Create | `backend/internal/auth/relay_identity_test.go` | Relay identity resolver tests |
| Modify | `backend/internal/auth/auth.go` | Reuse relay identity resolution during login/local-user sync |
| Modify | `backend/internal/auth/ldap.go` | Preserve username-first identity data for LDAP logins |
| Modify | `backend/internal/config/config.go` | Add relay default-group fallback config |
| Modify | `backend/ent/schema/repoconfig.go` | Add repo-level relay binding fields |
| Modify | `backend/ent/schema/session.go` | Add bootstrap/runtime snapshot fields |
| Modify | `backend/ent/schema/prrecord.go` | Add attribution summary fields while preserving legacy heuristic fields |
| Create | `backend/ent/schema/session_workspace.go` | Persist session/workspace observation records |
| Create | `backend/ent/schema/agent_metadata_event.go` | Persist normalized local collector snapshots |
| Create | `backend/ent/schema/commit_checkpoint.go` | Persist commit checkpoint anchor events |
| Create | `backend/ent/schema/commit_rewrite.go` | Persist amend/rebase/squash rewrite events |
| Create | `backend/ent/schema/pr_attribution_run.go` | Persist manual PR settlement audit runs |
| Create | `backend/internal/sessionbootstrap/service.go` | Bootstrap, heartbeat, and stop lifecycle orchestration |
| Create | `backend/internal/sessionbootstrap/service_test.go` | Bootstrap lifecycle tests |
| Create | `backend/internal/checkpoint/service.go` | Idempotent checkpoint/rewrite ingestion |
| Create | `backend/internal/checkpoint/service_test.go` | Checkpoint ingestion tests |
| Create | `backend/internal/attribution/service.go` | Manual PR settlement algorithm |
| Create | `backend/internal/attribution/service_test.go` | Clear/ambiguous settlement tests |
| Modify | `backend/internal/scm/provider.go` | Add `ListPRCommits` contract |
| Modify | `backend/internal/scm/github/github.go` | Implement GitHub PR commit enumeration |
| Modify | `backend/internal/scm/github/github_test.go` | Test GitHub PR commit enumeration |
| Modify | `backend/internal/scm/bitbucket/bitbucket.go` | Implement Bitbucket PR commit enumeration |
| Modify | `backend/internal/scm/bitbucket/bitbucket_test.go` | Test Bitbucket PR commit enumeration |
| Modify | `backend/internal/repo/service.go` | Persist repo relay binding fields |
| Modify | `backend/internal/handler/interfaces.go` | Add attribution service abstraction |
| Modify | `backend/internal/handler/session.go` | Add bootstrap endpoint and lifecycle integration while retaining legacy create |
| Create | `backend/internal/handler/checkpoint.go` | Hook ingest endpoints |
| Create | `backend/internal/handler/checkpoint_test.go` | Checkpoint endpoint tests |
| Modify | `backend/internal/handler/pr.go` | Add manual settle endpoint |
| Create | `backend/internal/handler/pr_attribution_test.go` | Manual settle handler tests |
| Modify | `backend/internal/handler/router.go` | Register bootstrap/checkpoint/settle routes |
| Modify | `backend/cmd/server/main.go` | Construct and wire bootstrap/checkpoint/attribution services |
| Modify | `ae-cli/internal/client/client.go` | Add bootstrap/checkpoint client methods |
| Modify | `ae-cli/internal/client/client_test.go` | Cover new ae-cli HTTP methods |
| Create | `ae-cli/internal/session/workspace.go` | Git context, workspace ID derivation, marker I/O, and legacy-state fallback |
| Create | `ae-cli/internal/session/workspace_test.go` | Workspace derivation and marker precedence tests |
| Create | `ae-cli/internal/session/runtime.go` | Runtime bundle/env/queue paths and permissions |
| Create | `ae-cli/internal/session/runtime_test.go` | Runtime bundle tests |
| Modify | `ae-cli/internal/session/manager.go` | Bootstrap/start/restore/stop orchestration |
| Modify | `ae-cli/internal/session/session_test.go` | Session lifecycle tests |
| Create | `ae-cli/internal/hooks/install.go` | Shared hook installation with legacy chaining |
| Create | `ae-cli/internal/hooks/install_test.go` | Hook install safety tests |
| Create | `ae-cli/internal/hooks/handler.go` | `post-commit` / `post-rewrite` business logic |
| Create | `ae-cli/internal/hooks/handler_test.go` | Hook payload, env-bootstrap, and fail-open tests |
| Create | `ae-cli/internal/hooks/queue.go` | Local retry queue persistence/replay |
| Create | `ae-cli/internal/hooks/queue_test.go` | Queue tests |
| Create | `ae-cli/internal/collector/types.go` | Shared normalized agent-snapshot types |
| Create | `ae-cli/internal/collector/collector.go` | Snapshot orchestrator and cache writer |
| Create | `ae-cli/internal/collector/codex.go` | Codex session reader |
| Create | `ae-cli/internal/collector/claude.go` | Claude session reader |
| Create | `ae-cli/internal/collector/kiro.go` | Kiro session reader |
| Create | `ae-cli/internal/collector/collector_test.go` | Collector aggregation tests |
| Create | `ae-cli/internal/collector/testdata/codex-session.jsonl` | Codex fixture |
| Create | `ae-cli/internal/collector/testdata/claude-session.jsonl` | Claude fixture |
| Create | `ae-cli/internal/collector/testdata/kiro-session.json` | Kiro fixture |
| Create | `ae-cli/cmd/hook.go` | Hidden hook command |
| Create | `ae-cli/cmd/flush.go` | Queue replay command |
| Modify | `ae-cli/cmd/start.go` | Enforce login, bootstrap session, install hooks |
| Modify | `ae-cli/cmd/stop.go` | Stop using marker/runtime state instead of the legacy global file |
| Modify | `ae-cli/cmd/shell.go` | Restore env bundle before launching the shell |
| Modify | `ae-cli/cmd/run.go` | Use workspace-aware state resolution |
| Modify | `ae-cli/internal/dispatcher/dispatcher.go` | Inject runtime env bundle into child tool processes |
| Modify | `frontend/src/types/index.ts` | Extend repo/session/PR types with attribution fields |
| Modify | `frontend/src/api/session.ts` | Session detail typing remains aligned with new backend shape |
| Modify | `frontend/src/api/pr.ts` | Add manual settle endpoint |
| Modify | `frontend/src/views/repos/RepoDetailView.vue` | Show attribution summary and per-PR settle actions |
| Modify | `frontend/src/views/sessions/SessionListView.vue` | Show provider/key/last-seen summary |
| Create | `frontend/src/views/sessions/SessionDetailView.vue` | Audit session bootstrap/workspace/checkpoint data |
| Modify | `frontend/src/router/index.ts` | Route real session detail page instead of aliasing the list view |
| Modify | `frontend/src/__tests__/api-modules.test.ts` | Cover settle endpoint |
| Create | `frontend/src/__tests__/repo-detail-view.test.ts` | Attribution UI tests |
| Create | `frontend/src/__tests__/session-detail-view.test.ts` | Session audit view tests |
| Create | `docs/ae-cli/session-pr-attribution.md` | Operator-facing lifecycle and recovery guide |

### Task 1: Relay Identity And Primary-Ledger API Surface

**Files:**
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Create: `backend/internal/auth/relay_identity.go`
- Create: `backend/internal/auth/relay_identity_test.go`
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/auth/ldap.go`

- [ ] **Step 1: Write failing relay identity and usage-log tests**

Create `backend/internal/auth/relay_identity_test.go` with:

```go
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

type fakeRelayIdentityProvider struct {
	findByUsername func(context.Context, string) (*relay.User, error)
	createUser     func(context.Context, relay.CreateUserRequest) (*relay.User, error)
}

func (f *fakeRelayIdentityProvider) Ping(context.Context) error { return nil }
func (f *fakeRelayIdentityProvider) Name() string               { return "sub2api" }
func (f *fakeRelayIdentityProvider) Authenticate(context.Context, string, string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) GetUser(context.Context, int64) (*relay.User, error) { return nil, nil }
func (f *fakeRelayIdentityProvider) FindUserByEmail(context.Context, string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return f.findByUsername(ctx, username)
}
func (f *fakeRelayIdentityProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return f.createUser(ctx, req)
}
func (f *fakeRelayIdentityProvider) ChatCompletion(context.Context, relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) ChatCompletionWithTools(context.Context, relay.ChatCompletionRequest, []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) { return nil, nil }
func (f *fakeRelayIdentityProvider) CreateUserAPIKey(context.Context, int64, relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return nil, nil
}
func (f *fakeRelayIdentityProvider) RevokeUserAPIKey(context.Context, int64) error { return nil }
func (f *fakeRelayIdentityProvider) ListUsageLogsByAPIKeyExact(context.Context, int64, time.Time, time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}

func TestResolveOrProvisionRelayUser_UsesUsernameAsStableKey(t *testing.T) {
	provider := &fakeRelayIdentityProvider{
		findByUsername: func(_ context.Context, username string) (*relay.User, error) {
			if username != "alice" {
				t.Fatalf("username = %q, want alice", username)
			}
			return &relay.User{ID: 91, Username: "alice", Email: "alice@relay.local"}, nil
		},
		createUser: func(context.Context, relay.CreateUserRequest) (*relay.User, error) {
			t.Fatal("createUser should not be called when username already exists")
			return nil, nil
		},
	}

	resolver := NewRelayIdentityResolver(provider, "corp.example")
	user, err := resolver.ResolveOrProvision(context.Background(), "alice", "alice@corp.example")
	if err != nil {
		t.Fatalf("ResolveOrProvision() error = %v", err)
	}
	if user.ID != 91 {
		t.Fatalf("user.ID = %d, want 91", user.ID)
	}
}

func TestResolveOrProvisionRelayUser_ProvisionsLDAPFallbackEmail(t *testing.T) {
	provider := &fakeRelayIdentityProvider{
		findByUsername: func(context.Context, string) (*relay.User, error) { return nil, nil },
		createUser: func(_ context.Context, req relay.CreateUserRequest) (*relay.User, error) {
			if req.Username != "bob" {
				t.Fatalf("Username = %q, want bob", req.Username)
			}
			if req.Email != "bob@corp.example" {
				t.Fatalf("Email = %q, want bob@corp.example", req.Email)
			}
			if req.Notes != "provisioned_by_ai_efficiency_ldap" {
				t.Fatalf("Notes = %q, want ldap provisioning marker", req.Notes)
			}
			return &relay.User{ID: 1002, Username: req.Username, Email: req.Email}, nil
		},
	}

	resolver := NewRelayIdentityResolver(provider, "corp.example")
	user, err := resolver.ResolveOrProvision(context.Background(), "bob", "")
	if err != nil {
		t.Fatalf("ResolveOrProvision() error = %v", err)
	}
	if user.ID != 1002 {
		t.Fatalf("user.ID = %d, want 1002", user.ID)
	}
}
```

Append these tests to `backend/internal/relay/sub2api_test.go`:

```go
func TestCreateUserAPIKeyWithExpiryAndGroup(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/keys" {
			t.Fatalf("path = %s, want /api/v1/keys", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id": 88, "user_id": 9, "name": "ae-session-1234", "status": "active", "secret": "sekret",
			},
		})
	}))
	defer srv.Close()

	p := NewSub2apiProvider(srv.Client(), srv.URL, srv.URL, "admin-token", "gpt-5.4", zap.NewNop())
	expiresAt := time.Date(2026, 3, 27, 18, 0, 0, 0, time.UTC)
	key, err := p.CreateUserAPIKey(context.Background(), 9, APIKeyCreateRequest{
		Name:      "ae-session-1234",
		GroupID:   "team-ai",
		ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateUserAPIKey() error = %v", err)
	}
	if key.ID != 88 || key.Secret != "sekret" {
		t.Fatalf("CreateUserAPIKey() = %+v", key)
	}
	if body["group_id"] != "team-ai" {
		t.Fatalf("group_id = %v, want team-ai", body["group_id"])
	}
	if body["expires_at"] != expiresAt.Format(time.RFC3339) {
		t.Fatalf("expires_at = %v, want RFC3339 time", body["expires_at"])
	}
}

func TestListUsageLogsByAPIKeyExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api_key_id"); got != "88" {
			t.Fatalf("api_key_id = %q, want 88", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{
				{
					"request_id": "req-1", "api_key_id": 88, "user_id": 9, "account_id": "acct-1",
					"group_id": "team-ai", "model": "gpt-5.4", "input_tokens": 120, "output_tokens": 45,
					"cache_tokens": 10, "total_tokens": 165, "total_cost": 0.42, "actual_cost": 0.40,
					"created_at": "2026-03-27T09:00:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	p := NewSub2apiProvider(srv.Client(), srv.URL, srv.URL, "admin-token", "gpt-5.4", zap.NewNop())
	logs, err := p.ListUsageLogsByAPIKeyExact(context.Background(), 88, time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC), time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListUsageLogsByAPIKeyExact() error = %v", err)
	}
	if len(logs) != 1 || logs[0].RequestID != "req-1" {
		t.Fatalf("logs = %+v", logs)
	}
}
```

- [ ] **Step 2: Run the new auth/relay tests to verify the gap**

Run: `cd backend && go test ./internal/auth/... ./internal/relay/... -run 'TestResolveOrProvisionRelayUser|TestCreateUserAPIKeyWithExpiryAndGroup|TestListUsageLogsByAPIKeyExact' -v`

Expected: FAIL because `relay.Provider` does not yet expose `FindUserByUsername`, `CreateUser`, `RevokeUserAPIKey`, or `ListUsageLogsByAPIKeyExact`, and `NewRelayIdentityResolver` does not exist.

- [ ] **Step 3: Expand relay types and the sub2api implementation**

Update `backend/internal/relay/provider.go` and `backend/internal/relay/types.go` to:

```go
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Notes    string `json:"notes,omitempty"`
}

type APIKeyCreateRequest struct {
	Name      string    `json:"name"`
	GroupID   string    `json:"group_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type UsageLog struct {
	RequestID    string    `json:"request_id"`
	CreatedAt    time.Time `json:"created_at"`
	APIKeyID     int64     `json:"api_key_id"`
	UserID       int64     `json:"user_id"`
	AccountID    string    `json:"account_id"`
	GroupID      string    `json:"group_id"`
	Model        string    `json:"model"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CacheTokens  int64     `json:"cache_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	ActualCost   float64   `json:"actual_cost"`
}

type Provider interface {
	Ping(ctx context.Context) error
	Name() string

	Authenticate(ctx context.Context, username, password string) (*User, error)
	GetUser(ctx context.Context, userID int64) (*User, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByUsername(ctx context.Context, username string) (*User, error)
	CreateUser(ctx context.Context, req CreateUserRequest) (*User, error)

	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

	GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)
	ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)
	CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error)
	RevokeUserAPIKey(ctx context.Context, keyID int64) error
	ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]UsageLog, error)
}
```

Implement the new sub2api methods in `backend/internal/relay/sub2api.go`:

```go
func (s *sub2apiRelay) FindUserByUsername(ctx context.Context, username string) (*User, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?username="+url.QueryEscape(username), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool   `json:"success"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: find user by username: decode: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, nil
	}
	return &result.Data[0], nil
}

func (s *sub2apiRelay) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	body, _ := json.Marshal(req)
	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: create user: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool `json:"success"`
		Data    User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create user: decode: %w", err)
	}
	return &result.Data, nil
}

func (s *sub2apiRelay) CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error) {
	payload, _ := json.Marshal(map[string]any{
		"user_id":    userID,
		"name":       req.Name,
		"group_id":   req.GroupID,
		"expires_at": req.ExpiresAt.Format(time.RFC3339),
	})
	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/keys", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("relay: create api key: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool             `json:"success"`
		Data    APIKeyWithSecret `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create api key: decode: %w", err)
	}
	return &result.Data, nil
}

func (s *sub2apiRelay) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/revoke", keyID), nil)
	if err != nil {
		return fmt.Errorf("relay: revoke api key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: revoke api key: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (s *sub2apiRelay) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]UsageLog, error) {
	path := fmt.Sprintf("/api/v1/admin/usage_logs?api_key_id=%d&from=%s&to=%s", apiKeyID, url.QueryEscape(from.Format(time.RFC3339)), url.QueryEscape(to.Format(time.RFC3339)))
	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("relay: usage logs: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool       `json:"success"`
		Data    []UsageLog `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: usage logs: decode: %w", err)
	}
	return result.Data, nil
}
```

- [ ] **Step 4: Add username-first relay identity resolution and wire auth to use it**

Create `backend/internal/auth/relay_identity.go` with:

```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

type RelayIdentityResolver struct {
	provider       relay.Provider
	fallbackDomain string
}

func NewRelayIdentityResolver(provider relay.Provider, fallbackDomain string) *RelayIdentityResolver {
	return &RelayIdentityResolver{provider: provider, fallbackDomain: fallbackDomain}
}

func (r *RelayIdentityResolver) ResolveOrProvision(ctx context.Context, username, email string) (*relay.User, error) {
	if r == nil || r.provider == nil || username == "" {
		return nil, nil
	}

	existing, err := r.provider.FindUserByUsername(ctx, username)
	if err != nil || existing != nil {
		return existing, err
	}

	if email == "" {
		email = username + "@" + strings.TrimSpace(r.fallbackDomain)
	}

	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, fmt.Errorf("relay identity password: %w", err)
	}

	return r.provider.CreateUser(ctx, relay.CreateUserRequest{
		Username: username,
		Email:    email,
		Password: base64.RawURLEncoding.EncodeToString(passwordBytes),
		Notes:    "provisioned_by_ai_efficiency_ldap",
	})
}
```

Update `backend/internal/auth/auth.go` and `backend/internal/auth/ldap.go` so `auth.Service` stores a resolver and populates `RelayUserID` during local-user sync:

```go
type Service struct {
	providers       []AuthProvider
	entClient       *ent.Client
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	relayIdentity   *RelayIdentityResolver
	logger          *zap.Logger
}

func (s *Service) SetRelayIdentityResolver(resolver *RelayIdentityResolver) {
	s.relayIdentity = resolver
}

func (s *Service) ensureLocalUser(ctx context.Context, info *UserInfo) (*ent.User, error) {
	if info.RelayUserID == nil && s.relayIdentity != nil {
		relayUser, err := s.relayIdentity.ResolveOrProvision(ctx, info.Username, info.Email)
		if err != nil {
			return nil, fmt.Errorf("resolve relay identity: %w", err)
		}
		if relayUser != nil {
			relayID := int(relayUser.ID)
			info.RelayUserID = &relayID
		}
	}
	u, err := s.entClient.User.Query().Where(entuser.UsernameEQ(info.Username)).Only(ctx)
	if err == nil {
		update := u.Update().SetEmail(info.Email).SetRole(entuser.Role(info.Role))
		if info.RelayUserID != nil {
			update.SetRelayUserID(*info.RelayUserID)
		}
		return update.Save(ctx)
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	create := s.entClient.User.Create().
		SetUsername(info.Username).
		SetEmail(info.Email).
		SetAuthSource(entuser.AuthSource(info.AuthSource)).
		SetRole(entuser.Role(info.Role))
	if info.RelayUserID != nil {
		create.SetRelayUserID(*info.RelayUserID)
	}
	return create.Save(ctx)
}
```

Keep `backend/internal/auth/ldap.go` username-first by returning:

```go
return &UserInfo{
	Username:   username,
	Email:      email,
	Role:       "user",
	AuthSource: "ldap",
}, nil
```

- [ ] **Step 5: Run the auth/relay tests again**

Run: `cd backend && go test ./internal/auth/... ./internal/relay/... -run 'TestResolveOrProvisionRelayUser|TestCreateUserAPIKeyWithExpiryAndGroup|TestListUsageLogsByAPIKeyExact' -v`

Expected: PASS for the new resolver and relay-provider methods.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/relay/provider.go backend/internal/relay/types.go backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go backend/internal/auth/relay_identity.go backend/internal/auth/relay_identity_test.go backend/internal/auth/auth.go backend/internal/auth/ldap.go
git commit -m "feat(backend): add relay identity and exact usage log APIs"
```

### Task 2: Attribution Schema And Repo Relay Bindings

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/ent/schema/repoconfig.go`
- Modify: `backend/ent/schema/session.go`
- Modify: `backend/ent/schema/prrecord.go`
- Create: `backend/ent/schema/session_workspace.go`
- Create: `backend/ent/schema/agent_metadata_event.go`
- Create: `backend/ent/schema/commit_checkpoint.go`
- Create: `backend/ent/schema/commit_rewrite.go`
- Create: `backend/ent/schema/pr_attribution_run.go`
- Create: `backend/internal/attribution/schema_test.go`
- Modify: `backend/internal/repo/service.go`

- [ ] **Step 1: Write a failing Ent schema smoke test**

Create `backend/internal/attribution/schema_test.go` with:

```go
package attribution

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	_ "github.com/mattn/go-sqlite3"
)

func TestAttributionSchemasCreateAndQuery(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	provider := client.ScmProvider.Create().
		SetName("gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("secret").
		SaveX(ctx)

	repo := client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("ai-efficiency").
		SetFullName("org/ai-efficiency").
		SetCloneURL("https://github.com/org/ai-efficiency.git").
		SetDefaultBranch("main").
		SetRelayProviderName("sub2api").
		SetRelayGroupID("team-ai").
		SaveX(ctx)

	sess := client.Session.Create().
		SetRepoConfigID(repo.ID).
		SetBranch("feat/attribution").
		SetRelayUserID(42).
		SetRelayAPIKeyID(1001).
		SetProviderName("sub2api").
		SetRuntimeRef("runtime/sess-1").
		SetInitialWorkspaceRoot("/tmp/repo").
		SetInitialGitDir("/tmp/repo/.git").
		SetInitialGitCommonDir("/tmp/repo/.git").
		SetHeadSHAAtStart("abc123").
		SetLastSeenAt(time.Now()).
		SaveX(ctx)

	client.SessionWorkspace.Create().
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetWorkspaceRoot("/tmp/repo").
		SetGitDir("/tmp/repo/.git").
		SetGitCommonDir("/tmp/repo/.git").
		SetBindingSource("marker").
		SaveX(ctx)

	client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource("marker").
		SetCommitSHA("abc123").
		SetParentShas([]string{"000000"}).
		SetBranchSnapshot("feat/attribution").
		SetHeadSnapshot("abc123").
		SetAgentSnapshot(map[string]any{"codex": map[string]any{"total_tokens": 500}}).
		SetCapturedAt(time.Now()).
		SaveX(ctx)

	pr := client.PrRecord.Create().
		SetRepoConfigID(repo.ID).
		SetScmPrID(55).
		SetStatus(prrecord.StatusOpen).
		SetAttributionStatus(prrecord.AttributionStatusNotRun).
		SaveX(ctx)

	run := client.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("alice").
		SetStatus("completed").
		SetResultClassification("clear").
		SetMatchedCommitShas([]string{"abc123"}).
		SetMatchedSessionIDs([]string{sess.ID.String()}).
		SetPrimaryUsageSummary(map[string]any{"total_tokens": 500}).
		SetMetadataSummary(map[string]any{"codex": map[string]any{"total_tokens": 500}}).
		SetValidationSummary(map[string]any{"result": "consistent", "confidence": "high"}).
		SaveX(ctx)

	if run.ID == 0 {
		t.Fatal("expected attribution run ID to be assigned")
	}
}
```

- [ ] **Step 2: Run the schema smoke test to confirm the model is incomplete**

Run: `cd backend && go test ./internal/attribution -run TestAttributionSchemasCreateAndQuery -v`

Expected: FAIL because the repo/session/PR fields and the new Ent schemas do not exist yet.

- [ ] **Step 3: Add repo/session/PR fields and the new attribution schemas**

Update `backend/internal/config/config.go` so relay config exposes a default group fallback:

```go
type RelayConfig struct {
	Provider       string `mapstructure:"provider"`
	URL            string `mapstructure:"url"`
	APIKey         string `mapstructure:"api_key"`
	Model          string `mapstructure:"model"`
	DefaultGroupID string `mapstructure:"default_group_id"`
}

v.SetDefault("relay.default_group_id", "")
```

Update `backend/ent/schema/repoconfig.go`, `backend/ent/schema/session.go`, and `backend/ent/schema/prrecord.go` with:

```go
// RepoConfig
field.String("relay_provider_name").Optional().Nillable(),
field.String("relay_group_id").Optional().Nillable(),

// Session
field.String("runtime_ref").Optional().Nillable(),
field.String("initial_workspace_root").Optional().Nillable(),
field.String("initial_git_dir").Optional().Nillable(),
field.String("initial_git_common_dir").Optional().Nillable(),
field.String("head_sha_at_start").Optional().Nillable(),
field.Time("last_seen_at").Optional().Nillable(),

// PrRecord
field.Enum("attribution_status").
	Values("not_run", "clear", "ambiguous", "failed").
	Default("not_run"),
field.Enum("attribution_confidence").
	Values("high", "medium", "low").
	Optional().
	Nillable(),
field.Int64("primary_token_count").Default(0),
field.Float("primary_token_cost").Default(0),
field.JSON("metadata_summary", map[string]any{}).Optional(),
field.Time("last_attributed_at").Optional().Nillable(),
field.Int("last_attribution_run_id").Optional().Nillable(),
```

Create the new schema files with these core fields:

```go
// backend/ent/schema/session_workspace.go
type SessionWorkspace struct{ ent.Schema }

func (SessionWorkspace) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").NotEmpty(),
		field.String("workspace_root").NotEmpty(),
		field.String("git_dir").NotEmpty(),
		field.String("git_common_dir").NotEmpty(),
		field.Time("first_seen_at").Default(timeNow),
		field.Time("last_seen_at").Default(timeNow).UpdateDefault(timeNow),
		field.Enum("binding_source").Values("marker", "env_bootstrap", "manual"),
	}
}

// backend/ent/schema/agent_metadata_event.go
type AgentMetadataEvent struct{ ent.Schema }

func (AgentMetadataEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").Optional().Nillable(),
		field.Enum("source").Values("codex", "claude", "kiro"),
		field.String("source_session_id").Optional().Nillable(),
		field.Enum("usage_unit").Values("token", "credit", "unknown").Default("unknown"),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("cached_input_tokens").Default(0),
		field.Int64("reasoning_tokens").Default(0),
		field.Float("credit_usage").Default(0),
		field.Float("context_usage_pct").Default(0),
		field.JSON("raw_payload", map[string]any{}).Optional(),
		field.Time("observed_at").Default(timeNow),
	}
}

// backend/ent/schema/commit_checkpoint.go
type CommitCheckpoint struct{ ent.Schema }

func (CommitCheckpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").Unique(),
		field.UUID("session_id", uuid.UUID{}).Optional().Nillable(),
		field.String("workspace_id").NotEmpty(),
		field.Int("repo_config_id"),
		field.String("commit_sha").NotEmpty(),
		field.JSON("parent_shas", []string{}),
		field.String("branch_snapshot").Optional().Nillable(),
		field.String("head_snapshot").Optional().Nillable(),
		field.Enum("binding_source").Values("marker", "env_bootstrap", "manual", "unbound"),
		field.JSON("agent_snapshot", map[string]any{}).Optional(),
		field.Time("captured_at").Default(timeNow),
	}
}

// backend/ent/schema/commit_rewrite.go
type CommitRewrite struct{ ent.Schema }

func (CommitRewrite) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").Unique(),
		field.UUID("session_id", uuid.UUID{}).Optional().Nillable(),
		field.String("workspace_id").NotEmpty(),
		field.Int("repo_config_id"),
		field.Enum("rewrite_type").Values("amend", "rebase", "squash", "unknown").Default("unknown"),
		field.String("old_commit_sha").NotEmpty(),
		field.String("new_commit_sha").NotEmpty(),
		field.Enum("binding_source").Values("marker", "env_bootstrap", "manual", "unbound"),
		field.Time("captured_at").Default(timeNow),
	}
}

// backend/ent/schema/pr_attribution_run.go
type PrAttributionRun struct{ ent.Schema }

func (PrAttributionRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("pr_record_id"),
		field.Enum("trigger_mode").Values("manual").Default("manual"),
		field.String("triggered_by").NotEmpty(),
		field.Enum("status").Values("completed", "failed"),
		field.Enum("result_classification").Values("clear", "ambiguous"),
		field.JSON("matched_commit_shas", []string{}),
		field.JSON("matched_session_ids", []string{}),
		field.JSON("primary_usage_summary", map[string]any{}).Optional(),
		field.JSON("metadata_summary", map[string]any{}).Optional(),
		field.JSON("validation_summary", map[string]any{}).Optional(),
		field.String("error_message").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
	}
}
```

Also add the matching Ent edges:

```go
// backend/ent/schema/session.go
edge.To("session_workspaces", SessionWorkspace.Type),
edge.To("agent_metadata_events", AgentMetadataEvent.Type),
edge.To("commit_checkpoints", CommitCheckpoint.Type),
edge.To("commit_rewrites", CommitRewrite.Type),

// backend/ent/schema/prrecord.go
edge.To("attribution_runs", PrAttributionRun.Type),
```

Update `backend/internal/repo/service.go` so repo create/update requests can persist the new binding fields:

```go
type CreateRequest struct {
	SCMProviderID     int    `json:"scm_provider_id" binding:"required"`
	FullName          string `json:"full_name" binding:"required"`
	GroupID           string `json:"group_id"`
	RelayProviderName string `json:"relay_provider_name"`
	RelayGroupID      string `json:"relay_group_id"`
}

type UpdateRequest struct {
	Name              string            `json:"name"`
	GroupID           string            `json:"group_id"`
	Status            string            `json:"status"`
	RelayProviderName string            `json:"relay_provider_name"`
	RelayGroupID      string            `json:"relay_group_id"`
	ScanPromptOverride map[string]string `json:"scan_prompt_override,omitempty"`
	ClearScanPrompt   bool              `json:"clear_scan_prompt,omitempty"`
}
```

- [ ] **Step 4: Regenerate Ent code and rerun the schema smoke test**

Run:

```bash
cd backend
go generate ./ent
go test ./internal/attribution -run TestAttributionSchemasCreateAndQuery -v
```

Expected: the Ent generation completes cleanly and the schema smoke test PASSes.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/ent/schema/repoconfig.go backend/ent/schema/session.go backend/ent/schema/prrecord.go backend/ent/schema/session_workspace.go backend/ent/schema/agent_metadata_event.go backend/ent/schema/commit_checkpoint.go backend/ent/schema/commit_rewrite.go backend/ent/schema/pr_attribution_run.go backend/internal/attribution/schema_test.go backend/internal/repo/service.go backend/ent
git commit -m "feat(backend): add attribution schemas and repo relay bindings"
```

### Task 3: Bootstrap API And Session Lifecycle

**Files:**
- Create: `backend/internal/sessionbootstrap/service.go`
- Create: `backend/internal/sessionbootstrap/service_test.go`
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing bootstrap lifecycle tests**

Create `backend/internal/sessionbootstrap/service_test.go` with:

```go
package sessionbootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/relay"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

type fakeRelayProvider struct {
	findByUsername func(context.Context, string) (*relay.User, error)
	createUser     func(context.Context, relay.CreateUserRequest) (*relay.User, error)
	createKey      func(context.Context, int64, relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error)
	revokeKey      func(context.Context, int64) error
}

func (f *fakeRelayProvider) Ping(context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string               { return "sub2api" }
func (f *fakeRelayProvider) Authenticate(context.Context, string, string) (*relay.User, error) { return nil, nil }
func (f *fakeRelayProvider) GetUser(context.Context, int64) (*relay.User, error) { return nil, nil }
func (f *fakeRelayProvider) FindUserByEmail(context.Context, string) (*relay.User, error) { return nil, nil }
func (f *fakeRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return f.findByUsername(ctx, username)
}
func (f *fakeRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return f.createUser(ctx, req)
}
func (f *fakeRelayProvider) ChatCompletion(context.Context, relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ChatCompletionWithTools(context.Context, relay.ChatCompletionRequest, []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) { return nil, nil }
func (f *fakeRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return f.createKey(ctx, userID, req)
}
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error { return f.revokeKey(ctx, keyID) }
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(context.Context, int64, time.Time, time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}

func TestBootstrapCreatesSessionKeyAndEnvBundle(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	scm := client.ScmProvider.Create().
		SetName("gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	repo := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName("ai-efficiency").
		SetFullName("org/ai-efficiency").
		SetCloneURL("https://github.com/org/ai-efficiency.git").
		SetDefaultBranch("main").
		SetRelayProviderName("sub2api").
		SetRelayGroupID("team-ai").
		SaveX(ctx)

	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@corp.example").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)

	provider := &fakeRelayProvider{
		findByUsername: func(context.Context, string) (*relay.User, error) {
			return &relay.User{ID: 91, Username: "alice", Email: "alice@corp.example"}, nil
		},
		createUser: func(context.Context, relay.CreateUserRequest) (*relay.User, error) { return nil, nil },
		createKey: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			if userID != 91 {
				t.Fatalf("userID = %d, want 91", userID)
			}
			if req.GroupID != "team-ai" {
				t.Fatalf("GroupID = %q, want team-ai", req.GroupID)
			}
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 501, UserID: 91, Name: req.Name, Status: "active"},
				Secret: "relay-secret",
			}, nil
		},
		revokeKey: func(context.Context, int64) error { return nil },
	}

	resolver := auth.NewRelayIdentityResolver(provider, "corp.example")
	svc := NewService(client, provider, resolver, "http://relay.example/v1", "default-team", zap.NewNop())
	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   repo.FullName,
		BranchSnapshot: "feat/attribution",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/repo",
		GitDir:         "/tmp/repo/.git",
		GitCommonDir:   "/tmp/repo/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if resp.RelayAPIKeyID != 501 {
		t.Fatalf("RelayAPIKeyID = %d, want 501", resp.RelayAPIKeyID)
	}
	if resp.EnvBundle["AE_SESSION_ID"] == "" || resp.EnvBundle["OPENAI_API_KEY"] != "relay-secret" {
		t.Fatalf("EnvBundle = %#v", resp.EnvBundle)
	}
}

func TestStopRevokesRelayKey(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	scm := client.ScmProvider.Create().
		SetName("gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	repo := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName("ai-efficiency").
		SetFullName("org/ai-efficiency").
		SetCloneURL("https://github.com/org/ai-efficiency.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@corp.example").
		SetAuthSource(user.AuthSourceLdap).
		SetRelayUserID(91).
		SaveX(ctx)

	sess := client.Session.Create().
		SetRepoConfigID(repo.ID).
		SetUserID(u.ID).
		SetBranch("feat/attribution").
		SetRelayUserID(91).
		SetRelayAPIKeyID(501).
		SetProviderName("sub2api").
		SetRuntimeRef("runtime/sess-1").
		SaveX(ctx)

	revoked := false
	provider := &fakeRelayProvider{
		findByUsername: func(context.Context, string) (*relay.User, error) { return &relay.User{ID: 91}, nil },
		createUser: func(context.Context, relay.CreateUserRequest) (*relay.User, error) { return nil, nil },
		createKey: func(context.Context, int64, relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) { return nil, nil },
		revokeKey: func(_ context.Context, keyID int64) error {
			if keyID != 501 {
				t.Fatalf("keyID = %d, want 501", keyID)
			}
			revoked = true
			return nil
		},
	}

	svc := NewService(client, provider, auth.NewRelayIdentityResolver(provider, "corp.example"), "http://relay.example/v1", "team-ai", zap.NewNop())
	stopped, err := svc.Stop(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !revoked {
		t.Fatal("expected relay API key revocation")
	}
	if stopped.Status != session.StatusCompleted {
		t.Fatalf("status = %s, want completed", stopped.Status)
	}
}
```

- [ ] **Step 2: Run the bootstrap tests to capture the missing service/API**

Run: `cd backend && go test ./internal/sessionbootstrap -run 'TestBootstrapCreatesSessionKeyAndEnvBundle|TestStopRevokesRelayKey' -v`

Expected: FAIL because `NewService`, `BootstrapRequest`, and the lifecycle methods do not exist yet.

- [ ] **Step 3: Implement bootstrap orchestration and the new handler contract**

Create `backend/internal/sessionbootstrap/service.go` with:

```go
package sessionbootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type BootstrapRequest struct {
	RepoFullName   string `json:"repo_full_name"`
	BranchSnapshot string `json:"branch_snapshot"`
	HeadSHA        string `json:"head_sha"`
	WorkspaceRoot  string `json:"workspace_root"`
	GitDir         string `json:"git_dir"`
	GitCommonDir   string `json:"git_common_dir"`
	WorkspaceID    string `json:"workspace_id"`
}

type BootstrapResponse struct {
	SessionID          string            `json:"session_id"`
	StartedAt          time.Time         `json:"started_at"`
	RelayUserID        int               `json:"relay_user_id"`
	RelayAPIKeyID      int               `json:"relay_api_key_id"`
	ProviderName       string            `json:"provider_name"`
	GroupID            string            `json:"group_id"`
	RouteBindingSource string            `json:"route_binding_source"`
	RuntimeRef         string            `json:"runtime_ref"`
	EnvBundle          map[string]string `json:"env_bundle"`
	KeyExpiresAt       time.Time         `json:"key_expires_at"`
}

type Service struct {
	entClient             *ent.Client
	relayProvider         relay.Provider
	relayIdentityResolver *auth.RelayIdentityResolver
	relayBaseURL          string
	defaultGroupID        string
	logger                *zap.Logger
}

func NewService(entClient *ent.Client, relayProvider relay.Provider, relayIdentityResolver *auth.RelayIdentityResolver, relayBaseURL, defaultGroupID string, logger *zap.Logger) *Service {
	return &Service{
		entClient:             entClient,
		relayProvider:         relayProvider,
		relayIdentityResolver: relayIdentityResolver,
		relayBaseURL:          relayBaseURL,
		defaultGroupID:        defaultGroupID,
		logger:                logger,
	}
}

func (s *Service) Bootstrap(ctx context.Context, userID int, req BootstrapRequest) (*BootstrapResponse, error) {
	repo, err := s.entClient.RepoConfig.Query().Where(repoconfig.Or(repoconfig.FullNameEQ(req.RepoFullName), repoconfig.CloneURLEQ(req.RepoFullName))).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap repo: %w", err)
	}

	localUser, err := s.entClient.User.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("bootstrap user: %w", err)
	}

	relayUser, err := s.relayIdentityResolver.ResolveOrProvision(ctx, localUser.Username, localUser.Email)
	if err != nil {
		return nil, fmt.Errorf("bootstrap relay identity: %w", err)
	}
	if relayUser == nil {
		return nil, fmt.Errorf("bootstrap relay identity: no relay user resolved")
	}

	providerName, groupID, bindingSource, err := s.resolveRouteBinding(repo)
	if err != nil {
		return nil, err
	}

	sessionID := uuid.New()
	keyExpiresAt := time.Now().Add(24 * time.Hour).UTC()
	apiKey, err := s.relayProvider.CreateUserAPIKey(ctx, relayUser.ID, relay.APIKeyCreateRequest{
		Name:      "ae-session-" + sessionID.String()[:8],
		GroupID:   groupID,
		ExpiresAt: keyExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap api key: %w", err)
	}

	runtimeRef := "runtime/" + sessionID.String()
	envBundle := map[string]string{
		"AE_SESSION_ID":        sessionID.String(),
		"AE_RUNTIME_REF":       runtimeRef,
		"AE_RELAY_API_KEY_ID":  fmt.Sprintf("%d", apiKey.ID),
		"AE_PROVIDER_NAME":     providerName,
		"AE_ENV_VERSION":       "1",
		"OPENAI_API_KEY":       apiKey.Secret,
		"OPENAI_BASE_URL":      strings.TrimRight(s.relayBaseURL, "/"),
		"ANTHROPIC_API_KEY":    apiKey.Secret,
		"ANTHROPIC_BASE_URL":   strings.TrimRight(s.relayBaseURL, "/"),
	}

	record, err := s.entClient.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repo.ID).
		SetUserID(localUser.ID).
		SetBranch(req.BranchSnapshot).
		SetRelayUserID(int(relayUser.ID)).
		SetRelayAPIKeyID(int(apiKey.ID)).
		SetProviderName(providerName).
		SetRuntimeRef(runtimeRef).
		SetInitialWorkspaceRoot(req.WorkspaceRoot).
		SetInitialGitDir(req.GitDir).
		SetInitialGitCommonDir(req.GitCommonDir).
		SetHeadSHAAtStart(req.HeadSHA).
		SetLastSeenAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap session create: %w", err)
	}

	return &BootstrapResponse{
		SessionID:          record.ID.String(),
		StartedAt:          record.StartedAt,
		RelayUserID:        int(relayUser.ID),
		RelayAPIKeyID:      int(apiKey.ID),
		ProviderName:       providerName,
		GroupID:            groupID,
		RouteBindingSource: bindingSource,
		RuntimeRef:         runtimeRef,
		EnvBundle:          envBundle,
		KeyExpiresAt:       keyExpiresAt,
	}, nil
}

func (s *Service) resolveRouteBinding(repo *ent.RepoConfig) (string, string, string, error) {
	providerName := s.relayProvider.Name()
	if repo.RelayProviderName != nil && *repo.RelayProviderName != "" && *repo.RelayProviderName != providerName {
		return "", "", "", fmt.Errorf("repo relay_provider_name %q does not match configured provider %q", *repo.RelayProviderName, providerName)
	}
	if repo.RelayGroupID != nil && *repo.RelayGroupID != "" {
		return providerName, *repo.RelayGroupID, "repo_config", nil
	}
	if s.defaultGroupID != "" {
		return providerName, s.defaultGroupID, "default", nil
	}
	return "", "", "", fmt.Errorf("no relay group resolved for repo %s", repo.FullName)
}

func (s *Service) Heartbeat(ctx context.Context, id uuid.UUID) (*ent.Session, error) {
	return s.entClient.Session.UpdateOneID(id).SetLastSeenAt(time.Now()).Save(ctx)
}

func (s *Service) Stop(ctx context.Context, id uuid.UUID) (*ent.Session, error) {
	current, err := s.entClient.Session.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.RelayAPIKeyID != nil {
		_ = s.relayProvider.RevokeUserAPIKey(ctx, int64(*current.RelayAPIKeyID))
	}
	now := time.Now()
	return s.entClient.Session.UpdateOneID(id).
		SetEndedAt(now).
		SetStatus(session.StatusCompleted).
		SetLastSeenAt(now).
		Save(ctx)
}
```

Update `backend/internal/handler/session.go` to add:

```go
type bootstrapSessionRequest struct {
	RepoFullName   string `json:"repo_full_name" binding:"required"`
	BranchSnapshot string `json:"branch_snapshot" binding:"required"`
	HeadSHA        string `json:"head_sha" binding:"required"`
	WorkspaceRoot  string `json:"workspace_root" binding:"required"`
	GitDir         string `json:"git_dir" binding:"required"`
	GitCommonDir   string `json:"git_common_dir" binding:"required"`
	WorkspaceID    string `json:"workspace_id" binding:"required"`
}

func (h *SessionHandler) Bootstrap(c *gin.Context) {
	var req bootstrapSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	resp, err := h.bootstrapService.Bootstrap(c.Request.Context(), userID.(int), sessionbootstrap.BootstrapRequest{
		RepoFullName:   req.RepoFullName,
		BranchSnapshot: req.BranchSnapshot,
		HeadSHA:        req.HeadSHA,
		WorkspaceRoot:  req.WorkspaceRoot,
		GitDir:         req.GitDir,
		GitCommonDir:   req.GitCommonDir,
		WorkspaceID:    req.WorkspaceID,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, resp)
}
```

- [ ] **Step 4: Wire heartbeat/stop/main/router through the new service**

Update the remaining integration points:

```go
// backend/internal/handler/session.go
func (h *SessionHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	s, err := h.bootstrapService.Heartbeat(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, s)
}

func (h *SessionHandler) Stop(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	s, err := h.bootstrapService.Stop(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, s)
}

// backend/internal/handler/router.go
sessionGroup.POST("/bootstrap", sessionHandler.Bootstrap)

// backend/cmd/server/main.go
relayIdentityResolver := auth.NewRelayIdentityResolver(relayProvider, "corp.example")
authService.SetRelayIdentityResolver(relayIdentityResolver)
bootstrapService := sessionbootstrap.NewService(entClient, relayProvider, relayIdentityResolver, cfg.Relay.URL, cfg.Relay.DefaultGroupID, logger)
sessionHandler := NewSessionHandler(entClient, bootstrapService)
```

- [ ] **Step 5: Run the bootstrap tests again**

Run: `cd backend && go test ./internal/sessionbootstrap ./internal/handler -run 'TestBootstrapCreatesSessionKeyAndEnvBundle|TestStopRevokesRelayKey' -v`

Expected: PASS for the lifecycle service and handler wiring.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/sessionbootstrap/service.go backend/internal/sessionbootstrap/service_test.go backend/internal/handler/session.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add session bootstrap lifecycle"
```

### Task 4: ae-cli Bootstrap Client, Workspace Marker, And Runtime Bundle

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Create: `ae-cli/internal/session/workspace.go`
- Create: `ae-cli/internal/session/workspace_test.go`
- Create: `ae-cli/internal/session/runtime.go`
- Create: `ae-cli/internal/session/runtime_test.go`
- Modify: `ae-cli/internal/session/manager.go`
- Modify: `ae-cli/internal/session/session_test.go`
- Modify: `ae-cli/cmd/start.go`
- Modify: `ae-cli/cmd/stop.go`
- Modify: `ae-cli/cmd/shell.go`
- Modify: `ae-cli/cmd/run.go`
- Modify: `ae-cli/internal/dispatcher/dispatcher.go`

- [ ] **Step 1: Write failing ae-cli bootstrap and workspace-state tests**

Create `ae-cli/internal/session/workspace_test.go` with:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveWorkspaceIDUsesCanonicalGitContext(t *testing.T) {
	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repo")
	worktreeRoot := filepath.Join(tmp, "repo", "worktrees", "feature")
	gitDir := filepath.Join(worktreeRoot, ".git")
	gitCommonDir := filepath.Join(repoRoot, ".git")
	if err := os.MkdirAll(gitCommonDir, 0o755); err != nil {
		t.Fatal(err)
	}

	id1, err := deriveWorkspaceID(repoRoot, worktreeRoot, gitDir, gitCommonDir)
	if err != nil {
		t.Fatalf("deriveWorkspaceID() error = %v", err)
	}
	id2, err := deriveWorkspaceID(repoRoot, worktreeRoot, gitDir, gitCommonDir)
	if err != nil {
		t.Fatalf("deriveWorkspaceID() error = %v", err)
	}
	if id1 != id2 {
		t.Fatalf("workspace IDs differ: %q vs %q", id1, id2)
	}
}

func TestCurrentPrefersWorkspaceMarkerOverLegacyState(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpHome, ".ae-cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpHome, ".ae-cli", "current-session.json"), []byte(`{"id":"legacy-sess","repo":"legacy","branch":"main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarker(workspaceRoot, Marker{SessionID: "marker-sess", WorkspaceID: "ws-1", RuntimeRef: "runtime/marker-sess", ProviderName: "sub2api", RelayAPIKeyID: 501}); err != nil {
		t.Fatal(err)
	}

	state, err := ResolveBoundState(workspaceRoot)
	if err != nil {
		t.Fatalf("ResolveBoundState() error = %v", err)
	}
	if state.SessionID != "marker-sess" {
		t.Fatalf("SessionID = %q, want marker-sess", state.SessionID)
	}
}
```

Create `ae-cli/internal/session/runtime_test.go` with:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRuntimeBundleUsesRestrictedPermissions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	err := WriteRuntimeBundle("sess-1", RuntimeBundle{
		Env: map[string]string{
			"AE_SESSION_ID": "sess-1",
			"OPENAI_API_KEY": "secret",
		},
	})
	if err != nil {
		t.Fatalf("WriteRuntimeBundle() error = %v", err)
	}

	dir := filepath.Join(tmpHome, ".ae-cli", "runtime", "sess-1")
	if mode := mustMode(t, dir); mode != 0o700 {
		t.Fatalf("dir mode = %#o, want 0700", mode)
	}
	if mode := mustMode(t, filepath.Join(dir, "env.json")); mode != 0o600 {
		t.Fatalf("env mode = %#o, want 0600", mode)
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	return info.Mode().Perm()
}
```

Append this targeted test to `ae-cli/internal/client/client_test.go`:

```go
func TestBootstrapSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions/bootstrap" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"session_id": "sess-1",
				"started_at": "2026-03-27T09:00:00Z",
				"relay_user_id": 91,
				"relay_api_key_id": 501,
				"provider_name": "sub2api",
				"group_id": "team-ai",
				"route_binding_source": "repo_config",
				"runtime_ref": "runtime/sess-1",
				"env_bundle": map[string]any{"AE_SESSION_ID": "sess-1"},
				"key_expires_at": "2026-03-28T09:00:00Z",
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	resp, err := c.BootstrapSession(context.Background(), BootstrapSessionRequest{
		RepoFullName: "org/ai-efficiency",
		BranchSnapshot: "feat/test",
		HeadSHA: "abc123",
		WorkspaceRoot: "/tmp/repo",
		GitDir: "/tmp/repo/.git",
		GitCommonDir: "/tmp/repo/.git",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("BootstrapSession() error = %v", err)
	}
	if resp.SessionID != "sess-1" || resp.RuntimeRef != "runtime/sess-1" {
		t.Fatalf("BootstrapSession() = %+v", resp)
	}
}
```

- [ ] **Step 2: Run the ae-cli workspace/client tests to confirm the current state model is insufficient**

Run: `cd ae-cli && go test ./internal/client ./internal/session -run 'TestBootstrapSession|TestDeriveWorkspaceIDUsesCanonicalGitContext|TestCurrentPrefersWorkspaceMarkerOverLegacyState|TestWriteRuntimeBundleUsesRestrictedPermissions' -v`

Expected: FAIL because the bootstrap client request/response types, runtime helpers, and workspace marker logic do not exist.

- [ ] **Step 3: Add workspace derivation, marker I/O, runtime bundle helpers, and the bootstrap client**

Create `ae-cli/internal/session/workspace.go` with:

```go
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Marker struct {
	SessionID     string `json:"session_id"`
	WorkspaceID   string `json:"workspace_id"`
	RuntimeRef    string `json:"runtime_ref"`
	ProviderName  string `json:"provider_name"`
	RelayAPIKeyID int    `json:"relay_api_key_id"`
	CreatedAt     string `json:"created_at"`
	LastSeenAt    string `json:"last_seen_at"`
}

func deriveWorkspaceID(repoRoot, workspaceRoot, gitDir, gitCommonDir string) (string, error) {
	canonical := []string{repoRoot, workspaceRoot, gitDir, gitCommonDir}
	for i, p := range canonical {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			canonical[i] = resolved
		}
		abs, err := filepath.Abs(canonical[i])
		if err != nil {
			return "", err
		}
		canonical[i] = filepath.Clean(abs)
	}

	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("ae-workspace\x1f"+canonical[0]+"\x1f"+canonical[1]+"\x1f"+canonical[2]+"\x1f"+canonical[3])).String(), nil
}

func markerPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".ae", "session.json")
}

func WriteMarker(workspaceRoot string, marker Marker) error {
	path := markerPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func ReadMarker(workspaceRoot string) (*Marker, error) {
	data, err := os.ReadFile(markerPath(workspaceRoot))
	if err != nil {
		return nil, err
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, err
	}
	return &marker, nil
}

type BoundState struct {
	SessionID     string
	WorkspaceID   string
	RuntimeRef    string
	ProviderName  string
	RelayAPIKeyID int
}

func ResolveBoundState(workspaceRoot string) (*BoundState, error) {
	if marker, err := ReadMarker(workspaceRoot); err == nil {
		return &BoundState{
			SessionID:     marker.SessionID,
			WorkspaceID:   marker.WorkspaceID,
			RuntimeRef:    marker.RuntimeRef,
			ProviderName:  marker.ProviderName,
			RelayAPIKeyID: marker.RelayAPIKeyID,
		}, nil
	}
	path, err := stateFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var legacy State
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	return &BoundState{SessionID: legacy.ID}, nil
}
```

Create `ae-cli/internal/session/runtime.go` with:

```go
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type RuntimeBundle struct {
	Env map[string]string `json:"env"`
}

func runtimeDir(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ae-cli", "runtime", sessionID), nil
}

func WriteRuntimeBundle(sessionID string, bundle RuntimeBundle) error {
	dir, err := runtimeDir(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "collectors"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "queue"), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "env.json"), data, 0o600)
}

func ReadRuntimeBundle(sessionID string) (*RuntimeBundle, error) {
	dir, err := runtimeDir(sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "env.json"))
	if err != nil {
		return nil, err
	}
	var bundle RuntimeBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("parse runtime bundle: %w", err)
	}
	return &bundle, nil
}

func RuntimeCollectorsDir(sessionID string) (string, error) {
	dir, err := runtimeDir(sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "collectors"), nil
}
```

Extend `ae-cli/internal/client/client.go` with:

```go
type BootstrapSessionRequest struct {
	RepoFullName   string `json:"repo_full_name"`
	BranchSnapshot string `json:"branch_snapshot"`
	HeadSHA        string `json:"head_sha"`
	WorkspaceRoot  string `json:"workspace_root"`
	GitDir         string `json:"git_dir"`
	GitCommonDir   string `json:"git_common_dir"`
	WorkspaceID    string `json:"workspace_id"`
}

type BootstrapSessionResponse struct {
	SessionID          string            `json:"session_id"`
	StartedAt          time.Time         `json:"started_at"`
	RelayUserID        int               `json:"relay_user_id"`
	RelayAPIKeyID      int               `json:"relay_api_key_id"`
	ProviderName       string            `json:"provider_name"`
	GroupID            string            `json:"group_id"`
	RouteBindingSource string            `json:"route_binding_source"`
	RuntimeRef         string            `json:"runtime_ref"`
	EnvBundle          map[string]string `json:"env_bundle"`
	KeyExpiresAt       time.Time         `json:"key_expires_at"`
}

func (c *Client) BootstrapSession(ctx context.Context, req BootstrapSessionRequest) (*BootstrapSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions/bootstrap", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create bootstrap request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send bootstrap request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	var envelope struct {
		Data BootstrapSessionResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode bootstrap response: %w", err)
	}
	return &envelope.Data, nil
}
```

- [ ] **Step 4: Move ae-cli session lifecycle onto bootstrap + marker/runtime state**

Update `ae-cli/internal/session/manager.go`, `ae-cli/cmd/start.go`, `ae-cli/cmd/stop.go`, `ae-cli/cmd/shell.go`, `ae-cli/cmd/run.go`, and `ae-cli/internal/dispatcher/dispatcher.go` so start/stop/read flows use the new state model:

```go
func (m *Manager) Start() (*State, error) {
	gitCtx, err := detectGitContext()
	if err != nil {
		return nil, fmt.Errorf("detect git context: %w", err)
	}

	workspaceID, err := deriveWorkspaceID(gitCtx.RepoRoot, gitCtx.WorkspaceRoot, gitCtx.GitDir, gitCtx.GitCommonDir)
	if err != nil {
		return nil, fmt.Errorf("derive workspace id: %w", err)
	}

	resp, err := m.client.BootstrapSession(context.Background(), client.BootstrapSessionRequest{
		RepoFullName:   gitCtx.RemoteURL,
		BranchSnapshot: gitCtx.Branch,
		HeadSHA:        gitCtx.HeadSHA,
		WorkspaceRoot:  gitCtx.WorkspaceRoot,
		GitDir:         gitCtx.GitDir,
		GitCommonDir:   gitCtx.GitCommonDir,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap session: %w", err)
	}

	if err := ensureInfoExclude(gitCtx.GitDir); err != nil {
		return nil, fmt.Errorf("ensure info/exclude: %w", err)
	}
	if err := WriteRuntimeBundle(resp.SessionID, RuntimeBundle{Env: resp.EnvBundle}); err != nil {
		return nil, fmt.Errorf("write runtime bundle: %w", err)
	}
	if err := WriteMarker(gitCtx.WorkspaceRoot, Marker{
		SessionID:     resp.SessionID,
		WorkspaceID:   workspaceID,
		RuntimeRef:    resp.RuntimeRef,
		ProviderName:  resp.ProviderName,
		RelayAPIKeyID: resp.RelayAPIKeyID,
		CreatedAt:     resp.StartedAt.Format(time.RFC3339),
		LastSeenAt:    resp.StartedAt.Format(time.RFC3339),
	}); err != nil {
		return nil, fmt.Errorf("write marker: %w", err)
	}

	return &State{ID: resp.SessionID, Repo: gitCtx.RemoteURL, Branch: gitCtx.Branch, StartedAt: resp.StartedAt}, nil
}

type GitContext struct {
	RepoRoot      string
	WorkspaceRoot string
	GitDir        string
	GitCommonDir  string
	RemoteURL     string
	Branch        string
	HeadSHA       string
	ParentSHAs    []string
}

func detectGitContext() (*GitContext, error) {
	repoRoot := strings.TrimSpace(string(mustGit("git", "rev-parse", "--show-toplevel")))
	gitDir := strings.TrimSpace(string(mustGit("git", "rev-parse", "--git-dir")))
	gitCommonDir := strings.TrimSpace(string(mustGit("git", "rev-parse", "--git-common-dir")))
	headSHA := strings.TrimSpace(string(mustGit("git", "rev-parse", "HEAD")))
	branch := strings.TrimSpace(string(mustGit("git", "symbolic-ref", "--short", "-q", "HEAD")))
	remoteURL := strings.TrimSpace(string(mustGit("git", "remote", "get-url", "origin")))
	parentLine := strings.TrimSpace(string(mustGit("git", "rev-list", "--parents", "-n", "1", "HEAD")))
	parts := strings.Fields(parentLine)
	parentSHAs := []string{}
	if len(parts) > 1 {
		parentSHAs = parts[1:]
	}
	return &GitContext{
		RepoRoot:      repoRoot,
		WorkspaceRoot: repoRoot,
		GitDir:        gitDir,
		GitCommonDir:  gitCommonDir,
		RemoteURL:     remoteURL,
		Branch:        branch,
		HeadSHA:       headSHA,
		ParentSHAs:    parentSHAs,
	}, nil
}

func ensureInfoExclude(gitDir string) error {
	excludePath := strings.TrimSpace(string(mustGit("git", "rev-parse", "--git-path", "info/exclude")))
	data, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(data), "/.ae/") {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString("/.ae/\n")
	return err
}

func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for key, value := range env {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

func mustGit(name string, args ...string) []byte {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		panic(err)
	}
	return out
}

// cmd/start.go
tokenPath, _ := auth.DefaultTokenPath()
	token, err := auth.ReadToken(tokenPath)
	if err != nil || !token.IsValid() {
		return fmt.Errorf("login required: run 'ae-cli login' first")
	}

// cmd/shell.go and internal/dispatcher/dispatcher.go
bundle, err := session.ReadRuntimeBundle(state.ID)
if err != nil {
	return fmt.Errorf("load runtime bundle: %w", err)
}
cmd.Env = append(os.Environ(), envPairs(bundle.Env)...)
```

- [ ] **Step 5: Run the ae-cli lifecycle tests again**

Run: `cd ae-cli && go test ./internal/client ./internal/session ./internal/dispatcher -run 'TestBootstrapSession|TestDeriveWorkspaceIDUsesCanonicalGitContext|TestCurrentPrefersWorkspaceMarkerOverLegacyState|TestWriteRuntimeBundleUsesRestrictedPermissions' -v`

Expected: PASS for the bootstrap client, workspace derivation, and runtime bundle behavior.

- [ ] **Step 6: Commit**

```bash
git add ae-cli/internal/client/client.go ae-cli/internal/client/client_test.go ae-cli/internal/session/workspace.go ae-cli/internal/session/workspace_test.go ae-cli/internal/session/runtime.go ae-cli/internal/session/runtime_test.go ae-cli/internal/session/manager.go ae-cli/internal/session/session_test.go ae-cli/cmd/start.go ae-cli/cmd/stop.go ae-cli/cmd/shell.go ae-cli/cmd/run.go ae-cli/internal/dispatcher/dispatcher.go
git commit -m "feat(ae-cli): bootstrap sessions into workspace markers and runtime bundles"
```

### Task 5: Shared Hooks, Env Bootstrap, And Local Retry Queue

**Files:**
- Create: `ae-cli/internal/hooks/install.go`
- Create: `ae-cli/internal/hooks/install_test.go`
- Create: `ae-cli/internal/hooks/handler.go`
- Create: `ae-cli/internal/hooks/handler_test.go`
- Create: `ae-cli/internal/hooks/queue.go`
- Create: `ae-cli/internal/hooks/queue_test.go`
- Create: `ae-cli/cmd/hook.go`
- Create: `ae-cli/cmd/flush.go`
- Modify: `ae-cli/cmd/start.go`

- [ ] **Step 1: Write failing hook-install and fail-open queue tests**

Create `ae-cli/internal/hooks/install_test.go` with:

```go
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSharedHooksChainsExistingLegacyHook(t *testing.T) {
	commonDir := t.TempDir()
	legacyDir := filepath.Join(commonDir, "legacy-hooks")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "post-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(commonDir, "/usr/local/bin/ae-cli", legacyDir, filepath.Join(commonDir, ".git", "hooks")); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(commonDir, "ae-hooks", "post-commit"))
	if err != nil {
		t.Fatalf("read generated hook: %v", err)
	}
	if !strings.Contains(string(data), legacyDir+"/post-commit") {
		t.Fatalf("generated hook did not chain legacy path: %s", string(data))
	}
}

func TestInstallSharedHooksRejectsRecursiveLegacyPath(t *testing.T) {
	commonDir := t.TempDir()
	if err := Install(commonDir, "/usr/local/bin/ae-cli", filepath.Join(commonDir, "ae-hooks"), filepath.Join(commonDir, ".git", "hooks")); err == nil {
		t.Fatal("expected Install() to reject recursive legacy hook path")
	}
}
```

Create `ae-cli/internal/hooks/handler_test.go` with:

```go
package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type recordingSender struct {
	commitCalls  int
	rewriteCalls int
}

func (s *recordingSender) SendCommitCheckpoint(context.Context, CommitCheckpointPayload) error {
	s.commitCalls++
	return nil
}

func (s *recordingSender) SendCommitRewrite(context.Context, CommitRewritePayload) error {
	s.rewriteCalls++
	return nil
}

type failingSender struct{}

func (failingSender) SendCommitCheckpoint(context.Context, CommitCheckpointPayload) error { return errors.New("backend down") }
func (failingSender) SendCommitRewrite(context.Context, CommitRewritePayload) error      { return errors.New("backend down") }

func TestPostCommitBootstrapsMarkerFromEnv(t *testing.T) {
	workspaceRoot := t.TempDir()
	handler := NewHandler(&recordingSender{}, filepath.Join(t.TempDir(), "queue"))
	t.Setenv("AE_SESSION_ID", "sess-1")
	t.Setenv("AE_RUNTIME_REF", "runtime/sess-1")
	t.Setenv("AE_PROVIDER_NAME", "sub2api")
	t.Setenv("AE_RELAY_API_KEY_ID", "501")

	if err := handler.writeMarkerFromEnv(workspaceRoot, "ws-1"); err != nil {
		t.Fatalf("writeMarkerFromEnv() error = %v", err)
	}
	marker, err := session.ReadMarker(workspaceRoot)
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if marker.SessionID != "sess-1" || marker.RuntimeRef != "runtime/sess-1" {
		t.Fatalf("marker = %+v", marker)
	}
}

func TestPostCommitQueuesEventWhenUploadFails(t *testing.T) {
	queueDir := filepath.Join(t.TempDir(), "queue")
	handler := NewHandler(failingSender{}, queueDir)
	if err := handler.enqueueCheckpoint(CommitCheckpointPayload{EventID: "cp-1", CommitSHA: "abc123"}); err != nil {
		t.Fatalf("enqueueCheckpoint() error = %v", err)
	}
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("queue file count = %d, want 1", len(entries))
	}
}

func TestFlushReplaysQueuedEvents(t *testing.T) {
	queueDir := filepath.Join(t.TempDir(), "queue")
	if err := os.MkdirAll(queueDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"kind":"checkpoint","enqueued_at":"2026-03-27T09:00:00Z","payload":{"event_id":"cp-1","commit_sha":"abc123"}}`)
	if err := os.WriteFile(filepath.Join(queueDir, "0001.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	if err := Flush(context.Background(), queueDir, sender); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if sender.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", sender.commitCalls)
	}
}
```

- [ ] **Step 2: Run the hook tests to verify the shared-hook path does not exist yet**

Run: `cd ae-cli && go test ./internal/hooks -run 'TestInstallSharedHooks|TestPostCommit|TestFlush' -v`

Expected: FAIL because the hooks package, queue logic, and hidden CLI commands do not exist.

- [ ] **Step 3: Implement shared hook installation with safe legacy chaining**

Create `ae-cli/internal/hooks/install.go` with:

```go
package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Install(commonDir, aeCLIPath, legacyHooksPath, defaultHooksDir string) error {
	hooksDir := filepath.Join(commonDir, "ae-hooks")
	if legacyHooksPath == hooksDir {
		return fmt.Errorf("refusing to install hooks recursively into %s", hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}

	postCommit := fmt.Sprintf("#!/bin/sh\nset -eu\n\"%s\" hook post-commit \"$@\"\nif [ -n \"%s\" ] && [ -x \"%s/post-commit\" ]; then\n  \"%s/post-commit\" \"$@\"\nfi\n", aeCLIPath, legacyHooksPath, legacyHooksPath, legacyHooksPath)
	postRewrite := fmt.Sprintf("#!/bin/sh\nset -eu\n\"%s\" hook post-rewrite \"$@\"\nif [ -n \"%s\" ] && [ -x \"%s/post-rewrite\" ]; then\n  \"%s/post-rewrite\" \"$@\"\nfi\n", aeCLIPath, legacyHooksPath, legacyHooksPath, legacyHooksPath)

	if err := os.WriteFile(filepath.Join(hooksDir, "post-commit"), []byte(postCommit), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "post-rewrite"), []byte(postRewrite), 0o755); err != nil {
		return err
	}
	return setCoreHooksPath(commonDir, hooksDir)
}

func setCoreHooksPath(commonDir, hooksDir string) error {
	return exec.Command("git", "--git-dir", commonDir, "config", "core.hooksPath", hooksDir).Run()
}
```

Create `ae-cli/internal/hooks/queue.go` with:

```go
type QueuedEvent struct {
	Kind       string          `json:"kind"`
	EnqueuedAt time.Time       `json:"enqueued_at"`
	Payload    json.RawMessage `json:"payload"`
}

func Enqueue(queueDir string, event QueuedEvent) error {
	if err := os.MkdirAll(queueDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	name := filepath.Join(queueDir, fmt.Sprintf("%d.json", event.EnqueuedAt.UnixNano()))
	return os.WriteFile(name, data, 0o600)
}

func Flush(ctx context.Context, queueDir string, sender Sender) error {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(queueDir, entry.Name()))
		if err != nil {
			return err
		}
		var event QueuedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		switch event.Kind {
		case "checkpoint":
			var payload CommitCheckpointPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			if err := sender.SendCommitCheckpoint(ctx, payload); err != nil {
				return err
			}
		case "rewrite":
			var payload CommitRewritePayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			if err := sender.SendCommitRewrite(ctx, payload); err != nil {
				return err
			}
		}
		if err := os.Remove(filepath.Join(queueDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement hook handlers and hidden `hook` / `flush` commands**

Create `ae-cli/internal/hooks/handler.go` and `ae-cli/cmd/hook.go` / `ae-cli/cmd/flush.go` with:

```go
type CommitCheckpointPayload struct {
	EventID        string    `json:"event_id"`
	RepoFullName   string    `json:"repo_full_name"`
	SessionID      string    `json:"session_id,omitempty"`
	WorkspaceID    string    `json:"workspace_id"`
	BindingSource  string    `json:"binding_source"`
	CommitSHA      string    `json:"commit_sha"`
	ParentSHAs     []string  `json:"parent_shas"`
	BranchSnapshot string    `json:"branch_snapshot"`
	HeadSnapshot   string    `json:"head_snapshot"`
	AgentSnapshot  any       `json:"agent_snapshot,omitempty"`
	CapturedAt     time.Time `json:"captured_at"`
}

type CommitRewritePayload struct {
	EventID       string    `json:"event_id"`
	RepoFullName  string    `json:"repo_full_name"`
	SessionID     string    `json:"session_id,omitempty"`
	WorkspaceID   string    `json:"workspace_id"`
	BindingSource string    `json:"binding_source"`
	RewriteType   string    `json:"rewrite_type"`
	OldCommitSHA  string    `json:"old_commit_sha"`
	NewCommitSHA  string    `json:"new_commit_sha"`
	CapturedAt    time.Time `json:"captured_at"`
}

type Sender interface {
	SendCommitCheckpoint(context.Context, CommitCheckpointPayload) error
	SendCommitRewrite(context.Context, CommitRewritePayload) error
}

type Handler struct {
	sender   Sender
	queueDir string
}

func NewHandler(sender Sender, queueDir string) *Handler {
	return &Handler{sender: sender, queueDir: queueDir}
}

func resolveBoundState(workspaceRoot string) (*session.BoundState, string, error) {
	state, err := session.ResolveBoundState(workspaceRoot)
	if err != nil {
		return nil, "", err
	}
	return state, "marker", nil
}

func checkpointEventID(repoConfigHint, commitSHA string) string {
	sum := sha256.Sum256([]byte("checkpoint" + repoConfigHint + commitSHA))
	return hex.EncodeToString(sum[:])
}

func rewriteEventID(repoConfigHint, oldSHA, newSHA, rewriteType string) string {
	sum := sha256.Sum256([]byte("rewrite" + repoConfigHint + oldSHA + newSHA + rewriteType))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) HandlePostCommit(ctx context.Context) error {
	gitCtx, err := detectGitContext()
	if err != nil {
		return nil // fail-open
	}

	state, bindingSource, err := resolveBoundState(gitCtx.WorkspaceRoot)
	if err != nil {
		return nil // fail-open
	}

	if bindingSource == "env_bootstrap" {
		if err := ensureInfoExclude(gitCtx.GitDir); err == nil {
			_ = WriteMarker(gitCtx.WorkspaceRoot, Marker{
				SessionID:     state.SessionID,
				WorkspaceID:   state.WorkspaceID,
				RuntimeRef:    state.RuntimeRef,
				ProviderName:  state.ProviderName,
				RelayAPIKeyID: state.RelayAPIKeyID,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339),
				LastSeenAt:    time.Now().UTC().Format(time.RFC3339),
			})
		}
	}

	payload := CommitCheckpointPayload{
		EventID:        checkpointEventID(gitCtx.RemoteURL, gitCtx.HeadSHA),
		RepoFullName:   gitCtx.RemoteURL,
		SessionID:      state.SessionID,
		WorkspaceID:    state.WorkspaceID,
		BindingSource:  bindingSource,
		CommitSHA:      gitCtx.HeadSHA,
		ParentSHAs:     gitCtx.ParentSHAs,
		BranchSnapshot: gitCtx.Branch,
		HeadSnapshot:   gitCtx.HeadSHA,
		CapturedAt:     time.Now().UTC(),
	}

	if err := h.sender.SendCommitCheckpoint(ctx, payload); err != nil {
		raw, _ := json.Marshal(payload)
		_ = Enqueue(h.queueDir, QueuedEvent{Kind: "checkpoint", EnqueuedAt: time.Now().UTC(), Payload: raw})
	}
	return nil
}

func (h *Handler) writeMarkerFromEnv(workspaceRoot, workspaceID string) error {
	keyID, _ := strconv.Atoi(os.Getenv("AE_RELAY_API_KEY_ID"))
	return session.WriteMarker(workspaceRoot, session.Marker{
		SessionID:     os.Getenv("AE_SESSION_ID"),
		WorkspaceID:   workspaceID,
		RuntimeRef:    os.Getenv("AE_RUNTIME_REF"),
		ProviderName:  os.Getenv("AE_PROVIDER_NAME"),
		RelayAPIKeyID: keyID,
	})
}

func (h *Handler) enqueueCheckpoint(payload CommitCheckpointPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return Enqueue(h.queueDir, QueuedEvent{Kind: "checkpoint", EnqueuedAt: time.Now().UTC(), Payload: raw})
}
```

Update `ae-cli/cmd/start.go` to call hook installation immediately after a successful bootstrap:

```go
selfPath, _ := os.Executable()
if err := hooks.Install(gitCtx.GitCommonDir, selfPath, "", filepath.Join(gitCtx.GitCommonDir, "hooks")); err != nil {
	return fmt.Errorf("install hooks: %w", err)
}
```

- [ ] **Step 5: Run the hook tests again**

Run: `cd ae-cli && go test ./internal/hooks -run 'TestInstallSharedHooks|TestPostCommit|TestFlush' -v`

Expected: PASS, including the fail-open queue behavior.

- [ ] **Step 6: Commit**

```bash
git add ae-cli/internal/hooks/install.go ae-cli/internal/hooks/install_test.go ae-cli/internal/hooks/handler.go ae-cli/internal/hooks/handler_test.go ae-cli/internal/hooks/queue.go ae-cli/internal/hooks/queue_test.go ae-cli/cmd/hook.go ae-cli/cmd/flush.go ae-cli/cmd/start.go
git commit -m "feat(ae-cli): add shared hook capture and retry queue"
```

### Task 6: Local Collector Snapshots For Codex, Claude, And Kiro

**Files:**
- Create: `ae-cli/internal/collector/types.go`
- Create: `ae-cli/internal/collector/collector.go`
- Create: `ae-cli/internal/collector/codex.go`
- Create: `ae-cli/internal/collector/claude.go`
- Create: `ae-cli/internal/collector/kiro.go`
- Create: `ae-cli/internal/collector/collector_test.go`
- Create: `ae-cli/internal/collector/testdata/codex-session.jsonl`
- Create: `ae-cli/internal/collector/testdata/claude-session.jsonl`
- Create: `ae-cli/internal/collector/testdata/kiro-session.json`
- Modify: `ae-cli/internal/hooks/handler.go`

- [ ] **Step 1: Add real-shape collector fixtures and failing parser tests**

Create the fixture files with these contents:

```json
// ae-cli/internal/collector/testdata/kiro-session.json
{
  "session_id": "kiro-sess-1",
  "cwd": "/tmp/repo",
  "session_state": {
    "rts_model_state": {
      "conversation_id": "conv-kiro-1",
      "context_usage_percentage": 47.5
    }
  }
}
```

```jsonl
// ae-cli/internal/collector/testdata/codex-session.jsonl
{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-sess-1","cwd":"/tmp/repo"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1200,"cached_input_tokens":300,"output_tokens":250,"reasoning_output_tokens":80,"total_tokens":1450}}}}
```

```jsonl
// ae-cli/internal/collector/testdata/claude-session.jsonl
{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-sess-1","message":{"usage":{"input_tokens":500,"output_tokens":120,"cache_creation_input_tokens":40,"cache_read_input_tokens":25}}}
{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-sess-1","message":{"usage":{"input_tokens":600,"output_tokens":140,"cache_creation_input_tokens":10,"cache_read_input_tokens":15}}}
```

Create `ae-cli/internal/collector/collector_test.go` with:

```go
package collector

import "testing"

func TestBuildSnapshotAggregatesCodexClaudeAndKiro(t *testing.T) {
	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:   []string{"testdata/codex-session.jsonl"},
		ClaudeFiles:  []string{"testdata/claude-session.jsonl"},
		KiroFiles:    []string{"testdata/kiro-session.json"},
		WorkspaceRoot: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex.SourceSessionID != "codex-sess-1" || snapshot.Codex.TotalTokens != 1450 {
		t.Fatalf("Codex snapshot = %+v", snapshot.Codex)
	}
	if snapshot.Claude.InputTokens != 1100 || snapshot.Claude.CachedInputTokens != 90 {
		t.Fatalf("Claude snapshot = %+v", snapshot.Claude)
	}
	if snapshot.Kiro.ConversationID != "conv-kiro-1" || snapshot.Kiro.ContextUsagePct != 47.5 {
		t.Fatalf("Kiro snapshot = %+v", snapshot.Kiro)
	}
}
```

- [ ] **Step 2: Run the collector test to confirm the parsers do not exist yet**

Run: `cd ae-cli && go test ./internal/collector -run TestBuildSnapshotAggregatesCodexClaudeAndKiro -v`

Expected: FAIL because the collector package and snapshot types do not exist.

- [ ] **Step 3: Implement the three tool readers and the shared snapshot type**

Create `ae-cli/internal/collector/types.go` with:

```go
package collector

type CodexSnapshot struct {
	SourceSessionID  string         `json:"source_session_id,omitempty"`
	InputTokens      int64          `json:"input_tokens,omitempty"`
	CachedInputTokens int64         `json:"cached_input_tokens,omitempty"`
	OutputTokens     int64          `json:"output_tokens,omitempty"`
	ReasoningTokens  int64          `json:"reasoning_tokens,omitempty"`
	TotalTokens      int64          `json:"total_tokens,omitempty"`
	RawPayload       map[string]any `json:"raw_payload,omitempty"`
}

type ClaudeSnapshot struct {
	SourceSessionID   string         `json:"source_session_id,omitempty"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	CachedInputTokens int64          `json:"cached_input_tokens,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type KiroSnapshot struct {
	ConversationID  string         `json:"conversation_id,omitempty"`
	CreditUsage     float64        `json:"credit_usage,omitempty"`
	ContextUsagePct float64        `json:"context_usage_pct,omitempty"`
	RawPayload      map[string]any `json:"raw_payload,omitempty"`
}

type Snapshot struct {
	Codex *CodexSnapshot  `json:"codex,omitempty"`
	Claude *ClaudeSnapshot `json:"claude,omitempty"`
	Kiro  *KiroSnapshot   `json:"kiro,omitempty"`
}

type Paths struct {
	CodexFiles    []string
	ClaudeFiles   []string
	KiroFiles     []string
	WorkspaceRoot string
}
```

Implement `ae-cli/internal/collector/codex.go`, `claude.go`, and `kiro.go` against the actual observed local file shapes:

```go
// codex.go
func readCodexSnapshot(path, workspaceRoot string) (*CodexSnapshot, error) {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sessionID string
	var snapshot *CodexSnapshot
	for _, line := range strings.Split(strings.TrimSpace(string(lines)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		switch row["type"] {
		case "session_meta":
			payload := row["payload"].(map[string]any)
			if payload["cwd"] == workspaceRoot {
				sessionID = payload["id"].(string)
			}
		case "event_msg":
			payload := row["payload"].(map[string]any)
			if payload["type"] == "token_count" && sessionID != "" {
				usage := payload["info"].(map[string]any)["total_token_usage"].(map[string]any)
				snapshot = &CodexSnapshot{
					SourceSessionID:   sessionID,
					InputTokens:       int64(usage["input_tokens"].(float64)),
					CachedInputTokens: int64(usage["cached_input_tokens"].(float64)),
					OutputTokens:      int64(usage["output_tokens"].(float64)),
					ReasoningTokens:   int64(usage["reasoning_output_tokens"].(float64)),
					TotalTokens:       int64(usage["total_tokens"].(float64)),
					RawPayload:        payload,
				}
			}
		}
	}
	return snapshot, nil
}

// claude.go
func readClaudeSnapshot(path, workspaceRoot string) (*ClaudeSnapshot, error) {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	snapshot := &ClaudeSnapshot{}
	for _, line := range strings.Split(strings.TrimSpace(string(lines)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		if row["type"] != "assistant" || row["cwd"] != workspaceRoot {
			continue
		}
		snapshot.SourceSessionID = row["sessionId"].(string)
		usage := row["message"].(map[string]any)["usage"].(map[string]any)
		snapshot.InputTokens += int64(usage["input_tokens"].(float64))
		snapshot.OutputTokens += int64(usage["output_tokens"].(float64))
		snapshot.CachedInputTokens += int64(usage["cache_creation_input_tokens"].(float64) + usage["cache_read_input_tokens"].(float64))
		snapshot.RawPayload = row
	}
	return snapshot, nil
}

// kiro.go
func readKiroSnapshot(path, workspaceRoot string) (*KiroSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	if row["cwd"] != workspaceRoot {
		return nil, nil
	}
	state := row["session_state"].(map[string]any)["rts_model_state"].(map[string]any)
	snapshot := &KiroSnapshot{
		ConversationID: state["conversation_id"].(string),
		RawPayload:     row,
	}
	if value, ok := state["context_usage_percentage"].(float64); ok {
		snapshot.ContextUsagePct = value
	}
	return snapshot, nil
}
```

- [ ] **Step 4: Integrate snapshot building into hook capture and runtime cache**

Create `ae-cli/internal/collector/collector.go` and update `ae-cli/internal/hooks/handler.go`:

```go
func BuildSnapshot(paths Paths) (*Snapshot, error) {
	codex, err := readCodexSnapshot(paths.CodexFiles[0], paths.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	claude, err := readClaudeSnapshot(paths.ClaudeFiles[0], paths.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	kiro, err := readKiroSnapshot(paths.KiroFiles[0], paths.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		Codex:  codex,
		Claude: claude,
		Kiro:   kiro,
	}, nil
}

func WriteCache(sessionID string, snapshot *Snapshot) error {
	dir, err := session.RuntimeCollectorsDir(sessionID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o600)
}

// hooks/handler.go
snapshot, err := collector.BuildSnapshot(collector.DefaultPaths(gitCtx.WorkspaceRoot))
if err == nil && state.SessionID != "" {
	_ = collector.WriteCache(state.SessionID, snapshot)
	payload.AgentSnapshot = snapshot
}
```

- [ ] **Step 5: Run the collector tests again**

Run: `cd ae-cli && go test ./internal/collector -run TestBuildSnapshotAggregatesCodexClaudeAndKiro -v`

Expected: PASS for the three snapshot readers and the aggregate snapshot builder.

- [ ] **Step 6: Commit**

```bash
git add ae-cli/internal/collector/types.go ae-cli/internal/collector/collector.go ae-cli/internal/collector/codex.go ae-cli/internal/collector/claude.go ae-cli/internal/collector/kiro.go ae-cli/internal/collector/collector_test.go ae-cli/internal/collector/testdata/codex-session.jsonl ae-cli/internal/collector/testdata/claude-session.jsonl ae-cli/internal/collector/testdata/kiro-session.json ae-cli/internal/hooks/handler.go
git commit -m "feat(ae-cli): collect codex claude and kiro snapshots"
```

### Task 7: Backend Checkpoint And Rewrite Ingestion

**Files:**
- Create: `backend/internal/checkpoint/service.go`
- Create: `backend/internal/checkpoint/service_test.go`
- Create: `backend/internal/handler/checkpoint.go`
- Create: `backend/internal/handler/checkpoint_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing checkpoint ingestion tests**

Create `backend/internal/checkpoint/service_test.go` with:

```go
package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
)

func TestRecordCheckpointUpsertsByEventIDAndWritesMetadataEvents(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	svc := NewService(client)
	req := CommitCheckpointRequest{
		EventID:        "cp-1",
		RepoFullName:   "org/ai-efficiency",
		SessionID:      "550e8400-e29b-41d4-a716-446655440000",
		WorkspaceID:    "ws-1",
		BindingSource:  "marker",
		CommitSHA:      "abc123",
		ParentSHAs:     []string{"000000"},
		BranchSnapshot: "feat/attribution",
		HeadSnapshot:   "abc123",
		AgentSnapshot: map[string]any{
			"codex": map[string]any{"source_session_id": "codex-sess-1", "input_tokens": 1200, "total_tokens": 1450},
		},
		CapturedAt: time.Now().UTC(),
	}

	if err := svc.RecordCheckpoint(ctx, req); err != nil {
		t.Fatalf("RecordCheckpoint() error = %v", err)
	}
	if err := svc.RecordCheckpoint(ctx, req); err != nil {
		t.Fatalf("second RecordCheckpoint() error = %v", err)
	}
	if count := client.CommitCheckpoint.Query().CountX(ctx); count != 1 {
		t.Fatalf("checkpoint count = %d, want 1", count)
	}
	if count := client.AgentMetadataEvent.Query().CountX(ctx); count != 1 {
		t.Fatalf("metadata event count = %d, want 1", count)
	}
}

func TestRecordRewriteAcceptsUnboundEvents(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	svc := NewService(client)
	if err := svc.RecordRewrite(ctx, CommitRewriteRequest{
		EventID:       "rw-1",
		RepoFullName:  "org/ai-efficiency",
		WorkspaceID:   "ws-1",
		BindingSource: "unbound",
		RewriteType:   "amend",
		OldCommitSHA:  "old123",
		NewCommitSHA:  "new456",
		CapturedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordRewrite() error = %v", err)
	}
	if count := client.CommitRewrite.Query().CountX(ctx); count != 1 {
		t.Fatalf("rewrite count = %d, want 1", count)
	}
}
```

- [ ] **Step 2: Run the checkpoint ingestion tests to confirm the service is missing**

Run: `cd backend && go test ./internal/checkpoint -run 'TestRecordCheckpointUpsertsByEventIDAndWritesMetadataEvents|TestRecordRewriteAcceptsUnboundEvents' -v`

Expected: FAIL because the checkpoint service and request types do not exist.

- [ ] **Step 3: Implement idempotent checkpoint/rewrite persistence**

Create `backend/internal/checkpoint/service.go` with:

```go
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

type CommitCheckpointRequest struct {
	EventID        string         `json:"event_id"`
	RepoFullName   string         `json:"repo_full_name"`
	SessionID      string         `json:"session_id,omitempty"`
	WorkspaceID    string         `json:"workspace_id"`
	BindingSource  string         `json:"binding_source"`
	CommitSHA      string         `json:"commit_sha"`
	ParentSHAs     []string       `json:"parent_shas"`
	BranchSnapshot string         `json:"branch_snapshot,omitempty"`
	HeadSnapshot   string         `json:"head_snapshot,omitempty"`
	AgentSnapshot  map[string]any `json:"agent_snapshot,omitempty"`
	CapturedAt     time.Time      `json:"captured_at"`
}

type CommitRewriteRequest struct {
	EventID       string    `json:"event_id"`
	RepoFullName  string    `json:"repo_full_name"`
	SessionID     string    `json:"session_id,omitempty"`
	WorkspaceID   string    `json:"workspace_id"`
	BindingSource string    `json:"binding_source"`
	RewriteType   string    `json:"rewrite_type"`
	OldCommitSHA  string    `json:"old_commit_sha"`
	NewCommitSHA  string    `json:"new_commit_sha"`
	CapturedAt    time.Time `json:"captured_at"`
}

func (s *Service) RecordCheckpoint(ctx context.Context, req CommitCheckpointRequest) error {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.Or(repoconfig.FullNameEQ(req.RepoFullName), repoconfig.CloneURLEQ(req.RepoFullName))).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("checkpoint repo: %w", err)
	}

	existing, err := s.entClient.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(req.EventID)).Only(ctx)
	if err == nil && existing != nil {
		return nil
	}

	create := s.entClient.CommitCheckpoint.Create().
		SetEventID(req.EventID).
		SetWorkspaceID(req.WorkspaceID).
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSource(req.BindingSource)).
		SetCommitSHA(req.CommitSHA).
		SetParentShas(req.ParentSHAs).
		SetBranchSnapshot(req.BranchSnapshot).
		SetHeadSnapshot(req.HeadSnapshot).
		SetAgentSnapshot(req.AgentSnapshot).
		SetCapturedAt(req.CapturedAt)
	if req.SessionID != "" {
		sessionID, err := uuid.Parse(req.SessionID)
		if err != nil {
			return fmt.Errorf("checkpoint session id: %w", err)
		}
		create.SetSessionID(sessionID)
	}
	return create.Exec(ctx)
}

func (s *Service) RecordRewrite(ctx context.Context, req CommitRewriteRequest) error {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.Or(repoconfig.FullNameEQ(req.RepoFullName), repoconfig.CloneURLEQ(req.RepoFullName))).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("rewrite repo: %w", err)
	}

	existing, err := s.entClient.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(req.EventID)).Only(ctx)
	if err == nil && existing != nil {
		return nil
	}

	create := s.entClient.CommitRewrite.Create().
		SetEventID(req.EventID).
		SetWorkspaceID(req.WorkspaceID).
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitrewrite.BindingSource(req.BindingSource)).
		SetRewriteType(commitrewrite.RewriteType(req.RewriteType)).
		SetOldCommitSHA(req.OldCommitSHA).
		SetNewCommitSHA(req.NewCommitSHA).
		SetCapturedAt(req.CapturedAt)
	if req.SessionID != "" {
		sessionID, err := uuid.Parse(req.SessionID)
		if err != nil {
			return fmt.Errorf("rewrite session id: %w", err)
		}
		create.SetSessionID(sessionID)
	}
	return create.Exec(ctx)
}
```

- [ ] **Step 4: Expose checkpoint endpoints and wire them into the server**

Create `backend/internal/handler/checkpoint.go` and register routes:

```go
func (h *CheckpointHandler) Commit(c *gin.Context) {
	var req checkpoint.CommitCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.service.RecordCheckpoint(c.Request.Context(), req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *CheckpointHandler) Rewrite(c *gin.Context) {
	var req checkpoint.CommitRewriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.service.RecordRewrite(c.Request.Context(), req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}

// router.go
checkpointGroup := protected.Group("/checkpoints")
checkpointGroup.POST("/commit", checkpointHandler.Commit)
checkpointGroup.POST("/rewrite", checkpointHandler.Rewrite)

// main.go
checkpointService := checkpoint.NewService(entClient)
checkpointHandler := handler.NewCheckpointHandler(checkpointService)
```

- [ ] **Step 5: Run the checkpoint tests again**

Run: `cd backend && go test ./internal/checkpoint ./internal/handler -run 'TestRecordCheckpointUpsertsByEventIDAndWritesMetadataEvents|TestRecordRewriteAcceptsUnboundEvents' -v`

Expected: PASS for service idempotency and endpoint plumbing.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/checkpoint/service.go backend/internal/checkpoint/service_test.go backend/internal/handler/checkpoint.go backend/internal/handler/checkpoint_test.go backend/internal/handler/router.go backend/internal/handler/session.go backend/cmd/server/main.go
git commit -m "feat(backend): ingest checkpoint and rewrite events"
```

### Task 8: SCM Commit Enumeration And Manual PR Settlement

**Files:**
- Modify: `backend/internal/scm/provider.go`
- Modify: `backend/internal/scm/github/github.go`
- Modify: `backend/internal/scm/github/github_test.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`
- Create: `backend/internal/attribution/service.go`
- Create: `backend/internal/attribution/service_test.go`
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/internal/handler/pr.go`
- Create: `backend/internal/handler/pr_attribution_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing SCM and attribution tests**

Create `backend/internal/attribution/service_test.go` with:

```go
package attribution

import "testing"

func TestSettlePR_UsesPreviousOverallCheckpointForMatchedInterval(t *testing.T) {
	// Build one session with checkpoints at 09:10 (non-pr) and 09:20 (matched PR commit).
	// Mock SCMProvider.ListPRCommits to return only the second SHA and relayProvider.ListUsageLogsByAPIKeyExact
	// to return one 165-token record for [09:10,09:20). Expect Settle() to return
	// attribution_status=clear, attribution_confidence=high, and primary_token_count=165.
}

func TestSettlePR_ReturnsAmbiguousWhenMatchedCheckpointIsUnbound(t *testing.T) {
	// Build one PR whose only matched checkpoint has binding_source=unbound and no session_id.
	// Expect Settle() to return attribution_status=ambiguous and result_classification=ambiguous.
}
```

Append provider tests:

```go
// backend/internal/scm/github/github_test.go
func TestListPRCommits(t *testing.T) {
	// Serve two GitHub commit objects and assert ListPRCommits() returns []string{"abc123", "def456"}.
}

// backend/internal/scm/bitbucket/bitbucket_test.go
func TestListPRCommits(t *testing.T) {
	// Serve two Bitbucket commit objects and assert ListPRCommits() returns []string{"abc123", "def456"}.
}
```

- [ ] **Step 2: Run the attribution and SCM tests to verify settlement is still missing**

Run: `cd backend && go test ./internal/scm/... ./internal/attribution -run 'TestListPRCommits|TestSettlePR_' -v`

Expected: FAIL because `SCMProvider` does not expose `ListPRCommits` and the attribution service does not exist.

- [ ] **Step 3: Add PR commit enumeration to the SCM providers**

Update `backend/internal/scm/provider.go`, `backend/internal/scm/github/github.go`, and `backend/internal/scm/bitbucket/bitbucket.go`:

```go
type SCMProvider interface {
	GetRepo(ctx context.Context, fullName string) (*Repo, error)
	ListRepos(ctx context.Context, opts ListOpts) ([]*Repo, error)
	CreatePR(ctx context.Context, req CreatePRRequest) (*PR, error)
	GetPR(ctx context.Context, repoFullName string, prID int) (*PR, error)
	ListPRs(ctx context.Context, repoFullName string, opts PRListOpts) ([]*PR, error)
	ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error)
	GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error)
	GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error)
	AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error
	SetPRStatus(ctx context.Context, req SetStatusRequest) error
	MergePR(ctx context.Context, repoFullName string, prID int, opts MergeOpts) error
}

// github.go
func (p *Provider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	commits, _, err := p.client.PullRequests.ListCommits(ctx, owner, repo, prID, nil)
	if err != nil {
		return nil, fmt.Errorf("github list pr commits: %w", err)
	}
	shas := make([]string, 0, len(commits))
	for _, commit := range commits {
		shas = append(shas, commit.GetSHA())
	}
	return shas, nil
}

// bitbucket.go
func (p *Provider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	data, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d/commits?limit=1000", project, repo, prID), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Values []struct {
			ID string `json:"id"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	shas := make([]string, 0, len(result.Values))
	for _, commit := range result.Values {
		shas = append(shas, commit.ID)
	}
	return shas, nil
}
```

- [ ] **Step 4: Implement the manual settlement algorithm and `POST /prs/:id/settle`**

Create `backend/internal/attribution/service.go` with:

```go
package attribution

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/scm"
)

type Result struct {
	PRRecordID            int                    `json:"pr_record_id"`
	AttributionStatus     string                 `json:"attribution_status"`
	AttributionConfidence string                 `json:"attribution_confidence,omitempty"`
	PrimaryTokenCount     int64                  `json:"primary_token_count"`
	PrimaryTokenCost      float64                `json:"primary_token_cost"`
	MetadataSummary       map[string]any         `json:"metadata_summary,omitempty"`
	ValidationSummary     map[string]any         `json:"validation_summary,omitempty"`
	RunID                 int                    `json:"run_id"`
}

type Service struct {
	entClient     *ent.Client
	relayProvider relay.Provider
}

func NewService(entClient *ent.Client, relayProvider relay.Provider) *Service {
	return &Service{entClient: entClient, relayProvider: relayProvider}
}

func (s *Service) Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*Result, error) {
	commitSHAs, err := provider.ListPRCommits(ctx, pr.Edges.RepoConfig.FullName, pr.ScmPrID)
	if err != nil {
		return nil, fmt.Errorf("list pr commits: %w", err)
	}

	checkpoints, err := s.loadMatchedCheckpoints(ctx, pr.Edges.RepoConfig.ID, commitSHAs)
	if err != nil {
		return nil, err
	}
	if len(checkpoints) == 0 || hasUnboundCheckpoint(checkpoints) {
		return s.finishAmbiguous(ctx, pr, triggeredBy, "matched checkpoint missing or unbound"), nil
	}

	sessionIDs := uniqueSessionIDs(checkpoints)
	var totalTokens int64
	var totalCost float64
	metadataSummary := map[string]any{}
	validation := map[string]any{"result": "consistent", "confidence": "high"}

	for _, sessionID := range sessionIDs {
		overall := s.loadSessionCheckpoints(ctx, sessionID)
		sort.Slice(overall, func(i, j int) bool { return overall[i].CapturedAt.Before(overall[j].CapturedAt) })
		for _, matched := range matchedForSession(checkpoints, sessionID) {
			from := sessionStartedAtOrPreviousCheckpoint(overall, matched)
			to := matched.CapturedAt
			logs, err := s.relayProvider.ListUsageLogsByAPIKeyExact(ctx, int64(matched.Edges.Session.RelayAPIKeyID), from, to)
			if err != nil {
				return s.finishAmbiguous(ctx, pr, triggeredBy, "primary usage unavailable"), nil
			}
			totalTokens += sumTokens(logs)
			totalCost += sumCost(logs)
			metadataSummary = mergeMetadataDelta(metadataSummary, matched.AgentSnapshot)
		}
	}

	return s.finishClear(ctx, pr, triggeredBy, totalTokens, totalCost, metadataSummary, validation)
}

func hasUnboundCheckpoint(checkpoints []*ent.CommitCheckpoint) bool {
	for _, checkpoint := range checkpoints {
		if checkpoint.BindingSource == commitcheckpoint.BindingSourceUnbound {
			return true
		}
	}
	return false
}

func sumTokens(logs []relay.UsageLog) int64 {
	var total int64
	for _, log := range logs {
		total += log.TotalTokens
	}
	return total
}

func sumCost(logs []relay.UsageLog) float64 {
	var total float64
	for _, log := range logs {
		total += log.TotalCost
	}
	return total
}

func (s *Service) finishAmbiguous(ctx context.Context, pr *ent.PrRecord, triggeredBy, reason string) *Result {
	run := s.entClient.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy(triggeredBy).
		SetStatus("completed").
		SetResultClassification("ambiguous").
		SetValidationSummary(map[string]any{"result": "mismatch", "confidence": "low", "reason": reason}).
		SaveX(ctx)
	s.entClient.PrRecord.UpdateOneID(pr.ID).
		SetAttributionStatus(prrecord.AttributionStatusAmbiguous).
		SetAttributionConfidence(prrecord.AttributionConfidenceLow).
		SetLastAttributionRunID(run.ID).
		SetLastAttributedAt(time.Now()).
		ExecX(ctx)
	return &Result{PRRecordID: pr.ID, AttributionStatus: "ambiguous", AttributionConfidence: "low", RunID: run.ID}
}

func (s *Service) finishClear(ctx context.Context, pr *ent.PrRecord, triggeredBy string, totalTokens int64, totalCost float64, metadataSummary map[string]any, validation map[string]any) (*Result, error) {
	run, err := s.entClient.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy(triggeredBy).
		SetStatus("completed").
		SetResultClassification("clear").
		SetPrimaryUsageSummary(map[string]any{"total_tokens": totalTokens, "total_cost": totalCost}).
		SetMetadataSummary(metadataSummary).
		SetValidationSummary(validation).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.entClient.PrRecord.UpdateOneID(pr.ID).
		SetAttributionStatus(prrecord.AttributionStatusClear).
		SetAttributionConfidence(prrecord.AttributionConfidenceHigh).
		SetPrimaryTokenCount(totalTokens).
		SetPrimaryTokenCost(totalCost).
		SetMetadataSummary(metadataSummary).
		SetLastAttributionRunID(run.ID).
		SetLastAttributedAt(time.Now()).
		Exec(ctx); err != nil {
		return nil, err
	}
	return &Result{PRRecordID: pr.ID, AttributionStatus: "clear", AttributionConfidence: "high", PrimaryTokenCount: totalTokens, PrimaryTokenCost: totalCost, MetadataSummary: metadataSummary, ValidationSummary: validation, RunID: run.ID}, nil
}

func (s *Service) loadMatchedCheckpoints(ctx context.Context, repoConfigID int, commitSHAs []string) ([]*ent.CommitCheckpoint, error) {
	return s.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.RepoConfigIDEQ(repoConfigID),
			commitcheckpoint.CommitSHAIn(commitSHAs...),
		).
		WithSession().
		All(ctx)
}

func uniqueSessionIDs(checkpoints []*ent.CommitCheckpoint) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	var result []uuid.UUID
	for _, checkpoint := range checkpoints {
		if checkpoint.SessionID == nil {
			continue
		}
		if _, ok := seen[*checkpoint.SessionID]; ok {
			continue
		}
		seen[*checkpoint.SessionID] = struct{}{}
		result = append(result, *checkpoint.SessionID)
	}
	return result
}

func (s *Service) loadSessionCheckpoints(ctx context.Context, sessionID uuid.UUID) []*ent.CommitCheckpoint {
	return s.entClient.CommitCheckpoint.Query().
		Where(commitcheckpoint.HasSessionWith(session.IDEQ(sessionID))).
		AllX(ctx)
}

func matchedForSession(checkpoints []*ent.CommitCheckpoint, sessionID uuid.UUID) []*ent.CommitCheckpoint {
	filtered := make([]*ent.CommitCheckpoint, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		if checkpoint.SessionID != nil && *checkpoint.SessionID == sessionID {
			filtered = append(filtered, checkpoint)
		}
	}
	return filtered
}

func sessionStartedAtOrPreviousCheckpoint(overall []*ent.CommitCheckpoint, current *ent.CommitCheckpoint) time.Time {
	for i := range overall {
		if overall[i].ID == current.ID {
			if i == 0 {
				return current.Edges.Session.StartedAt
			}
			return overall[i-1].CapturedAt
		}
	}
	return current.Edges.Session.StartedAt
}

func mergeMetadataDelta(acc map[string]any, snapshot map[string]any) map[string]any {
	if acc == nil {
		acc = map[string]any{}
	}
	for key, value := range snapshot {
		acc[key] = value
	}
	return acc
}
```

Update `backend/internal/handler/interfaces.go` and `backend/internal/handler/pr.go`:

```go
type prAttributor interface {
	Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.Result, error)
}

func (h *PRHandler) Settle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	pr, err := h.entClient.PrRecord.Query().Where(prrecord.IDEQ(id)).WithRepoConfig().Only(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "PR not found")
		return
	}

	scmProvider, _, err := h.repoService.GetSCMProvider(c.Request.Context(), pr.Edges.RepoConfig.ID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	username, _ := c.Get("username")
	result, err := h.attributor.Settle(c.Request.Context(), scmProvider, pr, username.(string))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, result)
}

// router.go
prGroup.POST("/:id/settle", prHandler.Settle)
```

- [ ] **Step 5: Run the settlement tests again**

Run: `cd backend && go test ./internal/scm/... ./internal/attribution ./internal/handler -run 'TestListPRCommits|TestSettlePR_|TestPRHandlerSettle' -v`

Expected: PASS for GitHub/Bitbucket commit enumeration, the interval algorithm, and the settle endpoint.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/scm/provider.go backend/internal/scm/github/github.go backend/internal/scm/github/github_test.go backend/internal/scm/bitbucket/bitbucket.go backend/internal/scm/bitbucket/bitbucket_test.go backend/internal/attribution/service.go backend/internal/attribution/service_test.go backend/internal/handler/interfaces.go backend/internal/handler/pr.go backend/internal/handler/pr_attribution_test.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add manual PR attribution settlement"
```

### Task 9: Frontend Session Audit And PR Attribution UI

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/session.ts`
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/views/sessions/SessionListView.vue`
- Create: `frontend/src/views/sessions/SessionDetailView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Create: `frontend/src/__tests__/repo-detail-view.test.ts`
- Create: `frontend/src/__tests__/session-detail-view.test.ts`

- [ ] **Step 1: Write failing frontend API and view tests**

Append this test to `frontend/src/__tests__/api-modules.test.ts`:

```ts
it('settlePR calls POST /prs/:id/settle', async () => {
  mockClient.post.mockResolvedValue({ data: { data: {} } })
  const { settlePR } = await import('@/api/pr')
  await settlePR(42)
  expect(mockClient.post).toHaveBeenCalledWith('/prs/42/settle')
})
```

Create `frontend/src/__tests__/repo-detail-view.test.ts` with:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { vi } from 'vitest'
import RepoDetailView from '@/views/repos/RepoDetailView.vue'
import * as repoAPI from '@/api/repo'
import * as prAPI from '@/api/pr'

it('renders attribution summary and settles a PR', async () => {
  vi.spyOn(repoAPI, 'getRepo').mockResolvedValue({ data: { data: { id: 1, name: 'ai-efficiency', full_name: 'org/ai-efficiency', clone_url: '', default_branch: 'main', ai_score: 80, status: 'active', last_scan_at: null, group_id: 0, created_at: '2026-03-27T00:00:00Z' } } } as any)
  vi.spyOn(prAPI, 'listPRs').mockResolvedValue({ data: { data: { items: [{ id: 11, scm_pr_id: 55, scm_pr_url: '', author: 'alice', title: 'Add attribution', source_branch: 'feat', target_branch: 'main', status: 'open', labels: [], lines_added: 10, lines_deleted: 2, ai_label: 'pending', ai_ratio: 0, token_cost: 0, attribution_status: 'clear', attribution_confidence: 'high', primary_token_cost: 1.25, cycle_time_hours: 0, merged_at: null, created_at: '2026-03-27T00:00:00Z' }], total: 1 } } } as any)
  const settleSpy = vi.spyOn(prAPI, 'settlePR').mockResolvedValue({ data: { data: { attribution_status: 'clear' } } } as any)

  const wrapper = mount(RepoDetailView, {
    global: {
      mocks: {
        $route: { params: { id: '1' } },
        $router: { push: vi.fn() },
      },
    },
  })

  await flushPromises()
  expect(wrapper.text()).toContain('clear')
  expect(wrapper.text()).toContain('$1.25')
  await wrapper.find('button').trigger('click')
  expect(settleSpy).toHaveBeenCalledWith(11)
})
```

Create `frontend/src/__tests__/session-detail-view.test.ts` with:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { vi } from 'vitest'
import SessionDetailView from '@/views/sessions/SessionDetailView.vue'
import * as sessionAPI from '@/api/session'

it('renders workspace and checkpoint details for one session', async () => {
  vi.spyOn(sessionAPI, 'getSession').mockResolvedValue({
    data: {
      data: {
        id: 'sess-1',
        branch: 'feat/attribution',
        status: 'active',
        started_at: '2026-03-27T09:00:00Z',
        ended_at: null,
        provider_name: 'sub2api',
        relay_api_key_id: 501,
        runtime_ref: 'runtime/sess-1',
        tool_invocations: [],
        edges: {
          session_workspaces: [{ workspace_id: 'ws-1', workspace_root: '/tmp/repo', binding_source: 'marker', last_seen_at: '2026-03-27T09:10:00Z' }],
          commit_checkpoints: [{ commit_sha: 'abc123', binding_source: 'marker', captured_at: '2026-03-27T09:10:00Z' }],
        },
      },
    },
  } as any)

  const wrapper = mount(SessionDetailView, {
    global: {
      mocks: {
        $route: { params: { id: 'sess-1' } },
      },
    },
  })

  await flushPromises()
  expect(wrapper.text()).toContain('sub2api')
  expect(wrapper.text()).toContain('/tmp/repo')
  expect(wrapper.text()).toContain('abc123')
})
```

- [ ] **Step 2: Run the frontend tests to confirm the new UI/API surface is still absent**

Run: `cd frontend && pnpm test -- --run api-modules repo-detail-view session-detail-view`

Expected: FAIL because `settlePR`, `SessionDetailView`, and the new attribution/session fields are not implemented.

- [ ] **Step 3: Extend the frontend types and API clients**

Update `frontend/src/types/index.ts` and `frontend/src/api/pr.ts`:

```ts
export interface Session {
  id: string
  branch: string
  status: string
  started_at: string
  ended_at: string | null
  provider_name?: string | null
  relay_api_key_id?: number | null
  runtime_ref?: string | null
  initial_workspace_root?: string | null
  last_seen_at?: string | null
  tool_invocations: Array<{ tool: string; start: string; end: string }>
  edges?: {
    repo_config?: RepoConfig
    session_workspaces?: Array<{ workspace_id: string; workspace_root: string; binding_source: string; last_seen_at: string }>
    commit_checkpoints?: Array<{ commit_sha: string; binding_source: string; captured_at: string }>
  }
}

export interface PRRecord {
  id: number
  scm_pr_id: number
  scm_pr_url: string
  author: string
  title: string
  source_branch: string
  target_branch: string
  status: string
  labels: string[]
  lines_added: number
  lines_deleted: number
  ai_label: string
  ai_ratio: number
  token_cost: number
  attribution_status?: 'not_run' | 'clear' | 'ambiguous' | 'failed'
  attribution_confidence?: 'high' | 'medium' | 'low' | null
  primary_token_count?: number
  primary_token_cost?: number
  metadata_summary?: Record<string, any>
  last_attributed_at?: string | null
  cycle_time_hours: number
  merged_at: string | null
  created_at: string
}

export function settlePR(prId: number) {
  return client.post<ApiResponse<{ attribution_status: string }>>(`/prs/${prId}/settle`)
}
```

- [ ] **Step 4: Render attribution summaries and a real session detail page**

Update `frontend/src/views/repos/RepoDetailView.vue`, `frontend/src/views/sessions/SessionListView.vue`, `frontend/src/views/sessions/SessionDetailView.vue`, and `frontend/src/router/index.ts`:

```vue
<!-- RepoDetailView.vue -->
<th class="px-3 py-2 text-left font-medium">Attribution</th>
<th class="px-3 py-2 text-left font-medium">Primary Cost</th>
<th class="px-3 py-2 text-left font-medium">Action</th>

<td class="px-3 py-2">
  <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5"
    :class="pr.attribution_status === 'clear' ? 'bg-green-50 text-green-700' : pr.attribution_status === 'ambiguous' ? 'bg-yellow-50 text-yellow-700' : pr.attribution_status === 'failed' ? 'bg-red-50 text-red-700' : 'bg-gray-50 text-gray-500'">
    {{ pr.attribution_status || 'not_run' }}
  </span>
  <span v-if="pr.attribution_confidence" class="ml-2 text-xs text-gray-400">{{ pr.attribution_confidence }}</span>
</td>
<td class="px-3 py-2 text-gray-500">
  {{ pr.primary_token_cost != null ? `$${pr.primary_token_cost.toFixed(2)}` : '—' }}
</td>
<td class="px-3 py-2">
  <button class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50" @click="handleSettlePR(pr.id)">
    Settle
  </button>
</td>
```

```ts
// RepoDetailView.vue script
import { listPRs, syncPRs, settlePR } from '@/api/pr'

async function handleSettlePR(prId: number) {
  try {
    await settlePR(prId)
    await loadPRs()
  } catch { /* settle failed */ }
}
```

```vue
<!-- SessionDetailView.vue -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getSession } from '@/api/session'
import type { Session } from '@/types'

const route = useRoute()
const session = ref<Session | null>(null)

onMounted(async () => {
  const res = await getSession(String(route.params.id))
  session.value = res.data.data ?? null
})
</script>

<template>
  <AppLayout>
    <div v-if="session" class="space-y-6">
      <div class="rounded-lg bg-white p-5 shadow">
        <h1 class="text-xl font-semibold text-gray-900">Session {{ session.id.slice(0, 8) }}</h1>
        <p class="mt-2 text-sm text-gray-500">Provider: {{ session.provider_name || '—' }}</p>
        <p class="text-sm text-gray-500">Relay Key ID: {{ session.relay_api_key_id ?? '—' }}</p>
        <p class="text-sm text-gray-500">Runtime Ref: {{ session.runtime_ref || '—' }}</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Observed Workspaces</h2>
        <ul class="mt-3 space-y-2 text-sm text-gray-600">
          <li v-for="ws in session.edges?.session_workspaces ?? []" :key="ws.workspace_id">
            {{ ws.workspace_root }} ({{ ws.binding_source }})
          </li>
        </ul>
      </div>
    </div>
  </AppLayout>
</template>
```

```ts
// router/index.ts
{
  path: '/sessions/:id',
  name: 'SessionDetail',
  component: () => import('@/views/sessions/SessionDetailView.vue'),
}
```

- [ ] **Step 5: Run the frontend tests and build again**

Run:

```bash
cd frontend
pnpm test -- --run api-modules repo-detail-view session-detail-view
pnpm build
```

Expected: tests PASS and `vite build` completes without type errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/api/session.ts frontend/src/api/pr.ts frontend/src/views/repos/RepoDetailView.vue frontend/src/views/sessions/SessionListView.vue frontend/src/views/sessions/SessionDetailView.vue frontend/src/router/index.ts frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/repo-detail-view.test.ts frontend/src/__tests__/session-detail-view.test.ts
git commit -m "feat(frontend): surface session audits and pr attribution"
```

### Task 10: Operator Docs And End-To-End Verification

**Files:**
- Create: `docs/ae-cli/session-pr-attribution.md`

- [ ] **Step 1: Write the operator-facing lifecycle guide**

Create `docs/ae-cli/session-pr-attribution.md` with:

```md
# Session / PR Attribution Operations

## Local State

- Workspace marker: `<workspace>/.ae/session.json`
- Runtime bundle: `~/.ae-cli/runtime/<session-id>/`
- Shared hooks: `$(git rev-parse --git-common-dir)/ae-hooks`
- Retry queue: `~/.ae-cli/runtime/<session-id>/queue/`

## Normal Flow

1. `ae-cli login`
2. `ae-cli start`
3. Commit normally; hooks upload checkpoints fail-open
4. If the backend was unavailable, run `ae-cli flush`
5. Trigger settlement with `POST /api/v1/prs/:id/settle`

## Recovery

- Missing marker but AE env vars present: the next `post-commit` rewrites `/.ae/session.json`
- Expired or revoked session key: `ae-cli start` abandons the stale session and bootstraps a new one
- Queued events: inspect `queue/` and replay with `ae-cli flush`
```

- [ ] **Step 2: Run the backend full test suite**

Run: `cd backend && go test ./...`

Expected: PASS. If a specific package is flaky or environment-sensitive, capture the exact failure and fix it before proceeding.

- [ ] **Step 3: Run the ae-cli full test suite**

Run: `cd ae-cli && go test ./...`

Expected: PASS, including the new workspace/runtime/hooks/collector packages.

- [ ] **Step 4: Run the frontend tests and production build**

Run:

```bash
cd frontend
pnpm test
pnpm build
```

Expected: PASS for Vitest and Vite.

- [ ] **Step 5: Execute the manual acceptance checklist**

Run and verify:

```bash
ae-cli login
ae-cli start
git commit --allow-empty -m "test attribution hook"
ae-cli flush
curl -X POST "$AE_SERVER_URL/api/v1/prs/<id>/settle" -H "Authorization: Bearer <token>"
```

Expected:
- `ae-cli start` writes `/.ae/session.json` and `~/.ae-cli/runtime/<session-id>/env.json`
- the commit creates one `commit_checkpoints` row and at least one `agent_metadata_events` row
- `ae-cli flush` drains any queued events
- `/prs/:id/settle` returns a summary with `attribution_status`, `primary_token_count`, `primary_token_cost`, and `validation_summary`

- [ ] **Step 6: Commit**

```bash
git add docs/ae-cli/session-pr-attribution.md
git commit -m "docs(ae-cli): document session attribution lifecycle"
```
