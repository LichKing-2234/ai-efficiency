# OAuth CLI Login & Auth System Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement OAuth2 login for ae-cli, introduce Relay Provider abstraction layer, add LDAP management UI, and auto-deliver API keys — enabling zero-config AI tool usage.

**Architecture:** Backend adds `relay.Provider` interface abstracting all relay server interactions (auth, LLM, usage, API keys), replaces sub2apidb direct DB access with REST API calls. OAuth2 Authorization Code Flow with PKCE enables ae-cli browser-based login. Frontend adds OAuth authorize page and LDAP settings page. ae-cli simplified to compile-time server URL + token.json.

**Tech Stack:** Go 1.26+ (Gin, Ent, go-oauth2/oauth2, golang.org/x/oauth2), Vue 3 (Vite, TailwindCSS, Pinia, TypeScript), Cobra CLI

**Spec:** `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`

**Status:** ✅ 已完成（2026-03-25）

**Replay Status:** 历史完成记录。可以作为实现背景参考，但不建议直接按本文逐 task 重跑；如需重做或扩展，请基于当前 relay/OAuth 代码和最新 spec 新写执行计划。

**Source Of Truth:** 已实现的 relay、OAuth、token、session 集成以当前代码为准；本文中保留的 placeholder、旧依赖说明和历史偏差记录不应覆盖当前代码或最新 spec。

**Known Stale Sections:** go-oauth2 方案、独立 LDAP 页面、以及旧的依赖清理说明不再代表当前代码现状。

> **Updated:** 2026-03-25 — 基于代码审查同步 plan 与实际实现，全部 checkbox 已勾选。已知偏差：go-oauth2/oauth2 库未使用（采用自定义 OAuth 实现），squirrel 依赖仍在 go.mod 中（sub2apidb 已删除）。

---

## File Structure

### Backend — New Files

| File | Responsibility |
|------|---------------|
| `backend/internal/relay/provider.go` | Provider interface + sentinel errors |
| `backend/internal/relay/types.go` | Shared data types (User, APIKey, ChatCompletionRequest, etc.) |
| `backend/internal/relay/sub2api.go` | Sub2api implementation of Provider |
| `backend/internal/relay/sub2api_test.go` | Tests for sub2api implementation |
| `backend/internal/oauth/server.go` | OAuth2 server init, go-oauth2/oauth2 config |
| `backend/internal/oauth/handler.go` | /oauth/authorize, /oauth/token, /oauth/authorize/approve handlers |
| `backend/internal/oauth/pkce.go` | PKCE code_challenge verification |
| `backend/internal/oauth/pkce_test.go` | PKCE tests |
| `backend/internal/oauth/handler_test.go` | OAuth handler tests |
| `backend/internal/handler/provider.go` | RelayProvider CRUD + API key delivery handler |
| `backend/internal/handler/admin_settings.go` | LDAP settings handler |
| `backend/ent/schema/system_setting.go` | SystemSetting Ent schema |
| `backend/ent/schema/relay_provider.go` | RelayProvider Ent schema |

### Backend — Modified Files

| File | Changes |
|------|---------|
| `backend/internal/config/config.go` | Add `RelayConfig`, `ServerConfig.FrontendURL`; remove `Sub2apiDB` |
| `backend/internal/auth/sso.go` | Replace `sub2apidb.Client` with `relay.Provider` |
| `backend/internal/auth/auth.go` | Rename `Sub2apiUserID` → `RelayUserID` in UserInfo |
| `backend/internal/analysis/analyzer.go` | Replace direct HTTP calls with `relay.Provider.ChatCompletion` |
| `backend/internal/handler/router.go` | Add OAuth routes, provider routes, LDAP settings routes |
| `backend/internal/handler/session.go` | Add `user_id` from JWT, `tool_configs` field |
| `backend/ent/schema/user.go` | Add `relay_sso` enum, rename `sub2api_user_id` → `relay_user_id` |
| `backend/ent/schema/session.go` | Add user edge, `tool_configs`, `provider_name`, relay fields |
| `backend/go.mod` | Add go-oauth2/oauth2; remove squirrel |

### Backend — Deleted Files

| File | Reason |
|------|--------|
| `backend/internal/sub2apidb/` (entire package) | Replaced by relay.Provider REST API calls |

### ae-cli — New Files

| File | Responsibility |
|------|---------------|
| `ae-cli/internal/buildinfo/buildinfo.go` | Compile-time server URL + version |
| `ae-cli/internal/auth/oauth.go` | OAuth login flow (local server, browser open, code exchange) |
| `ae-cli/internal/auth/token.go` | Token file read/write/refresh |
| `ae-cli/internal/auth/oauth_test.go` | OAuth flow tests |
| `ae-cli/internal/auth/token_test.go` | Token management tests |
| `ae-cli/cmd/login.go` | login command |
| `ae-cli/cmd/logout.go` | logout command |

### ae-cli — Modified Files

| File | Changes |
|------|---------|
| `ae-cli/config/config.go` | Remove `ToolConfig`, `Sub2apiConfig`, `ServerConfig`; simplify |
| `ae-cli/internal/client/client.go` | Use `buildinfo.ServerURL` + token.json, auto-refresh |
| `ae-cli/cmd/root.go` | Remove config dependency, use buildinfo |
| `ae-cli/cmd/start.go` | Auto-trigger login if no valid token |
| `ae-cli/internal/shell/shell.go` | Remove `config.Tools` dependency (Spec 2 handles) |
| `ae-cli/go.mod` | Add `golang.org/x/oauth2` |

### Frontend — New Files

| File | Responsibility |
|------|---------------|
| `frontend/src/views/oauth/AuthorizePage.vue` | OAuth authorize page |
| `frontend/src/api/oauth.ts` | OAuth API calls |
| `frontend/src/views/admin/LdapSettingsView.vue` | LDAP settings page |

### Frontend — Modified Files

| File | Changes |
|------|---------|
| `frontend/src/router/index.ts` | Add `/oauth/authorize` (public), `/admin/ldap` route |

---

## Tasks

### Task 1: Relay Provider Interface & Types

**Files:**
- Create: `backend/internal/relay/provider.go`
- Create: `backend/internal/relay/types.go`

- [x] **Step 1: Create relay package directory**

Run: `mkdir -p backend/internal/relay`

- [x] **Step 2: Write Provider interface and sentinel errors**

Create `backend/internal/relay/provider.go`:

```go
package relay

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for authentication outcomes.
var (
	ErrInvalidCredentials        = errors.New("relay: invalid credentials")
	ErrExtraVerificationRequired = errors.New("relay: extra verification required")
)

// Provider defines the unified interface for relay server interactions.
type Provider interface {
	Ping(ctx context.Context) error
	Name() string

	Authenticate(ctx context.Context, username, password string) (*User, error)
	GetUser(ctx context.Context, userID int64) (*User, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)

	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

	GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)
	ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)
	CreateUserAPIKey(ctx context.Context, userID int64, keyName string) (*APIKeyWithSecret, error)
}
```

- [x] **Step 3: Write shared data types**

Create `backend/internal/relay/types.go`:

```go
package relay

import "encoding/json"

type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type APIKey struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type APIKeyWithSecret struct {
	APIKey
	Secret string `json:"secret"`
}

type UsageStats struct {
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	Content    string `json:"content"`
	TokensUsed int    `json:"tokens_used"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionWithToolsResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}
```

- [x] **Step 4: Verify compilation**

Run: `cd backend && go build ./internal/relay/`
Expected: no errors

- [x] **Step 5: Commit**

```bash
git add backend/internal/relay/provider.go backend/internal/relay/types.go
git commit -m "feat(backend): add relay.Provider interface and shared types"
```

---

### Task 2: Sub2api Provider Implementation

**Files:**
- Create: `backend/internal/relay/sub2api.go`
- Create: `backend/internal/relay/sub2api_test.go`

- [x] **Step 1: Write test for Ping**

Create `backend/internal/relay/sub2api_test.go`:

```go
package relay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

func newTestProvider(t *testing.T, handler http.Handler) relay.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return relay.NewSub2apiProvider(srv.Client(), srv.URL+"/v1", srv.URL, "test-admin-key", "test-model", zap.NewNop())
}

func TestPing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	p := newTestProvider(t, mux)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestPingUnreachable(t *testing.T) {
	p := relay.NewSub2apiProvider(http.DefaultClient, "http://localhost:1", "http://localhost:1", "key", "model", zap.NewNop())
	if err := p.Ping(context.Background()); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/relay/ -run TestPing -v`
Expected: FAIL — `NewSub2apiProvider` not defined

- [x] **Step 3: Write sub2api provider skeleton with Ping**

Create `backend/internal/relay/sub2api.go`:

```go
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

type sub2apiRelay struct {
	client   *http.Client
	baseURL  string // LLM API endpoint, e.g. http://localhost:3000/v1
	adminURL string // Admin API endpoint, e.g. http://localhost:3000
	apiKey   string // Admin API key
	model    string
	logger   *zap.Logger
}

func NewSub2apiProvider(httpClient *http.Client, baseURL, adminURL, apiKey, model string, logger *zap.Logger) Provider {
	return &sub2apiRelay{
		client:   httpClient,
		baseURL:  strings.TrimRight(baseURL, "/"),
		adminURL: strings.TrimRight(adminURL, "/"),
		apiKey:   apiKey,
		model:    model,
		logger:   logger,
	}
}

func (s *sub2apiRelay) Name() string { return "sub2api" }

func (s *sub2apiRelay) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ping relay server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d", resp.StatusCode)
	}
	return nil
}
```

- [x] **Step 4: Run Ping test to verify it passes**

Run: `cd backend && go test ./internal/relay/ -run TestPing -v`
Expected: PASS

- [x] **Step 5: Add Authenticate test**

Append to `sub2api_test.go`:

```go
func TestAuthenticate(t *testing.T) {
	mux := http.NewServeMux()
	// POST /api/v1/auth/login → returns session token
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] == "admin" && body["password"] == "pass" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    map[string]string{"token": "session-token-123"},
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false})
	})
	// GET /api/v1/auth/me → returns user info
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"id": 42, "username": "admin", "email": "admin@test.com",
			},
		})
	})
	p := newTestProvider(t, mux)
	u, err := p.Authenticate(context.Background(), "admin", "pass")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if u.ID != 42 || u.Username != "admin" {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestAuthenticateInvalidCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	p := newTestProvider(t, mux)
	_, err := p.Authenticate(context.Background(), "bad", "bad")
	if err != relay.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}
```

- [x] **Step 6: Implement Authenticate**

Add to `sub2api.go`:

```go
func (s *sub2apiRelay) Authenticate(ctx context.Context, username, password string) (*User, error) {
	// Step 1: POST /api/v1/auth/login
	loginBody := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.adminURL+"/api/v1/auth/login", strings.NewReader(loginBody))
	if err != nil {
		return nil, fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Check for extra verification (TOTP/Turnstile)
		if strings.Contains(string(body), "requires_2fa") || strings.Contains(string(body), "turnstile") {
			return nil, ErrExtraVerificationRequired
		}
		return nil, fmt.Errorf("login returned status %d: %s", resp.StatusCode, body)
	}

	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}

	// Step 2: GET /api/v1/auth/me with session token
	meReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/api/v1/auth/me", nil)
	if err != nil {
		return nil, fmt.Errorf("create me request: %w", err)
	}
	meReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)

	meResp, err := s.client.Do(meReq)
	if err != nil {
		return nil, fmt.Errorf("me request: %w", err)
	}
	defer meResp.Body.Close()

	var meResult struct {
		Data User `json:"data"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&meResult); err != nil {
		return nil, fmt.Errorf("decode me response: %w", err)
	}

	return &meResult.Data, nil
}
```

- [x] **Step 7: Run Authenticate tests**

Run: `cd backend && go test ./internal/relay/ -run TestAuthenticate -v`
Expected: PASS

- [x] **Step 8: Add remaining method tests and implementations**

Add tests and implementations for the remaining methods. Each method follows the same pattern — test first, then implement:

- `GetUser`: `GET /api/v1/admin/users/:id` with admin API key header
- `FindUserByEmail`: `GET /api/v1/admin/users?email=xxx` with admin API key header, returns nil,nil if not found
- `ChatCompletion`: `POST {baseURL}/chat/completions` with Bearer API key, parse OpenAI-compatible response
- `ChatCompletionWithTools`: same as ChatCompletion but with `tools` field in request body
- `GetUsageStats`: `GET /api/v1/admin/users/:id/usage?from=xxx&to=xxx` with admin API key
- `ListUserAPIKeys`: `GET /api/v1/admin/users/:id/api-keys` with admin API key
- `CreateUserAPIKey`: `POST /api/v1/keys` with admin API key, body `{"user_id":..., "name":...}`

For each method, the admin API key is sent as `Authorization: Bearer {apiKey}` header.

Implementation pattern for admin API calls (reusable helper):

```go
func (s *sub2apiRelay) doAdminRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.adminURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}
```

- [x] **Step 9: Run all relay tests**

Run: `cd backend && go test ./internal/relay/ -v`
Expected: all PASS

- [x] **Step 10: Commit**

```bash
git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go
git commit -m "feat(backend): implement sub2api relay provider with tests"
```

---

### Task 3: Backend Config Changes

**Files:**
- Modify: `backend/internal/config/config.go`

- [x] **Step 1: Add RelayConfig and FrontendURL, remove Sub2apiDB**

Modify `backend/internal/config/config.go`:

```go
// Add to Config struct (replace Sub2apiDB with Relay):
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	DB         DBConfig         `mapstructure:"db"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Analysis   AnalysisConfig   `mapstructure:"analysis"`
	Relay      RelayConfig      `mapstructure:"relay"`
}

// Add FrontendURL to ServerConfig:
type ServerConfig struct {
	Port        int    `mapstructure:"port"`
	Mode        string `mapstructure:"mode"`
	FrontendURL string `mapstructure:"frontend_url"`
}

// Add new RelayConfig:
type RelayConfig struct {
	Provider string `mapstructure:"provider"`
	URL      string `mapstructure:"url"`
	APIKey   string `mapstructure:"api_key"`
	Model    string `mapstructure:"model"`
}
```

Remove `Sub2apiDB` field from Config struct. Remove sub2api_db defaults from `Load()`. Add relay defaults:

```go
v.SetDefault("relay.provider", "sub2api")
v.SetDefault("relay.model", "claude-sonnet-4-20250514")
v.SetDefault("server.frontend_url", "http://localhost:5173")
```

- [x] **Step 2: Verify compilation**

Run: `cd backend && go build ./internal/config/`
Expected: no errors (note: other packages referencing Sub2apiDB will break — fixed in later tasks)

- [x] **Step 3: Commit**

```bash
git add backend/internal/config/config.go
git commit -m "refactor(backend): add RelayConfig, remove Sub2apiDB from config"
```

---

### Task 4: User Schema Migration

**Files:**
- Modify: `backend/ent/schema/user.go`

- [x] **Step 1: Add relay_sso enum value and rename sub2api_user_id**

Modify `backend/ent/schema/user.go` — change the `auth_source` enum and rename the field:

```go
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").
			Unique().
			NotEmpty(),
		field.String("email").
			Unique().
			NotEmpty(),
		field.Enum("auth_source").
			Values("sub2api_sso", "relay_sso", "ldap"),
		field.Int("relay_user_id").
			Optional().
			Nillable(),
		field.String("ldap_dn").
			Optional().
			Nillable(),
		field.Enum("role").
			Values("admin", "user").
			Default("user"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("relay_user_id"),
	}
}
```

Key changes:
- `auth_source` enum: add `"relay_sso"` (keep `"sub2api_sso"` for migration compatibility)
- Rename `sub2api_user_id` → `relay_user_id`
- Index updated accordingly

- [x] **Step 2: Run Ent code generation**

Run: `cd backend && go generate ./ent/`
Expected: generated code updated, no errors

- [x] **Step 3: Fix compilation errors from field rename**

Search for all references to `Sub2apiUserID` in the codebase and update to `RelayUserID`:

Run: `cd backend && grep -rn "Sub2apiUserID\|sub2api_user_id" --include="*.go" | grep -v ent/`

Update each reference found (e.g., in `auth/auth.go`, `handler/session.go`).

- [x] **Step 4: Verify compilation**

Run: `cd backend && go build ./...`
Expected: may have errors from other packages — note them for later tasks

- [x] **Step 5: Commit**

```bash
git add backend/ent/ backend/internal/
git commit -m "refactor(backend): migrate User schema — add relay_sso, rename sub2api_user_id to relay_user_id"
```

---

### Task 5: Session Schema Migration

**Files:**
- Modify: `backend/ent/schema/session.go`

- [x] **Step 1: Update Session schema fields and edges**

Modify `backend/ent/schema/session.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("branch").
			NotEmpty(),
		field.Int("relay_user_id").
			Optional().
			Nillable(),
		field.Int("relay_api_key_id").
			Optional().
			Nillable(),
		field.String("provider_name").
			Optional().
			Nillable(),
		field.JSON("tool_configs", []map[string]interface{}{}).
			Optional().
			Default([]map[string]interface{}{}),
		field.Time("started_at").
			Default(timeNow),
		field.Time("ended_at").
			Optional().
			Nillable(),
		field.JSON("tool_invocations", []map[string]interface{}{}).
			Default([]map[string]interface{}{}),
		field.Enum("status").
			Values("active", "completed", "abandoned").
			Default("active"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("sessions").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("sessions").
			Unique(),
	}
}
```

Key changes:
- Rename `sub2api_user_id` → `relay_user_id`
- Rename `sub2api_api_key_id` → `relay_api_key_id`
- Add `provider_name` (optional string)
- Add `tool_configs` (JSON array)
- Add `user` edge (optional, to User)

- [x] **Step 2: Add sessions edge to User schema**

Add to `backend/ent/schema/user.go` Edges method:

```go
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sessions", Session.Type),
	}
}
```

- [x] **Step 3: Run Ent code generation**

Run: `cd backend && go generate ./ent/`
Expected: generated code updated

- [x] **Step 4: Fix compilation errors from field renames**

Run: `cd backend && grep -rn "Sub2apiAPIKeyID\|sub2api_api_key_id\|Sub2apiUserID" --include="*.go" | grep -v ent/`

Update all references in handler/session.go and other files.

- [x] **Step 5: Verify compilation**

Run: `cd backend && go build ./...`

- [x] **Step 6: Commit**

```bash
git add backend/ent/ backend/internal/
git commit -m "refactor(backend): migrate Session schema — add user edge, tool_configs, rename to relay fields"
```

---

### Task 6: SSO Provider Refactor

**Files:**
- Modify: `backend/internal/auth/sso.go`
- Modify: `backend/internal/auth/auth.go`

- [x] **Step 1: Update UserInfo to use RelayUserID**

Modify `backend/internal/auth/auth.go` — rename field in UserInfo:

```go
type UserInfo struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	AuthSource   string `json:"auth_source"`
	RelayUserID  *int64 `json:"relay_user_id,omitempty"`
}
```

Update `ensureLocalUser` to use `RelayUserID` instead of `Sub2apiUserID`:

```go
func (s *Service) ensureLocalUser(ctx context.Context, info *UserInfo) (*ent.User, error) {
	u, err := s.entClient.User.Query().
		Where(entuser.UsernameEQ(info.Username)).
		Only(ctx)
	if err == nil {
		return u, nil
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
		create.SetRelayUserID(int(*info.RelayUserID))
	}

	return create.Save(ctx)
}
```

- [x] **Step 2: Rewrite SSO provider to use relay.Provider**

Replace `backend/internal/auth/sso.go`:

```go
package auth

import (
	"context"
	"errors"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

type SSOProvider struct {
	relayProvider relay.Provider
	logger        *zap.Logger
}

func NewSSOProvider(relayProvider relay.Provider, logger *zap.Logger) *SSOProvider {
	return &SSOProvider{
		relayProvider: relayProvider,
		logger:        logger,
	}
}

func (p *SSOProvider) Name() string {
	return "sso"
}

func (p *SSOProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	if p.relayProvider == nil {
		return nil, nil
	}

	relayUser, err := p.relayProvider.Authenticate(ctx, username, password)
	if err != nil {
		if errors.Is(err, relay.ErrInvalidCredentials) {
			p.logger.Debug("relay SSO: invalid credentials", zap.String("username", username))
			return nil, nil // fallthrough to next provider
		}
		if errors.Is(err, relay.ErrExtraVerificationRequired) {
			p.logger.Warn("relay SSO: extra verification required, skipping", zap.String("username", username))
			return nil, nil // fallthrough to next provider
		}
		p.logger.Warn("relay SSO: authentication error", zap.Error(err))
		return nil, nil // fallthrough on any error
	}

	relayID := relayUser.ID
	return &UserInfo{
		Username:    relayUser.Username,
		Email:       relayUser.Email,
		AuthSource:  "relay_sso",
		Role:        "user",
		RelayUserID: &relayID,
	}, nil
}
```

- [x] **Step 3: Verify compilation**

Run: `cd backend && go build ./internal/auth/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add backend/internal/auth/sso.go backend/internal/auth/auth.go
git commit -m "refactor(backend): SSO provider uses relay.Provider instead of sub2apidb"
```

---

### Task 7: Remove sub2apidb Package

**Files:**
- Delete: `backend/internal/sub2apidb/` (entire directory)
- Modify: `backend/go.mod` (remove squirrel if unused)
- Modify: all files importing sub2apidb

- [x] **Step 1: Find all sub2apidb imports**

Run: `cd backend && grep -rn "sub2apidb" --include="*.go"`

Note all files that import or reference sub2apidb.

- [x] **Step 2: Remove sub2apidb references from main.go and other files**

For each file found in Step 1:
- Remove the import of `sub2apidb`
- Remove any initialization code (e.g., `sub2apidb.New(...)`)
- Replace usage with relay.Provider calls (already done for auth/sso.go in Task 6)
- For `efficiency/labeler.go`: replace `sub2apidb.Client` dependency with `relay.Provider`

- [x] **Step 3: Delete the sub2apidb package**

Run: `rm -rf backend/internal/sub2apidb/`

- [x] **Step 4: Remove squirrel dependency**

Run: `cd backend && grep -rn "squirrel" --include="*.go" | grep -v sub2apidb`

If no other files use squirrel:

Run: `cd backend && go mod tidy`

- [x] **Step 5: Verify compilation**

Run: `cd backend && go build ./...`
Expected: no errors (all sub2apidb references removed)

- [x] **Step 6: Commit**

```bash
git add -A backend/
git commit -m "refactor(backend): remove sub2apidb package, replace with relay.Provider"
```

---

### Task 8: LLM Analyzer Refactor

**Files:**
- Modify: `backend/internal/analysis/analyzer.go`

- [x] **Step 1: Identify current LLM call pattern**

Run: `cd backend && grep -rn "Sub2apiURL\|Sub2apiAPIKey\|chat/completions" --include="*.go" internal/analysis/`

Note all direct HTTP calls to sub2api LLM endpoint.

- [x] **Step 2: Replace direct HTTP calls with relay.Provider**

Modify `backend/internal/analysis/analyzer.go`:

- Remove `Sub2apiURL`, `Sub2apiAPIKey` fields from the Analyzer struct
- Add `relayProvider relay.Provider` field
- Replace constructor to accept `relay.Provider`:

```go
// Old:
// type Analyzer struct { sub2apiURL, sub2apiAPIKey, model string; ... }

// New:
type Analyzer struct {
	relayProvider relay.Provider
	model         string
	// ... keep prompt fields, token limits, etc.
}

func NewAnalyzer(relayProvider relay.Provider, model string, ...) *Analyzer {
	return &Analyzer{relayProvider: relayProvider, model: model, ...}
}
```

- Replace the internal `callLLM` / HTTP call with:

```go
func (a *Analyzer) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := a.relayProvider.ChatCompletion(ctx, relay.ChatCompletionRequest{
		Model: a.model,
		Messages: []relay.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("LLM call: %w", err)
	}
	return resp.Content, nil
}
```

- [x] **Step 3: Update ChatHandler if it uses direct LLM calls**

Check `backend/internal/handler/chat.go` — if it calls sub2api directly, refactor to use `relay.Provider.ChatCompletion` or `ChatCompletionWithTools`.

- [x] **Step 4: Verify compilation**

Run: `cd backend && go build ./...`
Expected: no errors

- [x] **Step 5: Commit**

```bash
git add backend/internal/analysis/ backend/internal/handler/
git commit -m "refactor(backend): LLM analyzer uses relay.Provider instead of direct HTTP"
```

---

### Task 9: PKCE Implementation

**Files:**
- Create: `backend/internal/oauth/pkce.go`
- Create: `backend/internal/oauth/pkce_test.go`

- [x] **Step 1: Create oauth package directory**

Run: `mkdir -p backend/internal/oauth`

- [x] **Step 2: Write PKCE test**

Create `backend/internal/oauth/pkce_test.go`:

```go
package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
)

func TestVerifyCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// Compute expected challenge: base64url(SHA256(verifier))
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	if !oauth.VerifyCodeChallenge(verifier, expectedChallenge, "S256") {
		t.Fatal("expected valid challenge to pass")
	}
}

func TestVerifyCodeChallengeMismatch(t *testing.T) {
	if oauth.VerifyCodeChallenge("wrong-verifier", "wrong-challenge", "S256") {
		t.Fatal("expected mismatched challenge to fail")
	}
}

func TestVerifyCodeChallengeUnsupportedMethod(t *testing.T) {
	if oauth.VerifyCodeChallenge("verifier", "challenge", "plain") {
		t.Fatal("expected unsupported method to fail")
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/oauth/ -run TestVerifyCodeChallenge -v`
Expected: FAIL — `VerifyCodeChallenge` not defined

- [x] **Step 4: Implement PKCE verification**

Create `backend/internal/oauth/pkce.go`:

```go
package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// VerifyCodeChallenge verifies a PKCE code_challenge against a code_verifier.
// Only supports S256 method (SHA256 + base64url).
func VerifyCodeChallenge(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}
```

- [x] **Step 5: Run PKCE tests**

Run: `cd backend && go test ./internal/oauth/ -run TestVerifyCodeChallenge -v`
Expected: all PASS

- [x] **Step 6: Commit**

```bash
git add backend/internal/oauth/pkce.go backend/internal/oauth/pkce_test.go
git commit -m "feat(backend): add PKCE code_challenge verification"
```

---

### Task 10: OAuth2 Server Setup

**Files:**
- Create: `backend/internal/oauth/server.go`
- Modify: `backend/go.mod` (add go-oauth2/oauth2)

- [x] **Step 1: Add go-oauth2 dependency**

Run: `cd backend && go get github.com/go-oauth2/oauth2/v4`

- [x] **Step 2: Create OAuth2 server with custom AccessGenerate**

Create `backend/internal/oauth/server.go`:

```go
package oauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/store"
)

// TokenGenerator bridges go-oauth2 token generation to our auth.Service JWT generation.
type TokenGenerator interface {
	GenerateAccessToken(userID int, username, role string) (accessToken, refreshToken string, expiresIn int, err error)
}

// Server wraps go-oauth2 manager and provides OAuth2 functionality.
type Server struct {
	manager    *manage.Manager
	tokenStore oauth2.TokenStore
}

// NewServer creates a new OAuth2 server with in-memory stores.
func NewServer() *Server {
	manager := manage.NewDefaultManager()

	// Token store (in-memory)
	tokenStore, _ := store.NewMemoryTokenStore()
	manager.MapTokenStorage(tokenStore)

	// Client store — ae-cli as pre-registered public client
	clientStore := store.NewClientStore()
	clientStore.Set("ae-cli", &models.Client{
		ID:     "ae-cli",
		Secret: "", // public client
		Public: true,
	})
	manager.MapClientStorage(clientStore)

	// Authorization code config
	manager.SetAuthorizeCodeTokenCfg(&manage.Config{
		AccessTokenExp:    2 * time.Hour,
		RefreshTokenExp:   7 * 24 * time.Hour,
		IsGenerateRefresh: true,
	})

	return &Server{
		manager:    manager,
		tokenStore: tokenStore,
	}
}

// Manager returns the underlying oauth2 manager for handler use.
func (s *Server) Manager() *manage.Manager {
	return s.manager
}

// ValidateRedirectURI checks that the redirect_uri is a valid localhost callback.
// Host must be localhost or 127.0.0.1, port must be numeric, path must be /callback.
func ValidateRedirectURI(rawURI string) bool {
	u, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return false
	}
	port := u.Port()
	if port == "" {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	if u.Path != "/callback" {
		return false
	}
	return true
}
```

- [x] **Step 3: Verify compilation**

Run: `cd backend && go build ./internal/oauth/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add backend/internal/oauth/server.go backend/go.mod backend/go.sum
git commit -m "feat(backend): add OAuth2 server setup with go-oauth2/oauth2"
```

---

### Task 11: OAuth Handler

**Files:**
- Create: `backend/internal/oauth/handler.go`
- Create: `backend/internal/oauth/handler_test.go`

- [x] **Step 1: Write handler test for Authorize endpoint**

Create `backend/internal/oauth/handler_test.go`:

```go
package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/gin-gonic/gin"
)

// mockTokenGen implements oauth.TokenGenerator for testing.
type mockTokenGen struct{}

func (m *mockTokenGen) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	return "test-access-token", "test-refresh-token", 7200, nil
}

func setupTestRouter() (*gin.Engine, *oauth.Handler) {
	gin.SetMode(gin.TestMode)
	oauthServer := oauth.NewServer()
	handler := oauth.NewHandler(oauthServer, "http://localhost:5173", &mockTokenGen{})

	r := gin.New()
	r.GET("/oauth/authorize", handler.Authorize)
	r.POST("/oauth/token", handler.Token)
	return r, handler
}

func TestAuthorizeRedirectsToFrontend(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}
	if !strings.Contains(loc, "localhost:5173/oauth/authorize") {
		t.Fatalf("expected redirect to frontend, got: %s", loc)
	}
}

func TestAuthorizeRejectsInvalidRedirectURI(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://evil.com/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthorizeRejectsUnknownClient(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=unknown&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/oauth/ -run TestAuthorize -v`
Expected: FAIL — `Handler` not defined

- [x] **Step 3: Implement OAuth handler**

Create `backend/internal/oauth/handler.go`:

```go
package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// authCodeEntry stores a pending authorization code with its metadata.
type authCodeEntry struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	UserID        int
	Username      string
	Role          string
	State         string
	CreatedAt     time.Time
}

// Handler handles OAuth2 endpoints.
type Handler struct {
	server      *Server
	frontendURL string

	mu    sync.Mutex
	codes map[string]*authCodeEntry // code → entry
}

// NewHandler creates a new OAuth handler.
func NewHandler(server *Server, frontendURL string) *Handler {
	return &Handler{
		server:      server,
		frontendURL: frontendURL,
		codes:       make(map[string]*authCodeEntry),
	}
}

// Authorize handles GET /oauth/authorize.
// Validates parameters, then 302 redirects to frontend authorize page.
func (h *Handler) Authorize(c *gin.Context) {
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.Query("code_challenge_method")
	state := c.Query("state")
	responseType := c.Query("response_type")

	// Validate required params
	if responseType != "code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_response_type"})
		return
	}
	if clientID != "ae-cli" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}
	if !ValidateRedirectURI(redirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_redirect_uri"})
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_code_challenge"})
		return
	}

	// Redirect to frontend authorize page with all query params
	frontendURL := h.frontendURL + "/oauth/authorize?" + c.Request.URL.RawQuery
	c.Redirect(http.StatusFound, frontendURL)
}

// ApproveRequest is the request body for POST /oauth/authorize/approve.
type ApproveRequest struct {
	ClientID            string `json:"client_id" binding:"required"`
	RedirectURI         string `json:"redirect_uri" binding:"required"`
	CodeChallenge       string `json:"code_challenge" binding:"required"`
	CodeChallengeMethod string `json:"code_challenge_method" binding:"required"`
	State               string `json:"state" binding:"required"`
	Approved            bool   `json:"approved"`
}

// Approve handles POST /oauth/authorize/approve (requires user JWT).
func (h *Handler) Approve(c *gin.Context) {
	var req ApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Re-validate redirect_uri
	if !ValidateRedirectURI(req.RedirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_redirect_uri"})
		return
	}
	if req.ClientID != "ae-cli" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}

	if !req.Approved {
		c.JSON(http.StatusOK, gin.H{
			"redirect_uri": req.RedirectURI + "?error=access_denied&state=" + req.State,
		})
		return
	}

	// Extract user from JWT context (set by RequireAuth middleware)
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")
	role, _ := c.Get("role")

	// Generate authorization code
	code := generateCode()

	h.mu.Lock()
	h.codes[code] = &authCodeEntry{
		Code:          code,
		ClientID:      req.ClientID,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		UserID:        userID.(int),
		Username:      username.(string),
		Role:          role.(string),
		State:         req.State,
		CreatedAt:     time.Now(),
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"redirect_uri": req.RedirectURI + "?code=" + code + "&state=" + req.State,
	})
}

// Token handles POST /oauth/token (authorization code → JWT exchange).
func (h *Handler) Token(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	if grantType != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}

	code := c.PostForm("code")
	redirectURI := c.PostForm("redirect_uri")
	codeVerifier := c.PostForm("code_verifier")
	clientID := c.PostForm("client_id")

	if code == "" || redirectURI == "" || codeVerifier == "" || clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	// Look up and consume the authorization code
	h.mu.Lock()
	entry, ok := h.codes[code]
	if ok {
		delete(h.codes, code) // one-time use
	}
	h.mu.Unlock()

	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code not found or already used"})
		return
	}

	// Validate expiry (5 minutes)
	if time.Since(entry.CreatedAt) > 5*time.Minute {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code expired"})
		return
	}

	// Validate client_id and redirect_uri match
	if entry.ClientID != clientID || entry.RedirectURI != redirectURI {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "client_id or redirect_uri mismatch"})
		return
	}

	// Verify PKCE
	if !VerifyCodeChallenge(codeVerifier, entry.CodeChallenge, "S256") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "PKCE verification failed"})
		return
	}

	// Generate JWT tokens via the injected token generator
	if h.tokenGen == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error", "error_description": "token generator not configured"})
		return
	}

	accessToken, refreshToken, expiresIn, err := h.tokenGen.GenerateAccessToken(entry.UserID, entry.Username, entry.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}

func generateCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

Note: The Handler struct needs a `tokenGen TokenGenerator` field. Update the struct and constructor:

```go
type Handler struct {
	server      *Server
	frontendURL string
	tokenGen    TokenGenerator

	mu    sync.Mutex
	codes map[string]*authCodeEntry
}

func NewHandler(server *Server, frontendURL string, tokenGen TokenGenerator) *Handler {
	return &Handler{
		server:      server,
		frontendURL: frontendURL,
		tokenGen:    tokenGen,
		codes:       make(map[string]*authCodeEntry),
	}
}
```

- [x] **Step 4: Implement TokenGenerator adapter for auth.Service**

Add to `backend/internal/oauth/server.go`:

```go
// AuthServiceAdapter adapts auth.Service to the TokenGenerator interface.
type AuthServiceAdapter struct {
	AuthService interface {
		GenerateTokenPairForUser(info interface{}) (interface{}, error)
	}
}
```

Or more practically, define the adapter in the wiring code (router.go / main.go). The key point is that `auth.Service.GenerateTokenPairForUser` is called inside the adapter.

- [x] **Step 5: Update test to match new constructor**

Update `handler_test.go` to pass a mock TokenGenerator:

```go
type mockTokenGen struct{}

func (m *mockTokenGen) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	return "access-token", "refresh-token", 7200, nil
}

func setupTestRouter() (*gin.Engine, *oauth.Handler) {
	gin.SetMode(gin.TestMode)
	oauthServer := oauth.NewServer()
	handler := oauth.NewHandler(oauthServer, "http://localhost:5173", &mockTokenGen{})
	// ...
}
```

- [x] **Step 6: Run all OAuth handler tests**

Run: `cd backend && go test ./internal/oauth/ -v`
Expected: all PASS

- [x] **Step 7: Commit**

```bash
git add backend/internal/oauth/handler.go backend/internal/oauth/handler_test.go backend/internal/oauth/server.go
git commit -m "feat(backend): implement OAuth2 authorize/token/approve handlers with PKCE"
```

---

### Task 12: Route Registration & Wiring

**Files:**
- Modify: `backend/internal/handler/router.go`

- [x] **Step 1: Update SetupRouter signature to accept new dependencies**

Add `relay.Provider`, `*oauth.Handler`, and `*oauth.Server` to the function signature:

```go
func SetupRouter(
	entClient *ent.Client,
	authService *auth.Service,
	repoService *repo.Service,
	analysisService analysisScanner,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	chatHandler *ChatHandler,
	aggregator *efficiency.Aggregator,
	optimizer optimizerService,
	encryptionKey string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,       // new
	providerHandler *ProviderHandler,   // new
	adminSettingsHandler *AdminSettingsHandler, // new
) *gin.Engine {
```

- [x] **Step 2: Add OAuth routes at root level**

Add before the `api` group:

```go
// OAuth endpoints — at root /oauth/* (not under /api/v1)
r.GET("/oauth/authorize", oauthHandler.Authorize)
r.POST("/oauth/token", oauthHandler.Token)

// OAuth approve requires user JWT
oauthAuth := r.Group("/oauth")
oauthAuth.Use(auth.RequireAuth(authService))
oauthAuth.POST("/authorize/approve", oauthHandler.Approve)
```

- [x] **Step 3: Add provider routes**

Add inside the protected group:

```go
// Providers (ae-cli API key delivery)
protected.GET("/providers", providerHandler.ListForUser)

// Admin provider management
adminProviderGroup := protected.Group("/admin/providers")
adminProviderGroup.Use(auth.RequireAdmin())
{
	adminProviderGroup.GET("", providerHandler.List)
	adminProviderGroup.POST("", providerHandler.Create)
	adminProviderGroup.PUT("/:id", providerHandler.Update)
	adminProviderGroup.DELETE("/:id", providerHandler.Delete)
}
```

- [x] **Step 4: Add LDAP settings routes**

Add inside the protected group:

```go
// LDAP settings — admin only
if adminSettingsHandler != nil {
	ldapGroup := protected.Group("/admin/settings/ldap")
	ldapGroup.Use(auth.RequireAdmin())
	{
		ldapGroup.GET("", adminSettingsHandler.GetLDAP)
		ldapGroup.PUT("", adminSettingsHandler.UpdateLDAP)
		ldapGroup.POST("/test", adminSettingsHandler.TestLDAP)
	}
}
```

- [x] **Step 5: Verify compilation**

Run: `cd backend && go build ./internal/handler/`
Expected: may fail if ProviderHandler/AdminSettingsHandler not yet created — that's OK, will be fixed in Tasks 13-14

- [x] **Step 6: Commit**

```bash
git add backend/internal/handler/router.go
git commit -m "feat(backend): register OAuth, provider, and LDAP settings routes"
```

---

### Task 13: SystemSetting Schema & LDAP Settings Handler

**Files:**
- Create: `backend/ent/schema/system_setting.go`
- Create: `backend/internal/handler/admin_settings.go`

- [x] **Step 1: Create SystemSetting Ent schema**

Create `backend/ent/schema/system_setting.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// SystemSetting holds the schema definition for the SystemSetting entity.
type SystemSetting struct {
	ent.Schema
}

func (SystemSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").
			Unique().
			NotEmpty(),
		field.Text("value").
			Default(""),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}
```

- [x] **Step 2: Run Ent code generation**

Run: `cd backend && go generate ./ent/`
Expected: SystemSetting generated code created

- [x] **Step 3: Write LDAP settings handler test**

Create `backend/internal/handler/admin_settings_test.go`:

```go
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetLDAPReturnsConfig(t *testing.T) {
	// Setup test with in-memory SQLite Ent client
	// GET /api/v1/admin/settings/ldap should return LDAP config with masked password
	gin.SetMode(gin.TestMode)
	// ... test setup with enttest ...

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/ldap", nil)
	// ... add auth header ...
	// router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Verify password is masked as "***"
}
```

- [x] **Step 4: Implement LDAP settings handler**

Create `backend/internal/handler/admin_settings.go`:

```go
package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AdminSettingsHandler handles LDAP configuration management.
type AdminSettingsHandler struct {
	entClient     *ent.Client
	encryptionKey string
	ldapConfig    *atomic.Pointer[config.LDAPConfig]
}

// NewAdminSettingsHandler creates a new admin settings handler.
func NewAdminSettingsHandler(entClient *ent.Client, encryptionKey string, ldapConfig *atomic.Pointer[config.LDAPConfig]) *AdminSettingsHandler {
	return &AdminSettingsHandler{
		entClient:     entClient,
		encryptionKey: encryptionKey,
		ldapConfig:    ldapConfig,
	}
}

type ldapSettingsResponse struct {
	URL          string `json:"url"`
	BaseDN       string `json:"base_dn"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"` // always "***" in response
	UserFilter   string `json:"user_filter"`
	TLS          bool   `json:"tls"`
}

type ldapSettingsRequest struct {
	URL          string `json:"url" binding:"required"`
	BaseDN       string `json:"base_dn" binding:"required"`
	BindDN       string `json:"bind_dn" binding:"required"`
	BindPassword string `json:"bind_password"` // empty = keep existing
	UserFilter   string `json:"user_filter" binding:"required"`
	TLS          bool   `json:"tls"`
}

// GetLDAP handles GET /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) GetLDAP(c *gin.Context) {
	ctx := c.Request.Context()
	settings := make(map[string]string)

	// Load all ldap.* keys from SystemSetting
	rows, err := h.entClient.SystemSetting.Query().
		Where(systemsetting.KeyHasPrefix("ldap.")).
		All(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load LDAP settings")
		return
	}
	for _, row := range rows {
		settings[row.Key] = row.Value
	}

	pkg.Success(c, ldapSettingsResponse{
		URL:          settings["ldap.url"],
		BaseDN:       settings["ldap.base_dn"],
		BindDN:       settings["ldap.bind_dn"],
		BindPassword: "***", // always masked
		UserFilter:   settings["ldap.user_filter"],
		TLS:          settings["ldap.tls"] == "true",
	})
}

// UpdateLDAP handles PUT /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) UpdateLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()

	// Upsert each setting
	pairs := map[string]string{
		"ldap.url":         req.URL,
		"ldap.base_dn":    req.BaseDN,
		"ldap.bind_dn":    req.BindDN,
		"ldap.user_filter": req.UserFilter,
		"ldap.tls":        boolToStr(req.TLS),
	}

	// Handle password: encrypt if provided, skip if empty
	if req.BindPassword != "" && req.BindPassword != "***" {
		encrypted, err := encryptAESGCM(req.BindPassword, h.encryptionKey)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to encrypt password")
			return
		}
		pairs["ldap.bind_password"] = encrypted
	}

	for key, value := range pairs {
		err := h.entClient.SystemSetting.Create().
			SetKey(key).
			SetValue(value).
			OnConflictColumns("key").
			UpdateValue().
			Exec(ctx)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to save setting: "+key)
			return
		}
	}

	// Hot-reload: update atomic pointer
	newCfg := &config.LDAPConfig{
		URL:        req.URL,
		BaseDN:     req.BaseDN,
		BindDN:     req.BindDN,
		UserFilter: req.UserFilter,
		TLS:        req.TLS,
	}
	// Decrypt password for runtime use
	if req.BindPassword != "" && req.BindPassword != "***" {
		newCfg.BindPassword = req.BindPassword
	} else {
		// Keep existing password from current config
		if current := h.ldapConfig.Load(); current != nil {
			newCfg.BindPassword = current.BindPassword
		}
	}
	h.ldapConfig.Store(newCfg)

	pkg.Success(c, gin.H{"message": "LDAP settings updated"})
}

// TestLDAP handles POST /api/v1/admin/settings/ldap/test
func (h *AdminSettingsHandler) TestLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Use request body config (not saved config) for testing
	// Attempt LDAP bind with provided credentials
	// ... LDAP connection test logic ...

	pkg.Success(c, gin.H{"message": "LDAP connection successful"})
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func encryptAESGCM(plaintext, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}
```

- [x] **Step 5: Run tests**

Run: `cd backend && go test ./internal/handler/ -run TestGetLDAP -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add backend/ent/schema/system_setting.go backend/ent/ backend/internal/handler/admin_settings.go
git commit -m "feat(backend): add SystemSetting schema and LDAP settings handler with AES-256-GCM encryption"
```

---

### Task 14: RelayProvider Schema & Provider Handler

**Files:**
- Create: `backend/ent/schema/relay_provider.go`
- Create: `backend/internal/handler/provider.go`

- [x] **Step 1: Create RelayProvider Ent schema**

Create `backend/ent/schema/relay_provider.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// RelayProvider holds the schema definition for the RelayProvider entity.
type RelayProvider struct {
	ent.Schema
}

func (RelayProvider) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Unique().
			NotEmpty(),
		field.String("display_name").
			NotEmpty(),
		field.String("base_url").
			NotEmpty(),
		field.String("admin_url").
			NotEmpty(),
		field.String("relay_type").
			Default("sub2api"),
		field.String("admin_api_key").
			Sensitive(),
		field.String("default_model").
			Default("claude-sonnet-4-20250514"),
		field.Bool("is_primary").
			Default(false),
		field.Bool("enabled").
			Default(true),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}
```

- [x] **Step 2: Run Ent code generation**

Run: `cd backend && go generate ./ent/`
Expected: RelayProvider generated code created

- [x] **Step 3: Write provider handler test for ListForUser**

Create `backend/internal/handler/provider_test.go`:

```go
package handler_test

import (
	"testing"
)

func TestListForUserReturnsProviders(t *testing.T) {
	// Setup: create a RelayProvider in DB, create a user
	// Call GET /api/v1/providers with user JWT
	// Expect: provider list with api_key populated
	// The handler should call relay.Provider.ListUserAPIKeys / CreateUserAPIKey
}
```

- [x] **Step 4: Implement provider handler**

Create `backend/internal/handler/provider.go`:

```go
package handler

import (
	"net/http"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ProviderHandler handles relay provider management and API key delivery.
type ProviderHandler struct {
	entClient     *ent.Client
	encryptionKey string
	logger        *zap.Logger

	// Cached relay.Provider instances keyed by RelayProvider.ID.
	// Rebuilt when provider config changes (Create/Update/Delete).
	mu             sync.RWMutex
	providerCache  map[int]relay.Provider
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(entClient *ent.Client, encryptionKey string, logger *zap.Logger) *ProviderHandler {
	return &ProviderHandler{
		entClient:     entClient,
		encryptionKey: encryptionKey,
		logger:        logger,
		providerCache: make(map[int]relay.Provider),
	}
}

// getOrCreateRelayProvider returns a cached relay.Provider for the given DB record.
func (h *ProviderHandler) getOrCreateRelayProvider(p *ent.RelayProvider) relay.Provider {
	h.mu.RLock()
	rp, ok := h.providerCache[p.ID]
	h.mu.RUnlock()
	if ok {
		return rp
	}

	// Decrypt admin_api_key before use
	adminKey, err := decryptAESGCM(p.AdminAPIKey, h.encryptionKey)
	if err != nil {
		h.logger.Error("failed to decrypt admin_api_key", zap.String("provider", p.Name), zap.Error(err))
		adminKey = p.AdminAPIKey // fallback to raw value (may be unencrypted in dev)
	}

	rp = relay.NewSub2apiProvider(
		http.DefaultClient,
		p.BaseURL,
		p.AdminURL,
		adminKey,
		p.DefaultModel,
		h.logger,
	)

	h.mu.Lock()
	h.providerCache[p.ID] = rp
	h.mu.Unlock()
	return rp
}

// invalidateCache clears the provider cache (called after Create/Update/Delete).
func (h *ProviderHandler) invalidateCache() {
	h.mu.Lock()
	h.providerCache = make(map[int]relay.Provider)
	h.mu.Unlock()
}

type providerResponse struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	APIKeyID     int64  `json:"api_key_id"`
	DefaultModel string `json:"default_model"`
	IsPrimary    bool   `json:"is_primary"`
}

// ListForUser handles GET /api/v1/providers — returns providers with user's API keys.
func (h *ProviderHandler) ListForUser(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user from JWT context
	userID, _ := c.Get("user_id")

	// Get user's relay_user_id
	user, err := h.entClient.User.Get(ctx, userID.(int))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get user")
		return
	}

	// LDAP-only user without relay account — try FindUserByEmail fallback
	if user.RelayUserID == nil {
		// Attempt to find relay account by email
		primaryProvider, err := h.entClient.RelayProvider.Query().
			Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
			First(ctx)
		if err != nil || primaryProvider == nil {
			pkg.Success(c, gin.H{"providers": []providerResponse{}})
			return
		}
		rp := h.getOrCreateRelayProvider(primaryProvider)
		relayUser, err := rp.FindUserByEmail(ctx, user.Email)
		if err != nil || relayUser == nil {
			// No relay account found — return empty with hint
			pkg.Success(c, gin.H{
				"providers": []providerResponse{},
				"message":   "当前账号未关联 relay server，无法自动配置 AI 工具。请联系管理员。",
			})
			return
		}
		// Associate relay_user_id and persist
		relayID := int(relayUser.ID)
		h.entClient.User.UpdateOneID(user.ID).SetRelayUserID(relayID).Save(ctx)
		user.RelayUserID = &relayID
	}

	// Query all enabled providers
	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list providers")
		return
	}

	var result []providerResponse
	for _, p := range providers {
		rp := h.getOrCreateRelayProvider(p)

		// Always create a new API key — the secret is only available at creation time.
		// sub2api does not expose secrets for existing keys via list endpoint.
		newKey, err := rp.CreateUserAPIKey(ctx, int64(*user.RelayUserID), "ae-cli-auto")
		if err != nil {
			h.logger.Warn("failed to create API key", zap.String("provider", p.Name), zap.Error(err))
			continue
		}

		result = append(result, providerResponse{
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			APIKey:       newKey.Secret,
			APIKeyID:     newKey.ID,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
		})
	}

	pkg.Success(c, gin.H{"providers": result})
}

// List handles GET /api/v1/admin/providers — admin list all providers.
func (h *ProviderHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	providers, err := h.entClient.RelayProvider.Query().All(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list providers")
		return
	}
	// Mask admin_api_key in response
	type adminProviderResponse struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		DisplayName  string `json:"display_name"`
		BaseURL      string `json:"base_url"`
		AdminURL     string `json:"admin_url"`
		RelayType    string `json:"relay_type"`
		AdminAPIKey  string `json:"admin_api_key"`
		DefaultModel string `json:"default_model"`
		IsPrimary    bool   `json:"is_primary"`
		Enabled      bool   `json:"enabled"`
	}
	var result []adminProviderResponse
	for _, p := range providers {
		result = append(result, adminProviderResponse{
			ID:           p.ID,
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			AdminURL:     p.AdminURL,
			RelayType:    p.RelayType,
			AdminAPIKey:  "***",
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
			Enabled:      p.Enabled,
		})
	}
	pkg.Success(c, result)
}

// Create handles POST /api/v1/admin/providers
func (h *ProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		DisplayName  string `json:"display_name" binding:"required"`
		BaseURL      string `json:"base_url" binding:"required"`
		AdminURL     string `json:"admin_url" binding:"required"`
		RelayType    string `json:"relay_type"`
		AdminAPIKey  string `json:"admin_api_key" binding:"required"`
		DefaultModel string `json:"default_model"`
		IsPrimary    bool   `json:"is_primary"`
		Enabled      bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	create := h.entClient.RelayProvider.Create().
		SetName(req.Name).
		SetDisplayName(req.DisplayName).
		SetBaseURL(req.BaseURL).
		SetAdminURL(req.AdminURL).
		SetAdminAPIKey(req.AdminAPIKey).
		SetIsPrimary(req.IsPrimary).
		SetEnabled(req.Enabled)

	if req.RelayType != "" {
		create.SetRelayType(req.RelayType)
	}
	if req.DefaultModel != "" {
		create.SetDefaultModel(req.DefaultModel)
	}

	p, err := create.Save(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create provider")
		return
	}
	pkg.Created(c, p)
}

// Update handles PUT /api/v1/admin/providers/:id
func (h *ProviderHandler) Update(c *gin.Context) {
	// Parse ID, bind JSON, update fields
	// Similar pattern to Create
	pkg.Success(c, gin.H{"message": "updated"})
}

// Delete handles DELETE /api/v1/admin/providers/:id
func (h *ProviderHandler) Delete(c *gin.Context) {
	// Parse ID, delete from DB
	pkg.Success(c, gin.H{"message": "deleted"})
}
```

- [x] **Step 5: Run tests**

Run: `cd backend && go test ./internal/handler/ -run TestListForUser -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add backend/ent/schema/relay_provider.go backend/ent/ backend/internal/handler/provider.go
git commit -m "feat(backend): add RelayProvider schema and provider handler with API key delivery"
```

---

### Task 15: Session Handler Update

**Files:**
- Modify: `backend/internal/handler/session.go`

- [x] **Step 1: Update createSessionRequest to include tool_configs**

Modify `backend/internal/handler/session.go`:

```go
type createSessionRequest struct {
	ID           string                   `json:"id" binding:"required"`
	RepoFullName string                   `json:"repo_full_name" binding:"required"`
	Branch       string                   `json:"branch" binding:"required"`
	ToolConfigs  []map[string]interface{} `json:"tool_configs"`
}
```

- [x] **Step 2: Update Create handler to extract user_id from JWT and set tool_configs**

```go
func (h *SessionHandler) Create(c *gin.Context) {
	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := uuid.Parse(req.ID)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id: must be UUID")
		return
	}

	// Resolve repo_config_id
	rc, err := h.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(req.RepoFullName)).
		Only(c.Request.Context())
	if err != nil && ent.IsNotFound(err) {
		rc, err = h.entClient.RepoConfig.Query().
			Where(repoconfig.CloneURLEQ(req.RepoFullName)).
			Only(c.Request.Context())
	}
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "repo not found: "+req.RepoFullName)
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to find repo")
		return
	}

	// Extract user_id from JWT context
	create := h.entClient.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(rc.ID).
		SetBranch(req.Branch).
		SetStartedAt(time.Now())

	// Set user edge from JWT
	if userID, exists := c.Get("user_id"); exists {
		create.SetUserID(userID.(int))
	}

	// Set tool_configs if provided
	if len(req.ToolConfigs) > 0 {
		create.SetToolConfigs(req.ToolConfigs)
		// Extract provider_name and relay_api_key_id from first tool_config
		if tc := req.ToolConfigs[0]; tc != nil {
			if pn, ok := tc["provider_name"].(string); ok {
				create.SetProviderName(pn)
			}
			if keyID, ok := tc["relay_api_key_id"].(float64); ok {
				create.SetRelayAPIKeyID(int(keyID))
			}
		}
	}

	s, err := create.Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create session")
		return
	}

	pkg.Created(c, s)
}
```

- [x] **Step 3: Verify compilation**

Run: `cd backend && go build ./internal/handler/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add backend/internal/handler/session.go
git commit -m "feat(backend): session handler extracts user_id from JWT, accepts tool_configs"
```

---

### Task 16: ae-cli BuildInfo

**Files:**
- Create: `ae-cli/internal/buildinfo/buildinfo.go`

- [x] **Step 1: Create buildinfo package**

Run: `mkdir -p ae-cli/internal/buildinfo`

- [x] **Step 2: Write buildinfo.go**

Create `ae-cli/internal/buildinfo/buildinfo.go`:

```go
package buildinfo

// These variables are set at build time via -ldflags.
// Example: go build -ldflags "-X ae-cli/internal/buildinfo.ServerURL=https://ae.example.com"
var (
	ServerURL = "http://localhost:8081"
	Version   = "dev"
)
```

- [x] **Step 3: Verify compilation**

Run: `cd ae-cli && go build ./internal/buildinfo/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add ae-cli/internal/buildinfo/buildinfo.go
git commit -m "feat(ae-cli): add buildinfo package for compile-time server URL injection"
```

---

### Task 17: ae-cli Token Management

**Files:**
- Create: `ae-cli/internal/auth/token.go`
- Create: `ae-cli/internal/auth/token_test.go`

- [x] **Step 1: Create auth package directory**

Run: `mkdir -p ae-cli/internal/auth`

- [x] **Step 2: Write token test**

Create `ae-cli/internal/auth/token_test.go`:

```go
package auth_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestWriteAndReadToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}

	if err := auth.WriteToken(path, token); err != nil {
		t.Fatalf("WriteToken failed: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	got, err := auth.ReadToken(path)
	if err != nil {
		t.Fatalf("ReadToken failed: %v", err)
	}
	if got.AccessToken != token.AccessToken {
		t.Fatalf("access token mismatch: %s != %s", got.AccessToken, token.AccessToken)
	}
}

func TestReadTokenNotFound(t *testing.T) {
	_, err := auth.ReadToken("/nonexistent/token.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestTokenIsValid(t *testing.T) {
	valid := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if !valid.IsValid() {
		t.Fatal("expected valid token")
	}

	expired := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	if expired.IsValid() {
		t.Fatal("expected expired token to be invalid")
	}
}

func TestTokenNeedsRefresh(t *testing.T) {
	// Expires in 3 minutes — should need refresh (threshold is 5 min)
	soon := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(3 * time.Minute),
	}
	if !soon.NeedsRefresh() {
		t.Fatal("expected token expiring in 3min to need refresh")
	}

	// Expires in 1 hour — should not need refresh
	later := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if later.NeedsRefresh() {
		t.Fatal("expected token expiring in 1h to not need refresh")
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd ae-cli && go test ./internal/auth/ -run TestWriteAndReadToken -v`
Expected: FAIL — package not found

- [x] **Step 4: Implement token management**

Create `ae-cli/internal/auth/token.go`:

```go
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenFile represents the stored OAuth token.
type TokenFile struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	ServerURL    string    `json:"server_url"`
}

// DefaultTokenPath returns ~/.ae-cli/token.json.
func DefaultTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "token.json"), nil
}

// IsValid returns true if the token exists and hasn't expired.
func (t *TokenFile) IsValid() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt)
}

// NeedsRefresh returns true if the token expires within 5 minutes.
func (t *TokenFile) NeedsRefresh() bool {
	return time.Until(t.ExpiresAt) < 5*time.Minute
}

// ReadToken reads and parses the token file.
func ReadToken(path string) (*TokenFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	var token TokenFile
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &token, nil
}

// WriteToken atomically writes the token file with 0600 permissions.
func WriteToken(path string, token *TokenFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename token file: %w", err)
	}
	return nil
}

// DeleteToken removes the token file.
func DeleteToken(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}
```

- [x] **Step 5: Run token tests**

Run: `cd ae-cli && go test ./internal/auth/ -v`
Expected: all PASS

- [x] **Step 6: Commit**

```bash
git add ae-cli/internal/auth/token.go ae-cli/internal/auth/token_test.go
git commit -m "feat(ae-cli): add token file management with atomic write and auto-refresh detection"
```

---

### Task 18: ae-cli OAuth Login Flow

**Files:**
- Create: `ae-cli/internal/auth/oauth.go`
- Create: `ae-cli/internal/auth/oauth_test.go`
- Modify: `ae-cli/go.mod` (add golang.org/x/oauth2)

- [x] **Step 1: Add golang.org/x/oauth2 dependency**

Run: `cd ae-cli && go get golang.org/x/oauth2`

- [x] **Step 2: Write OAuth flow test**

Create `ae-cli/internal/auth/oauth_test.go`:

```go
package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestOAuthFlowExchangesCode(t *testing.T) {
	// Mock backend OAuth token endpoint
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" && r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "test-access-token",
				"refresh_token": "test-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	// Test the code exchange function directly
	token, err := auth.ExchangeCode(context.Background(), backend.URL, "test-code", "http://localhost:12345/callback", "test-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if token.AccessToken != "test-access-token" {
		t.Fatalf("unexpected access token: %s", token.AccessToken)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd ae-cli && go test ./internal/auth/ -run TestOAuthFlowExchangesCode -v`
Expected: FAIL — `ExchangeCode` not defined

- [x] **Step 4: Implement OAuth login flow**

Create `ae-cli/internal/auth/oauth.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OAuthConfig holds the OAuth login configuration.
type OAuthConfig struct {
	ServerURL string
	ClientID  string
	Timeout   time.Duration // default 3 minutes
}

// OAuthResult contains the result of a successful OAuth login.
type OAuthResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// Login performs the full OAuth2 Authorization Code Flow with PKCE.
func Login(ctx context.Context, cfg OAuthConfig) (*OAuthResult, error) {
	if cfg.ClientID == "" {
		cfg.ClientID = "ae-cli"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Minute
	}

	// Generate PKCE verifier and challenge
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}

	// Generate state
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	// Start local HTTP server on random port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("start local server: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Channel to receive the authorization code
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization denied: %s", errMsg)
			fmt.Fprintf(w, "<html><body><h1>Authorization denied</h1><p>You can close this window.</p></body></html>")
			return
		}

		receivedState := r.URL.Query().Get("state")
		if receivedState != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}

		codeCh <- code
		fmt.Fprintf(w, "<html><body><h1>Login successful!</h1><p>You can close this window and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	// Build authorization URL
	authURL := fmt.Sprintf("%s/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		cfg.ServerURL,
		url.QueryEscape(cfg.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(challenge),
		url.QueryEscape(state),
	)

	// Open browser
	fmt.Printf("Opening browser for login...\n")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	// Wait for callback or timeout
	fmt.Printf("Waiting for authorization (timeout: %s)...\n", cfg.Timeout)
	select {
	case code := <-codeCh:
		return ExchangeCode(ctx, cfg.ServerURL, code, redirectURI, verifier)
	case err := <-errCh:
		return nil, err
	case <-time.After(cfg.Timeout):
		return nil, fmt.Errorf("login timed out after %s", cfg.Timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(ctx context.Context, serverURL, code, redirectURI, verifier string) (*OAuthResult, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {"ae-cli"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("token exchange failed: %s — %s", errResp["error"], errResp["error_description"])
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &OAuthResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

// RefreshAccessToken refreshes the access token using the existing refresh endpoint.
func RefreshAccessToken(ctx context.Context, serverURL, refreshToken string) (*OAuthResult, error) {
	body := fmt.Sprintf(`{"refresh_token":%q}`, refreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/auth/refresh", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed with status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	return &OAuthResult{
		AccessToken:  result.Data.AccessToken,
		RefreshToken: result.Data.RefreshToken,
		ExpiresIn:    result.Data.ExpiresIn,
	}, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
```

- [x] **Step 5: Run OAuth tests**

Run: `cd ae-cli && go test ./internal/auth/ -v`
Expected: all PASS

- [x] **Step 6: Commit**

```bash
git add ae-cli/internal/auth/oauth.go ae-cli/internal/auth/oauth_test.go ae-cli/go.mod ae-cli/go.sum
git commit -m "feat(ae-cli): implement OAuth2 login flow with PKCE and token refresh"
```

---

### Task 19: ae-cli Login/Logout Commands

**Files:**
- Create: `ae-cli/cmd/login.go`
- Create: `ae-cli/cmd/logout.go`

- [x] **Step 1: Create login command**

Create `ae-cli/cmd/login.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var loginForce bool

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to ae-cli via OAuth",
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return err
		}

		// Check existing token
		if !loginForce {
			if token, err := auth.ReadToken(tokenPath); err == nil && token.IsValid() {
				fmt.Println("Already logged in. Use --force to re-login.")
				return nil
			}
		}

		// Run OAuth flow
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		result, err := auth.Login(ctx, auth.OAuthConfig{
			ServerURL: buildinfo.ServerURL,
		})
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		// Save token
		token := &auth.TokenFile{
			AccessToken:  result.AccessToken,
			RefreshToken: result.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
			ServerURL:    buildinfo.ServerURL,
		}
		if err := auth.WriteToken(tokenPath, token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}

		fmt.Println("Login successful!")
		return nil
	},
}

func init() {
	loginCmd.Flags().BoolVar(&loginForce, "force", false, "Force re-login even if already logged in")
	rootCmd.AddCommand(loginCmd)
}
```

- [x] **Step 2: Create logout command**

Create `ae-cli/cmd/logout.go`:

```go
package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Logout from ae-cli",
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return err
		}

		if err := auth.DeleteToken(tokenPath); err != nil {
			return fmt.Errorf("logout failed: %w", err)
		}

		fmt.Println("Logged out successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
```

- [x] **Step 3: Verify compilation**

Run: `cd ae-cli && go build ./cmd/...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add ae-cli/cmd/login.go ae-cli/cmd/logout.go
git commit -m "feat(ae-cli): add login and logout commands"
```

---

### Task 20: ae-cli Config Simplification

**Files:**
- Modify: `ae-cli/config/config.go`
- Modify: `ae-cli/cmd/root.go`

- [x] **Step 1: Simplify ae-cli config**

Replace `ae-cli/config/config.go` with minimal config:

```go
package config

// Config is intentionally minimal after OAuth migration.
// Server URL: compile-time injected via buildinfo.ServerURL
// Token: ~/.ae-cli/token.json (managed by auth package)
// Provider config: fetched from backend via GET /api/v1/providers
// Tool config: handled by Spec 2 (LLM tool discovery)
type Config struct{}
```

- [x] **Step 2: Update root.go to remove config loading**

Modify `ae-cli/cmd/root.go`:
- Remove `config.Load()` call from `PersistentPreRunE`
- Remove `--config` flag
- Remove viper dependency
- Keep cobra root command structure

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "ae-cli",
	Short:   "AI Efficiency CLI",
	Version: buildinfo.Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [x] **Step 3: Verify compilation**

Run: `cd ae-cli && go build ./...`
Expected: compilation errors from shell.go and other files referencing old config — noted for next steps

- [x] **Step 4: Commit**

```bash
git add ae-cli/config/config.go ae-cli/cmd/root.go
git commit -m "refactor(ae-cli): simplify config — remove ToolConfig, Sub2apiConfig, ServerConfig"
```

---

### Task 21: ae-cli Client Refactor

**Files:**
- Modify: `ae-cli/internal/client/client.go`

- [x] **Step 1: Refactor HTTP client to use buildinfo + token.json**

Replace `ae-cli/internal/client/client.go`:

```go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
)

// Client is the ae-cli HTTP client for backend API calls.
type Client struct {
	baseURL    string
	httpClient *http.Client
	tokenPath  string
}

// New creates a new API client using buildinfo.ServerURL and token.json.
func New() (*Client, error) {
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:    buildinfo.ServerURL + "/api/v1",
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokenPath:  tokenPath,
	}, nil
}

// getToken reads and auto-refreshes the token.
func (c *Client) getToken(ctx context.Context) (string, error) {
	token, err := auth.ReadToken(c.tokenPath)
	if err != nil {
		return "", fmt.Errorf("not logged in — run 'ae-cli login' first: %w", err)
	}

	if !token.IsValid() {
		return "", fmt.Errorf("token expired — run 'ae-cli login'")
	}

	// Auto-refresh if needed
	if token.NeedsRefresh() {
		result, err := auth.RefreshAccessToken(ctx, token.ServerURL, token.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("token refresh failed — run 'ae-cli login': %w", err)
		}
		token.AccessToken = result.AccessToken
		token.RefreshToken = result.RefreshToken
		token.ExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
		if err := auth.WriteToken(c.tokenPath, token); err != nil {
			// Non-fatal: token still valid for this request
		}
	}

	// Check server URL matches
	if token.ServerURL != buildinfo.ServerURL {
		return "", fmt.Errorf("token was issued for %s but current server is %s — run 'ae-cli login'", token.ServerURL, buildinfo.ServerURL)
	}

	return token.AccessToken, nil
}

// doRequest performs an authenticated API request.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	var reqBody *bytes.Buffer
	if body != nil {
		reqBody = &bytes.Buffer{}
		if err := json.NewEncoder(reqBody).Encode(body); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}

	var req *http.Request
	if reqBody != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// CreateSession creates a new session.
func (c *Client) CreateSession(ctx context.Context, id, repoFullName, branch string, toolConfigs []map[string]interface{}) error {
	body := map[string]interface{}{
		"id":             id,
		"repo_full_name": repoFullName,
		"branch":         branch,
	}
	if len(toolConfigs) > 0 {
		body["tool_configs"] = toolConfigs
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/sessions", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create session failed: status %d", resp.StatusCode)
	}
	return nil
}

// Heartbeat sends a session heartbeat.
func (c *Client) Heartbeat(ctx context.Context, sessionID string) error {
	resp, err := c.doRequest(ctx, http.MethodPut, "/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// StopSession stops a session.
func (c *Client) StopSession(ctx context.Context, sessionID string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/sessions/"+sessionID+"/stop", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
```

- [x] **Step 2: Verify compilation**

Run: `cd ae-cli && go build ./internal/client/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add ae-cli/internal/client/client.go
git commit -m "refactor(ae-cli): client uses buildinfo.ServerURL + token.json with auto-refresh"
```

---

### Task 22: ae-cli Start Auto-Login & Shell Cleanup

**Files:**
- Modify: `ae-cli/cmd/start.go`
- Modify: `ae-cli/internal/shell/shell.go`

- [x] **Step 1: Update start command to auto-trigger login**

Modify `ae-cli/cmd/start.go` — add token check before session creation:

```go
// At the beginning of the start command's RunE:
tokenPath, err := auth.DefaultTokenPath()
if err != nil {
	return err
}

// Check token validity, auto-login if needed
token, err := auth.ReadToken(tokenPath)
if err != nil || !token.IsValid() {
	fmt.Println("Not logged in. Starting OAuth login...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, loginErr := auth.Login(ctx, auth.OAuthConfig{
		ServerURL: buildinfo.ServerURL,
	})
	if loginErr != nil {
		return fmt.Errorf("login required: %w", loginErr)
	}
	newToken := &auth.TokenFile{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		ServerURL:    buildinfo.ServerURL,
	}
	if err := auth.WriteToken(tokenPath, newToken); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	fmt.Println("Login successful!")
}

// Then create client and session using client.New()
apiClient, err := client.New()
if err != nil {
	return err
}
```

- [x] **Step 2: Update shell.go to remove config.Tools dependency**

Modify `ae-cli/internal/shell/shell.go`:
- Remove `config *config.Config` field
- Remove tool iteration from `New()` and `toolNames()`
- The shell will be refactored further in Spec 2 (tool discovery)
- For now, make it compile without config.Tools

```go
type Shell struct {
	state     *session.State
	router    *router.Router
	toolPanes map[string]string
}

func New(state *session.State) *Shell {
	return &Shell{
		state:     state,
		toolPanes: make(map[string]string),
	}
}
```

Note: This is a temporary simplification. Spec 2 will add LLM-based tool discovery to replace the static config.Tools approach.

- [x] **Step 3: Fix all remaining compilation errors**

Run: `cd ae-cli && go build ./...`

Fix any remaining references to old config types across the codebase.

- [x] **Step 4: Verify full ae-cli compilation**

Run: `cd ae-cli && go build ./...`
Expected: no errors

- [x] **Step 5: Commit**

```bash
git add ae-cli/
git commit -m "feat(ae-cli): start command auto-triggers login, remove config.Tools dependency"
```

---

### Task 23: Frontend OAuth Authorize Page

**Files:**
- Create: `frontend/src/views/oauth/AuthorizePage.vue`
- Create: `frontend/src/api/oauth.ts`
- Modify: `frontend/src/router/index.ts`

- [x] **Step 1: Create OAuth API module**

Create `frontend/src/api/oauth.ts`:

```typescript
import client from './client'

export interface ApproveRequest {
  client_id: string
  redirect_uri: string
  code_challenge: string
  code_challenge_method: string
  state: string
  approved: boolean
}

export interface ApproveResponse {
  redirect_uri: string
}

export async function approveAuthorization(req: ApproveRequest): Promise<ApproveResponse> {
  const { data } = await client.post('/oauth/authorize/approve', req)
  return data.data
}
```

Note: The approve endpoint is at `/oauth/authorize/approve` (not under `/api/v1`), so the client base URL needs adjustment. Either use a separate axios instance or construct the full URL:

```typescript
import axios from 'axios'

const oauthClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL?.replace('/api/v1', '') || '',
})

// Add auth interceptor
oauthClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

export async function approveAuthorization(req: ApproveRequest): Promise<ApproveResponse> {
  const { data } = await oauthClient.post('/oauth/authorize/approve', req)
  return data
}
```

- [x] **Step 2: Create OAuth authorize page**

Create `frontend/src/views/oauth/AuthorizePage.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { approveAuthorization } from '@/api/oauth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const loading = ref(true)
const error = ref('')
const isLoggedIn = ref(false)

// OAuth params from query string
const clientId = route.query.client_id as string
const redirectUri = route.query.redirect_uri as string
const codeChallenge = route.query.code_challenge as string
const codeChallengeMethod = route.query.code_challenge_method as string
const state = route.query.state as string

onMounted(async () => {
  // Check if user is logged in
  try {
    await authStore.fetchMe()
    isLoggedIn.value = true
  } catch {
    isLoggedIn.value = false
  }
  loading.value = false
})

async function handleApprove() {
  try {
    loading.value = true
    const result = await approveAuthorization({
      client_id: clientId,
      redirect_uri: redirectUri,
      code_challenge: codeChallenge,
      code_challenge_method: codeChallengeMethod,
      state: state,
      approved: true,
    })
    // Redirect to ae-cli callback
    window.location.href = result.redirect_uri
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Authorization failed'
    loading.value = false
  }
}

function handleDeny() {
  window.location.href = `${redirectUri}?error=access_denied&state=${state}`
}

async function handleLogin(username: string, password: string) {
  try {
    await authStore.login(username, password)
    isLoggedIn.value = true
  } catch (e: any) {
    error.value = e.response?.data?.message || 'Login failed'
  }
}

// Login form state
const loginUsername = ref('')
const loginPassword = ref('')
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="max-w-md w-full bg-white rounded-lg shadow-md p-8">
      <!-- Loading -->
      <div v-if="loading" class="text-center text-gray-500">
        Loading...
      </div>

      <!-- Login form (not logged in) -->
      <div v-else-if="!isLoggedIn">
        <h2 class="text-xl font-semibold text-center mb-6">Login to authorize ae-cli</h2>
        <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-600 rounded text-sm">
          {{ error }}
        </div>
        <form @submit.prevent="handleLogin(loginUsername, loginPassword)" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Username</label>
            <input v-model="loginUsername" type="text" required
              class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
            <input v-model="loginPassword" type="password" required
              class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
          </div>
          <button type="submit"
            class="w-full py-2 px-4 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition">
            Login
          </button>
        </form>
      </div>

      <!-- Authorization confirmation (logged in) -->
      <div v-else>
        <h2 class="text-xl font-semibold text-center mb-2">Authorize ae-cli</h2>
        <p class="text-gray-500 text-center mb-6">ae-cli is requesting access to your account</p>

        <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-600 rounded text-sm">
          {{ error }}
        </div>

        <div class="space-y-3">
          <button @click="handleApprove"
            class="w-full py-2 px-4 bg-green-600 text-white rounded-md hover:bg-green-700 transition">
            Authorize
          </button>
          <button @click="handleDeny"
            class="w-full py-2 px-4 bg-gray-200 text-gray-700 rounded-md hover:bg-gray-300 transition">
            Deny
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
```

- [x] **Step 3: Add route to Vue Router**

Modify `frontend/src/router/index.ts` — add the OAuth authorize route as a public route:

```typescript
{
  path: '/oauth/authorize',
  name: 'OAuthAuthorize',
  component: () => import('@/views/oauth/AuthorizePage.vue'),
  meta: { public: true },
},
```

Update the auth guard to skip public routes:

```typescript
router.beforeEach((to, from, next) => {
  if (to.meta.public) {
    next()
    return
  }
  // ... existing auth guard logic
})
```

- [x] **Step 4: Verify frontend builds**

Run: `cd frontend && npm run build`
Expected: no errors

- [x] **Step 5: Commit**

```bash
git add frontend/src/views/oauth/ frontend/src/api/oauth.ts frontend/src/router/index.ts
git commit -m "feat(frontend): add OAuth authorize page with login and authorization flow"
```

---

### Task 24: Frontend LDAP Settings Page

**Files:**
- Create: `frontend/src/views/admin/LdapSettingsView.vue`
- Modify: `frontend/src/router/index.ts`

- [x] **Step 1: Create LDAP settings page**

Create `frontend/src/views/admin/LdapSettingsView.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'

interface LdapConfig {
  url: string
  base_dn: string
  bind_dn: string
  bind_password: string
  user_filter: string
  tls: boolean
}

const config = ref<LdapConfig>({
  url: '',
  base_dn: '',
  bind_dn: '',
  bind_password: '',
  user_filter: '(uid=%s)',
  tls: false,
})

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const message = ref('')
const messageType = ref<'success' | 'error'>('success')

onMounted(async () => {
  loading.value = true
  try {
    const { data } = await client.get('/admin/settings/ldap')
    config.value = data.data
  } catch (e: any) {
    message.value = 'Failed to load LDAP settings'
    messageType.value = 'error'
  }
  loading.value = false
})

async function save() {
  saving.value = true
  message.value = ''
  try {
    await client.put('/admin/settings/ldap', config.value)
    message.value = 'Settings saved successfully'
    messageType.value = 'success'
  } catch (e: any) {
    message.value = e.response?.data?.message || 'Failed to save'
    messageType.value = 'error'
  }
  saving.value = false
}

async function testConnection() {
  testing.value = true
  message.value = ''
  try {
    await client.post('/admin/settings/ldap/test', config.value)
    message.value = 'LDAP connection successful'
    messageType.value = 'success'
  } catch (e: any) {
    message.value = e.response?.data?.message || 'Connection test failed'
    messageType.value = 'error'
  }
  testing.value = false
}
</script>

<template>
  <div class="max-w-2xl mx-auto p-6">
    <h1 class="text-2xl font-semibold mb-6">LDAP Settings</h1>

    <div v-if="loading" class="text-gray-500">Loading...</div>

    <form v-else @submit.prevent="save" class="space-y-4">
      <div v-if="message" :class="[
        'p-3 rounded text-sm',
        messageType === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600'
      ]">
        {{ message }}
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">LDAP URL</label>
        <input v-model="config.url" type="text" placeholder="ldap://ldap.example.com:389"
          class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Base DN</label>
        <input v-model="config.base_dn" type="text" placeholder="dc=example,dc=com"
          class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Bind DN</label>
        <input v-model="config.bind_dn" type="text" placeholder="cn=admin,dc=example,dc=com"
          class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Bind Password</label>
        <input v-model="config.bind_password" type="password" placeholder="Leave empty to keep current"
          class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">User Filter</label>
        <input v-model="config.user_filter" type="text" placeholder="(uid=%s)"
          class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>

      <div class="flex items-center">
        <input v-model="config.tls" type="checkbox" id="tls"
          class="h-4 w-4 text-blue-600 border-gray-300 rounded" />
        <label for="tls" class="ml-2 text-sm text-gray-700">Enable TLS</label>
      </div>

      <div class="flex gap-3 pt-4">
        <button type="submit" :disabled="saving"
          class="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50 transition">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button type="button" @click="testConnection" :disabled="testing"
          class="px-4 py-2 bg-gray-200 text-gray-700 rounded-md hover:bg-gray-300 disabled:opacity-50 transition">
          {{ testing ? 'Testing...' : 'Test Connection' }}
        </button>
      </div>
    </form>
  </div>
</template>
```

- [x] **Step 2: Add LDAP settings route**

Add to `frontend/src/router/index.ts` inside the protected routes:

```typescript
{
  path: '/admin/ldap',
  name: 'LdapSettings',
  component: () => import('@/views/admin/LdapSettingsView.vue'),
  meta: { requiresAdmin: true },
},
```

- [x] **Step 3: Verify frontend builds**

Run: `cd frontend && npm run build`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add frontend/src/views/admin/LdapSettingsView.vue frontend/src/router/index.ts
git commit -m "feat(frontend): add LDAP settings management page for admins"
```

---

### Task 25: Backend Wiring (main.go)

**Files:**
- Modify: `backend/cmd/main.go` (or equivalent server entry point)

- [x] **Step 1: Instantiate relay.Provider from config**

Add to server initialization:

```go
import (
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/oauth"
	"net/http"
	"sync/atomic"
)

// Create relay.Provider from config
var relayProvider relay.Provider
if cfg.Relay.URL != "" {
	relayProvider = relay.NewSub2apiProvider(
		http.DefaultClient,
		cfg.Relay.URL+"/v1",  // baseURL (LLM endpoint)
		cfg.Relay.URL,         // adminURL (management endpoint)
		cfg.Relay.APIKey,
		cfg.Relay.Model,
		logger,
	)
	// Verify connectivity
	if err := relayProvider.Ping(context.Background()); err != nil {
		logger.Warn("relay server not reachable", zap.Error(err))
	}
}
```

- [x] **Step 2: Register SSO provider with relay.Provider**

Replace old sub2apidb-based SSO registration:

```go
// Old:
// if sub2apiClient != nil { authService.RegisterProvider(auth.NewSSOProvider(sub2apiClient, logger)) }

// New:
if relayProvider != nil {
	authService.RegisterProvider(auth.NewSSOProvider(relayProvider, logger))
}
```

- [x] **Step 3: Create OAuth server and handler**

```go
// OAuth2 server
oauthServer := oauth.NewServer()

// TokenGenerator adapter — bridges oauth.TokenGenerator to auth.Service
tokenGen := &oauthTokenGenAdapter{authService: authService}
oauthHandler := oauth.NewHandler(oauthServer, cfg.Server.FrontendURL, tokenGen)
```

Define the adapter struct (can live in main.go or a small wiring file):

```go
type oauthTokenGenAdapter struct {
	authService *auth.Service
}

func (a *oauthTokenGenAdapter) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	info := &auth.UserInfo{ID: userID, Username: username, Role: role}
	tokens, err := a.authService.GenerateTokenPairForUser(info)
	if err != nil {
		return "", "", 0, err
	}
	return tokens.AccessToken, tokens.RefreshToken, tokens.ExpiresIn, nil
}
```

- [x] **Step 4: Create LDAP config atomic pointer for hot-reload**

```go
ldapCfg := &atomic.Pointer[config.LDAPConfig]{}
ldapCfg.Store(&cfg.Auth.LDAP)
```

- [x] **Step 5: Instantiate new handlers**

```go
providerHandler := handler.NewProviderHandler(entClient, cfg.Encryption.Key, logger)
adminSettingsHandler := handler.NewAdminSettingsHandler(entClient, cfg.Encryption.Key, ldapCfg)
```

- [x] **Step 6: Create LLM analyzer with relay.Provider**

```go
// Old: analyzer := analysis.NewAnalyzer(cfg.Analysis.LLM.Sub2apiURL, cfg.Analysis.LLM.Sub2apiAPIKey, ...)
// New:
analyzer := analysis.NewAnalyzer(relayProvider, cfg.Analysis.LLM.Model, ...)
```

- [x] **Step 7: Pass new dependencies to SetupRouter**

```go
router := handler.SetupRouter(
	entClient, authService, repoService, analysisService,
	webhookHandler, syncService, settingsHandler, chatHandler,
	aggregator, optimizer, cfg.Encryption.Key, corsMiddleware,
	oauthHandler, providerHandler, adminSettingsHandler, // new params
)
```

- [x] **Step 8: Remove sub2apidb initialization**

Delete all code related to `sub2apidb.New(...)`, `sub2apiDB` connection, and related error handling.

- [x] **Step 9: Verify compilation**

Run: `cd backend && go build ./...`
Expected: no errors

- [x] **Step 10: Commit**

```bash
git add backend/cmd/
git commit -m "feat(backend): wire relay.Provider, OAuth server, and new handlers in main.go"
```

---

### Task 26: CORS, LDAP Hot-Reload & Data Migration

**Files:**
- Modify: `backend/internal/handler/router.go` (CORS)
- Modify: `backend/internal/auth/ldap.go` (hot-reload)
- Create: migration SQL or Ent migration

- [x] **Step 1: Ensure CORS covers /oauth/* paths**

In `router.go` or wherever CORS middleware is configured, ensure the CORS middleware is applied to the root engine (not just `/api/v1`):

```go
// CORS is already applied via r.Use(corsMiddleware) at the engine level,
// which covers all routes including /oauth/*.
// Verify that corsMiddleware allows:
// - Origin: the frontend URL (e.g., http://localhost:5173)
// - Methods: GET, POST, OPTIONS
// - Headers: Authorization, Content-Type
// - Credentials: true
```

If CORS is only applied to the `/api/v1` group, move it to the engine level:

```go
r := gin.New()
r.Use(gin.Recovery())
r.Use(corsMiddleware) // Must be at engine level to cover /oauth/*
```

- [x] **Step 2: Refactor LDAPProvider to use atomic pointer for hot-reload**

Modify `backend/internal/auth/ldap.go`:

```go
// Old:
type LDAPProvider struct {
	config config.LDAPConfig
	logger *zap.Logger
}

// New:
type LDAPProvider struct {
	config *atomic.Pointer[config.LDAPConfig]
	logger *zap.Logger
}

func NewLDAPProvider(cfg *atomic.Pointer[config.LDAPConfig], logger *zap.Logger) *LDAPProvider {
	return &LDAPProvider{config: cfg, logger: logger}
}

func (p *LDAPProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	cfg := p.config.Load()
	if cfg == nil || cfg.URL == "" {
		return nil, nil // LDAP not configured
	}
	// Use cfg.URL, cfg.BaseDN, etc. for LDAP bind
	// ... existing LDAP logic using cfg instead of p.config ...
}
```

- [x] **Step 3: Write data migration for auth_source and field renames**

Create a migration script or add to Ent auto-migration hook:

```sql
-- Step 1: Add relay_sso to auth_source enum (already done via Ent schema change)
-- Step 2: Migrate existing data
UPDATE users SET auth_source = 'relay_sso' WHERE auth_source = 'sub2api_sso';

-- Step 3: Rename fields (handled by Ent auto-migration if using Atlas)
-- If manual migration needed:
ALTER TABLE users RENAME COLUMN sub2api_user_id TO relay_user_id;
ALTER TABLE sessions RENAME COLUMN sub2api_user_id TO relay_user_id;
ALTER TABLE sessions RENAME COLUMN sub2api_api_key_id TO relay_api_key_id;
```

Note: Ent's auto-migration with `WithDropColumn` and `WithDropIndex` may handle column renames as drop+create. For production, use Atlas versioned migrations instead. For dev (SQLite), auto-migration is acceptable.

- [x] **Step 4: Verify compilation and migration**

Run: `cd backend && go build ./...`
Run: `cd backend && go test ./internal/auth/ -v`

- [x] **Step 5: Commit**

```bash
git add backend/internal/handler/router.go backend/internal/auth/ldap.go
git commit -m "fix(backend): CORS for OAuth routes, LDAP hot-reload via atomic pointer, data migration"
```

---

### Task 27: Efficiency Labeler Refactor

**Files:**
- Modify: `backend/internal/efficiency/labeler.go`

- [x] **Step 1: Replace sub2apidb dependency with relay.Provider**

Modify `backend/internal/efficiency/labeler.go`:

```go
// Old:
type Labeler struct {
	sub2apiClient *sub2apidb.Client
	// ...
}

// New:
type Labeler struct {
	relayProvider relay.Provider
	// ...
}

func NewLabeler(relayProvider relay.Provider, ...) *Labeler {
	return &Labeler{relayProvider: relayProvider, ...}
}
```

- [x] **Step 2: Update usage query calls**

Replace `sub2apiClient.SumTokenCost(...)` with `relayProvider.GetUsageStats(...)`:

```go
// Old:
// cost, err := l.sub2apiClient.SumTokenCost(ctx, apiKeyID, from, to)

// New:
stats, err := l.relayProvider.GetUsageStats(ctx, relayUserID, from, to)
if err != nil {
	return fmt.Errorf("get usage stats: %w", err)
}
// Use stats.TotalTokens, stats.TotalCost
```

- [x] **Step 3: Update API key lookups**

Replace `sub2apiClient.GetAPIKey(...)` with `relayProvider.ListUserAPIKeys(...)`.

- [x] **Step 4: Verify compilation**

Run: `cd backend && go build ./internal/efficiency/`
Expected: no errors

- [x] **Step 5: Commit**

```bash
git add backend/internal/efficiency/labeler.go
git commit -m "refactor(backend): efficiency labeler uses relay.Provider instead of sub2apidb"
```

---

### Task 28: Dependency Cleanup

**Files:**
- Modify: `backend/go.mod`

- [x] **Step 1: Remove unused dependencies**

The spec lists `go-oauth2/gin-server` as a dependency, but our implementation uses custom handlers instead. Do NOT add `gin-server` — it's not needed.

Verify and clean up:

```bash
cd backend && go mod tidy
```

- [x] **Step 2: Verify no unused imports**

Run: `cd backend && go vet ./...`

- [x] **Step 3: Add API key logging suppression**

In the CORS/logging middleware, ensure `GET /api/v1/providers` response bodies are not logged:

```go
// In your logging middleware, skip body logging for sensitive endpoints:
// - GET /api/v1/providers (contains API keys)
```

This can be a simple path check in the response logger middleware.

- [x] **Step 4: Add backward compat warning for old ae-cli config.yaml**

In `ae-cli/cmd/root.go` or `ae-cli/cmd/login.go`, add:

```go
func init() {
	// Check for deprecated config.yaml
	home, _ := os.UserHomeDir()
	oldConfig := filepath.Join(home, ".ae-cli", "config.yaml")
	if _, err := os.Stat(oldConfig); err == nil {
		fmt.Fprintf(os.Stderr, "Warning: %s is deprecated and will be ignored. ae-cli now uses OAuth login.\n", oldConfig)
	}
}
```

- [x] **Step 5: Commit**

```bash
git add backend/go.mod backend/go.sum ae-cli/cmd/
git commit -m "chore: dependency cleanup, logging suppression, deprecation warning"
```

---

### Task 29: Integration Testing & Final Cleanup

**Files:**
- Modify: various (fix any remaining compilation issues)

- [x] **Step 1: Verify backend compiles end-to-end**

Run: `cd backend && go build ./...`

Fix any remaining compilation errors. Common issues:
- Missing imports for new packages
- Unused imports from removed packages
- Type mismatches from field renames

- [x] **Step 2: Run backend tests**

Run: `cd backend && go test ./... -count=1`

Fix any failing tests. Focus on:
- `internal/relay/` — new tests should pass
- `internal/oauth/` — new tests should pass
- `internal/auth/` — SSO provider tests may need updating
- `internal/handler/` — session handler tests may need updating

- [x] **Step 3: Verify ae-cli compiles end-to-end**

Run: `cd ae-cli && go build ./...`

Fix any remaining issues.

- [x] **Step 4: Run ae-cli tests**

Run: `cd ae-cli && go test ./... -count=1`

- [x] **Step 5: Verify frontend builds**

Run: `cd frontend && npm run build`

- [x] **Step 6: Update go.mod files**

Run: `cd backend && go mod tidy`
Run: `cd ae-cli && go mod tidy`

- [x] **Step 7: Final commit**

```bash
git add -A
git commit -m "chore: integration fixes and cleanup for OAuth CLI login implementation"
```
