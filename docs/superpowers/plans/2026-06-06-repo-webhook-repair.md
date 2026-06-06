# Repo Webhook Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an admin repair path that re-registers webhooks for already-bound `webhook_failed` repos, with explicit public callback URL configuration and Bitbucket signature validation.

**Architecture:** Keep webhook repair inside `backend/internal/repo` so it reuses the existing `RepoConfig -> ScmProvider` boundary and `SCMProvider` interface. Add a public callback URL contract at config/service construction, pass provider-specific callback URLs into SCM providers, and expose narrow admin-only handlers for single and batch repair. The frontend reuses the current repository health surfaces and adds focused admin actions without exposing webhook secrets.

**Tech Stack:** Go, Gin, Ent, zap, Viper, go-github, Vue 3, Pinia, TypeScript, Vitest, TailwindCSS.

**Status:** In progress. Tasks 1-4 have been implemented and verified in this worktree; Tasks 5-6 remain open.

---

## File Map

### Backend Configuration And SCM Callback Plumbing

- Modify: `backend/internal/config/config.go`
  - Add `ServerConfig.PublicURL`, default/bind `server.public_url`, and support `AE_SERVER_PUBLIC_URL`.
- Modify: `backend/internal/config/writable_config.go`
  - Persist `public_url` when writing runtime config.
- Modify: `backend/internal/config/config_test.go`
  - Cover env loading and writable config round-trip for `public_url`.
- Modify: `backend/cmd/server/main.go`
  - Pass server URL options into `repo.NewService`.
- Modify: `backend/internal/repo/service.go`
  - Add optional service configuration for webhook public URL, frontend URL, server mode, and test provider factory injection.
- Modify: `backend/internal/repo/factory.go`
  - Pass provider-specific callback URLs into GitHub and Bitbucket provider constructors.
- Modify: `backend/internal/scm/github/github.go`
  - Store an optional callback URL and set `HookConfig.URL` during webhook registration.
- Modify: `backend/internal/scm/github/github_extra_test.go`
  - Cover GitHub webhook callback URL payload.
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
  - Reject empty callback URL before creating a webhook and keep using the configured callback URL in the payload.
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`
  - Cover Bitbucket callback URL payload and missing callback validation.

### Backend Repair Service And API

- Create: `backend/internal/repo/webhook_repair.go`
  - Define repair result types, status constants, public URL errors, single repair, batch repair, and helper functions.
- Create: `backend/internal/repo/webhook_repair_test.go`
  - Cover unbound/inactive rejection, missing public URL, Bitbucket registration success/failure, force deletion, and batch filtering.
- Modify: `backend/internal/handler/repo.go`
  - Add `RepairWebhook` and `RepairFailedWebhooks` handler methods and error-to-status mapping.
- Modify: `backend/internal/handler/router.go`
  - Add admin-only `POST /api/v1/repos/repair-webhooks` and `POST /api/v1/repos/:id/repair-webhook`.
- Create: `backend/internal/handler/repo_webhook_repair_test.go`
  - Cover route authorization, request parsing, and key HTTP status semantics.

### Bitbucket Inbound Signature Validation

- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
  - Add `ErrInvalidSignature`, `IsInvalidSignature`, and HMAC-SHA256 validation for stored secrets.
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`
  - Cover valid signature, missing signature with stored secret, invalid signature, unsupported algorithm, and no-secret compatibility.
- Modify: `backend/internal/webhook/handler.go`
  - Return `401` for Bitbucket signature failures and still store a dead letter.
- Modify: `backend/internal/webhook/webhook_coverage_test.go`
  - Update secret-path tests and add handler coverage for invalid Bitbucket signature dead letters.

### Frontend

- Modify: `frontend/src/types/index.ts`
  - Add `webhook_id` to `RepoConfig`, and add repair request/result/summary types.
- Modify: `frontend/src/api/repo.ts`
  - Add `repairWebhook` and `repairFailedWebhooks`.
- Modify: `frontend/src/views/repos/RepoListView.vue`
  - Add admin-only batch repair action, loading/error/result state, and list refresh.
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
  - Add admin-only single repo repair action for bound `webhook_failed` repos and optional force replacement when `webhook_id` exists.
- Modify: `frontend/src/i18n.ts`
  - Add English and Chinese labels/messages for webhook repair.
- Modify: `frontend/src/__tests__/api-modules.test.ts`
  - Cover new API wrappers.
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
  - Cover batch action visibility, success summary, failure message, and list refresh.
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`
  - Cover detail repair visibility, default repair, force repair, and error message.

### Deploy And Docs

- Modify: `deploy/config.example.yaml`
  - Document `server.public_url`.
- Modify: `deploy/.env.example`
  - Document `AE_SERVER_PUBLIC_URL`.
- Modify: `deploy/docker-compose.yml`
  - Pass `AE_SERVER_PUBLIC_URL` into the backend container.
- Modify: `deploy/docker-compose.bootstrap.yml`
  - Pass `AE_SERVER_PUBLIC_URL` into the backend container.
- Modify: `deploy/docker-compose.external.yml`
  - Pass `AE_SERVER_PUBLIC_URL` into the backend container.
- Modify: `deploy/docker-compose.dev.yml`
  - Set local `AE_SERVER_PUBLIC_URL` to the backend URL for dev.
- Modify: `deploy/docker-compose.local.yml`
  - Set local `AE_SERVER_PUBLIC_URL` to the backend URL for local bundled mode.
- Modify: `docs/architecture.md`
  - Describe webhook repair and public callback URL as current SCM operations behavior.
- Modify: `docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md`
  - Link to the repair spec and clarify that auto-bind does not repair already-bound webhook failures.
- Modify: `docs/superpowers/specs/2026-06-06-repo-webhook-repair-design.md`
  - Change status to implemented after code and verification are complete.

---

## Task 1: Add Public Callback URL Configuration And Provider Plumbing

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/writable_config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/factory.go`
- Modify: `backend/internal/scm/github/github.go`
- Modify: `backend/internal/scm/github/github_extra_test.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`

- [x] **Step 1: Write failing config tests**

Add this test to `backend/internal/config/config_test.go` near other `Load` tests:

```go
func TestLoadReadsServerPublicURLFromEnvironment(t *testing.T) {
	t.Setenv("AE_SERVER_PUBLIC_URL", "https://ai-efficiency.example.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.PublicURL != "https://ai-efficiency.example.com" {
		t.Fatalf("PublicURL = %q, want https://ai-efficiency.example.com", cfg.Server.PublicURL)
	}
}
```

Update `TestEnsureWritableConfigFileCreatesReloadableConfig` in `backend/internal/config/config_test.go` so the input config includes `PublicURL` and the loaded config assertion checks it:

```go
cfg := &Config{
	Server: ServerConfig{
		Port:        8081,
		Mode:        "release",
		FrontendURL: "http://localhost:8081",
		PublicURL:   "https://ai-efficiency.example.com",
	},
	DB:         DBConfig{DSN: "postgres://postgres:postgres@localhost:5432/ai_efficiency?sslmode=disable", MaxOpenConns: 25, MaxIdleConns: 5, ConnMaxLifetime: 300},
	Redis:      RedisConfig{Addr: "redis:6379", DB: 0},
	Relay:      RelayConfig{Provider: "sub2api", URL: "http://relay.example.com", AdminAPIKey: "admin-live", Model: "gpt-5.4", DefaultGroupID: "42"},
	Auth:       AuthConfig{JWTSecret: "jwt-secret", AccessTokenTTL: 7200, RefreshTokenTTL: 604800, LDAP: LDAPConfig{URL: "ldap://ldap.example.com:389", BaseDN: "dc=example,dc=com", BindDN: "cn=admin,dc=example,dc=com", BindPassword: "secret", UserFilter: "(uid=%s)", TLS: true}},
	Encryption: EncryptionConfig{Key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	Deployment: DeploymentConfig{Mode: "bundled", StateDir: "/var/lib/ai-efficiency", Update: UpdateConfig{Enabled: true, ApplyEnabled: true, ReleaseAPIURL: "https://api.github.com/repos/example/ai-efficiency/releases/latest", ImageRepository: "ghcr.io/example/ai-efficiency", Channel: "stable"}},
}
```

Add this assertion after reloading the config:

```go
if loaded.Server.PublicURL != "https://ai-efficiency.example.com" {
	t.Fatalf("loaded public URL = %q", loaded.Server.PublicURL)
}
```

- [x] **Step 2: Run config tests and verify failure**

Run:

```bash
cd backend && go test ./internal/config -run 'TestLoadReadsServerPublicURLFromEnvironment|TestEnsureWritableConfigFileCreatesReloadableConfig' -count=1
```

Expected: FAIL because `ServerConfig.PublicURL` does not exist and `server.public_url` is not persisted.

- [x] **Step 3: Implement config loading and persistence**

In `backend/internal/config/config.go`, extend `ServerConfig`:

```go
type ServerConfig struct {
	Port        int    `mapstructure:"port"`
	Mode        string `mapstructure:"mode"` // debug / release
	FrontendURL string `mapstructure:"frontend_url"`
	PublicURL   string `mapstructure:"public_url"`
}
```

Add the default and env binding:

```go
v.SetDefault("server.public_url", "")
```

```go
"server.public_url",
```

In `backend/internal/config/writable_config.go`, add `public_url` beside `frontend_url` in the server map:

```go
"server": map[string]any{
	"port":         cfg.Server.Port,
	"mode":         cfg.Server.Mode,
	"frontend_url": cfg.Server.FrontendURL,
	"public_url":   cfg.Server.PublicURL,
},
```

- [x] **Step 4: Write failing SCM provider callback tests**

In `backend/internal/scm/bitbucket/bitbucket_test.go`, update `TestRegisterWebhook` to construct the provider with a callback URL and assert payload URL:

```go
p, _ := New(srv.URL, "test-token", zap.NewNop(), "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket")
```

Add after the event assertion:

```go
if gotBody["url"] != "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket" {
	t.Fatalf("url = %v, want callback URL", gotBody["url"])
}
```

Add a missing callback test:

```go
func TestRegisterWebhookRequiresCallbackURL(t *testing.T) {
	p, _ := New("https://bb.example.com", "tok", zap.NewNop())

	_, err := p.RegisterWebhook(context.Background(), "P/r", []string{"push"}, "secret")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "webhook callback URL is required") {
		t.Fatalf("error = %v, want callback URL requirement", err)
	}
}
```

In `backend/internal/scm/github/github_extra_test.go`, add:

```go
func TestRegisterWebhookSendsCallbackURL(t *testing.T) {
	var got struct {
		Config map[string]string `json:"config"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/hooks" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42})
	}))
	t.Cleanup(server.Close)

	p, err := New(server.URL, "tok", zap.NewNop(), "https://ai-efficiency.example.com/api/v1/webhooks/github")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	id, err := p.RegisterWebhook(context.Background(), "owner/repo", []string{"push"}, "secret")
	if err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if id != "42" {
		t.Fatalf("id = %q, want 42", id)
	}
	if got.Config["url"] != "https://ai-efficiency.example.com/api/v1/webhooks/github" {
		t.Fatalf("url = %q, want callback URL", got.Config["url"])
	}
}
```

- [x] **Step 5: Run SCM provider tests and verify failure**

Run:

```bash
cd backend && go test ./internal/scm/bitbucket ./internal/scm/github -run 'TestRegisterWebhook|TestRegisterWebhookRequiresCallbackURL|TestRegisterWebhookSendsCallbackURL' -count=1
```

Expected: FAIL because Bitbucket does not reject empty callback URLs and GitHub does not accept/pass a callback URL.

- [x] **Step 6: Implement callback-aware SCM providers and repo service options**

In `backend/internal/scm/bitbucket/bitbucket.go`, add this guard at the start of `RegisterWebhook` after `splitFullName`:

```go
if strings.TrimSpace(p.webhookCallbackURL) == "" {
	return "", fmt.Errorf("webhook callback URL is required")
}
```

In `backend/internal/scm/github/github.go`, add a field and optional constructor argument matching Bitbucket. Replace the provider struct and constructor with:

```go
type Provider struct {
	client             *gh.Client
	baseURL            string
	logger             *zap.Logger
	token              string
	webhookCallbackURL string
}

func New(baseURL, token string, logger *zap.Logger, webhookCallbackURL ...string) (*Provider, error) {
	cbURL := ""
	if len(webhookCallbackURL) > 0 {
		cbURL = strings.TrimSpace(webhookCallbackURL[0])
	}
	var client *gh.Client
	if token != "" {
		client = gh.NewClient(nil).WithAuthToken(token)
	} else {
		client = gh.NewClient(nil)
	}
	if baseURL != "" && baseURL != "https://api.github.com" {
		var err error
		client, err = client.WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, fmt.Errorf("github enterprise url: %w", err)
		}
	}
	return &Provider{
		client:             client,
		baseURL:            strings.TrimRight(baseURL, "/"),
		token:              token,
		logger:             logger,
		webhookCallbackURL: cbURL,
	}, nil
}
```

In GitHub `RegisterWebhook`, set `URL` only when configured:

```go
hookConfig := &gh.HookConfig{
	ContentType: strPtr("json"),
	Secret:      &secret,
}
if strings.TrimSpace(p.webhookCallbackURL) != "" {
	hookConfig.URL = strPtr(p.webhookCallbackURL)
}

hook, _, err := p.client.Repositories.CreateHook(ctx, owner, repo, &gh.Hook{
	Config: hookConfig,
	Events: events,
	Active: &active,
})
```

In `backend/internal/repo/service.go`, add `net/url` to imports, then add service options and callback resolution:

```go
type ServiceOptions struct {
	WebhookPublicURL string
	FrontendURL      string
	ServerMode       string
	SCMFactory       scmProviderFactory
}

type scmProviderFactory func(providerType, baseURL string, apiCredential any, callbackURL string) (scm.SCMProvider, error)

var ErrWebhookPublicURLRequired = errors.New("server.public_url is required for webhook registration")

type Service struct {
	entClient        *ent.Client
	encryptionKey    string
	logger           *zap.Logger
	autoBindPostBind autoBindPostBindFunc
	webhookPublicURL string
	frontendURL      string
	serverMode       string
	scmFactory       scmProviderFactory
}

func NewService(entClient *ent.Client, encryptionKey string, logger *zap.Logger, options ...ServiceOptions) *Service {
	opt := ServiceOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	return &Service{
		entClient:        entClient,
		encryptionKey:    encryptionKey,
		logger:           logger,
		webhookPublicURL: strings.TrimSpace(opt.WebhookPublicURL),
		frontendURL:      strings.TrimSpace(opt.FrontendURL),
		serverMode:       strings.TrimSpace(opt.ServerMode),
		scmFactory:       opt.SCMFactory,
	}
}

func (s *Service) webhookCallbackURL(providerType string, require bool) (string, error) {
	base := strings.TrimSpace(s.webhookPublicURL)
	if base == "" && s.serverMode != "release" {
		base = strings.TrimSpace(s.frontendURL)
	}
	if base == "" {
		if require {
			return "", ErrWebhookPublicURLRequired
		}
		return "", nil
	}

	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid webhook public URL %q", base)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid webhook public URL scheme %q", parsed.Scheme)
	}

	base = strings.TrimRight(parsed.String(), "/")
	switch providerType {
	case string(scmprovider.TypeGithub):
		return base + "/api/v1/webhooks/github", nil
	case string(scmprovider.TypeBitbucketServer):
		return base + "/api/v1/webhooks/bitbucket", nil
	default:
		return "", fmt.Errorf("unsupported provider type: %s", providerType)
	}
}
```

In `backend/internal/repo/factory.go`, change factory functions to accept callback URLs:

```go
func newGitHubProvider(baseURL string, apiCredential any, logger *zap.Logger, callbackURL string) (scm.SCMProvider, error) {
	apiPayload, err := normalizeAPIPayload(apiCredential)
	if err != nil {
		return nil, err
	}
	secret, err := credential.ResolveAPISecret(apiPayload)
	if err != nil {
		return nil, err
	}
	return github.New(baseURL, secret, logger, callbackURL)
}

func newBitbucketProvider(baseURL string, apiCredential any, logger *zap.Logger, callbackURL string) (scm.SCMProvider, error) {
	apiPayload, err := normalizeAPIPayload(apiCredential)
	if err != nil {
		return nil, err
	}
	secret, err := credential.ResolveAPISecret(apiPayload)
	if err != nil {
		return nil, err
	}
	return bitbucket.New(baseURL, secret, logger, callbackURL)
}
```

In `Service.newSCMProvider`, derive a non-required callback URL and pass it into the factory:

```go
callbackURL, err := s.webhookCallbackURL(providerType, false)
if err != nil {
	return nil, err
}
if s.scmFactory != nil {
	return s.scmFactory(providerType, baseURL, apiCredential, callbackURL)
}
switch providerType {
case string(scmprovider.TypeGithub):
	return newGitHubProvider(baseURL, apiCredential, s.logger, callbackURL)
case string(scmprovider.TypeBitbucketServer):
	return newBitbucketProvider(baseURL, apiCredential, s.logger, callbackURL)
default:
	return nil, fmt.Errorf("unsupported provider type: %s", providerType)
}
```

In `backend/cmd/server/main.go`, construct the repo service with config values:

```go
repoService := repo.NewService(entClient, cfg.Encryption.Key, logger, repo.ServiceOptions{
	WebhookPublicURL: cfg.Server.PublicURL,
	FrontendURL:      cfg.Server.FrontendURL,
	ServerMode:       cfg.Server.Mode,
})
```

- [x] **Step 7: Run focused config and provider tests**

Run:

```bash
cd backend && go test ./internal/config ./internal/scm/bitbucket ./internal/scm/github -run 'TestLoadReadsServerPublicURLFromEnvironment|TestEnsureWritableConfigFileCreatesReloadableConfig|TestRegisterWebhook|TestRegisterWebhookRequiresCallbackURL|TestRegisterWebhookSendsCallbackURL' -count=1
```

Expected: PASS.

- [x] **Step 8: Commit callback plumbing**

Run:

```bash
git add backend/internal/config/config.go backend/internal/config/writable_config.go backend/internal/config/config_test.go backend/cmd/server/main.go backend/internal/repo/service.go backend/internal/repo/factory.go backend/internal/scm/github/github.go backend/internal/scm/github/github_extra_test.go backend/internal/scm/bitbucket/bitbucket.go backend/internal/scm/bitbucket/bitbucket_test.go
git commit -m "feat(webhook): configure public callback urls"
```

---

## Task 2: Add Repo Webhook Repair Service

**Files:**
- Create: `backend/internal/repo/webhook_repair.go`
- Create: `backend/internal/repo/webhook_repair_test.go`
- Modify: `backend/internal/repo/service.go`

- [x] **Step 1: Write failing repair service tests**

Create `backend/internal/repo/webhook_repair_test.go` with these helpers and tests:

```go
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

const webhookRepairTestKey = "0000000000000000000000000000000000000000000000000000000000000000"

type bitbucketRepairServer struct {
	server        *httptest.Server
	deleteCalled  bool
	registerCalls int
	failRegister  bool
}

func newBitbucketRepairServer(t *testing.T) *bitbucketRepairServer {
	t.Helper()
	s := &bitbucketRepairServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("repo method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug": "repo",
			"name": "repo",
			"project": map[string]any{"key": "PROJ"},
			"links": map[string]any{
				"clone": []map[string]string{{"name": "http", "href": "https://bitbucket.example.com/scm/proj/repo.git"}},
			},
		})
	})
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("webhook method = %s", r.Method)
		}
		s.registerCalls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode register body: %v", err)
		}
		if body["url"] != "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket" {
			t.Fatalf("url = %v, want public callback URL", body["url"])
		}
		if s.failRegister {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"requires REPO_ADMIN"}]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99})
	})
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo/webhooks/old-hook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("delete method = %s", r.Method)
		}
		s.deleteCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func newRepairTestService(t *testing.T, publicURL string) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, webhookRepairTestKey, zap.NewNop(), ServiceOptions{
		WebhookPublicURL: publicURL,
		ServerMode:       "release",
	})
	return client, svc
}

func createRepairProvider(t *testing.T, client *ent.Client, baseURL string) *ent.ScmProvider {
	t.Helper()
	encrypted, err := pkg.Encrypt(`{"token":"test-token"}`, webhookRepairTestKey)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}
	provider, err := client.ScmProvider.Create().
		SetName("bitbucket").
		SetType(scmprovider.TypeBitbucketServer).
		SetBaseURL(baseURL).
		SetCredentials(encrypted).
		SetStatus(scmprovider.StatusActive).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

func createRepairRepo(t *testing.T, client *ent.Client, provider *ent.ScmProvider, status repoconfig.Status) *ent.RepoConfig {
	return createRepairRepoNamed(t, client, provider, "PROJ/repo", "repo", status)
}

func createRepairRepoNamed(t *testing.T, client *ent.Client, provider *ent.ScmProvider, fullName string, name string, status repoconfig.Status) *ent.RepoConfig {
	t.Helper()
	create := client.RepoConfig.Create().
		SetRepoKey("bitbucket.example.com/" + strings.ToLower(fullName)).
		SetName(name).
		SetFullName(fullName).
		SetCloneURL("https://bitbucket.example.com/scm/proj/" + name + ".git").
		SetDefaultBranch("main").
		SetStatus(status)
	if provider != nil {
		create.SetScmProviderID(provider.ID)
	}
	repo, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return repo
}

func TestRepairWebhookRejectsUnboundRepo(t *testing.T) {
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	repo := createRepairRepo(t, client, nil, repoconfig.StatusWebhookFailed)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrRepoUnbound) {
		t.Fatalf("err = %v, want ErrRepoUnbound", err)
	}
}

func TestRepairWebhookRejectsInactiveRepo(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusInactive)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrRepoInactive) {
		t.Fatalf("err = %v, want ErrRepoInactive", err)
	}
}

func TestRepairWebhookMissingPublicURLFailsBeforeSCM(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrWebhookPublicURLRequired) {
		t.Fatalf("err = %v, want ErrWebhookPublicURLRequired", err)
	}
	if server.registerCalls != 0 {
		t.Fatalf("register calls = %d, want 0", server.registerCalls)
	}
}

func TestRepairWebhookRegistersBoundWebhookFailedRepo(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	result, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairWebhook: %v", err)
	}
	if result.WebhookStatus != WebhookRepairRegistered {
		t.Fatalf("WebhookStatus = %q, want %q", result.WebhookStatus, WebhookRepairRegistered)
	}

	loaded := client.RepoConfig.GetX(context.Background(), repo.ID)
	if loaded.Status != repoconfig.StatusActive {
		t.Fatalf("status = %q, want active", loaded.Status)
	}
	if loaded.WebhookID == nil || *loaded.WebhookID != "99" {
		t.Fatalf("webhook id = %v, want 99", loaded.WebhookID)
	}
	if loaded.WebhookSecret == nil || *loaded.WebhookSecret == "" {
		t.Fatal("expected webhook secret")
	}
}

func TestRepairWebhookForceDeletesExistingWebhookBeforeReplacement(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)
	client.RepoConfig.UpdateOneID(repo.ID).SetWebhookID("old-hook").SetWebhookSecret("old-secret").SaveX(context.Background())

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{Force: true})
	if err != nil {
		t.Fatalf("RepairWebhook: %v", err)
	}
	if !server.deleteCalled {
		t.Fatal("expected delete call")
	}
}

func TestRepairWebhookRegistrationFailureKeepsWebhookFailed(t *testing.T) {
	server := newBitbucketRepairServer(t)
	server.failRegister = true
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	result, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairWebhook should return item failure without endpoint error: %v", err)
	}
	if result.WebhookStatus != WebhookRepairFailed {
		t.Fatalf("WebhookStatus = %q, want failed", result.WebhookStatus)
	}
	if !strings.Contains(result.Error, "requires REPO_ADMIN") {
		t.Fatalf("error = %q, want upstream permission text", result.Error)
	}
	loaded := client.RepoConfig.GetX(context.Background(), repo.ID)
	if loaded.Status != repoconfig.StatusWebhookFailed {
		t.Fatalf("status = %q, want webhook_failed", loaded.Status)
	}
}

func TestRepairFailedWebhooksScansOnlyBoundWebhookFailedRepos(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	target := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)
	createRepairRepoNamed(t, client, provider, "PROJ/active", "active", repoconfig.StatusActive)
	createRepairRepoNamed(t, client, nil, "PROJ/unbound", "unbound", repoconfig.StatusWebhookFailed)

	result, err := svc.RepairFailedWebhooks(context.Background(), RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairFailedWebhooks: %v", err)
	}
	if result.Summary.Scanned != 1 || result.Summary.Repaired != 1 {
		t.Fatalf("summary = %+v, want scanned=1 repaired=1", result.Summary)
	}
	if len(result.Items) != 1 || result.Items[0].RepoConfigID != target.ID {
		t.Fatalf("items = %+v, want only repo %d", result.Items, target.ID)
	}
}
```

If the compiler reports unused imports from this pasted test, remove only the unused imports that are not needed by the final test file.

- [x] **Step 2: Run repair service tests and verify failure**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/repo -run 'TestRepairWebhook|TestRepairFailedWebhooks' -count=1
```

Expected: FAIL because repair types and methods do not exist.

- [x] **Step 3: Implement repair types and status constants**

Create `backend/internal/repo/webhook_repair.go`:

```go
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"go.uber.org/zap"
)

const (
	WebhookRepairRegistered        = "registered"
	WebhookRepairAlreadyRegistered = "already_registered"
	WebhookRepairFailed            = "failed"
)

var (
	ErrRepoInactive = errors.New("repo is inactive")
)

type RepairWebhookRequest struct {
	Force bool `json:"force"`
}

type RepairWebhookSummary struct {
	Scanned           int `json:"scanned"`
	Repaired          int `json:"repaired"`
	AlreadyRegistered int `json:"already_registered"`
	Failed            int `json:"failed"`
}

type RepairWebhookBatchResult struct {
	Summary RepairWebhookSummary  `json:"summary"`
	Items   []RepairWebhookResult `json:"items"`
}

type RepairWebhookResult struct {
	RepoConfigID   int    `json:"repo_config_id"`
	FullName       string `json:"full_name"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	WebhookStatus  string `json:"webhook_status"`
	WebhookID      string `json:"webhook_id,omitempty"`
	CallbackURL    string `json:"callback_url,omitempty"`
	Error          string `json:"error,omitempty"`
}
```

- [x] **Step 4: Implement single repo repair**

Add `RepairWebhook` to `backend/internal/repo/webhook_repair.go`:

```go
func (s *Service) RepairWebhook(ctx context.Context, repoID int, req RepairWebhookRequest) (RepairWebhookResult, error) {
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		WithScmProvider(func(query *ent.ScmProviderQuery) {
			query.WithAPICredential()
		}).
		Only(ctx)
	if err != nil {
		return RepairWebhookResult{}, fmt.Errorf("repair webhook: load repo: %w", err)
	}

	result := RepairWebhookResult{
		RepoConfigID:   rc.ID,
		FullName:       rc.FullName,
		PreviousStatus: string(rc.Status),
		Status:         string(rc.Status),
		WebhookStatus:  WebhookRepairFailed,
	}

	provider := rc.Edges.ScmProvider
	if provider == nil {
		return result, ErrRepoUnbound
	}
	if rc.Status == repoconfig.StatusInactive {
		return result, ErrRepoInactive
	}
	if rc.WebhookID != nil && *rc.WebhookID != "" && !req.Force && rc.Status == repoconfig.StatusActive {
		result.WebhookStatus = WebhookRepairAlreadyRegistered
		result.WebhookID = *rc.WebhookID
		return result, nil
	}

	callbackURL, err := s.webhookCallbackURL(string(provider.Type), true)
	if err != nil {
		return result, err
	}
	result.CallbackURL = callbackURL

	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return result, fmt.Errorf("repair webhook: resolve api credential: %w", err)
	}

	scmProvider, err := s.newSCMProviderWithCallback(string(provider.Type), provider.BaseURL, apiPayload, callbackURL)
	if err != nil {
		return result, fmt.Errorf("repair webhook: create scm provider: %w", err)
	}

	repoInfo, err := scmProvider.GetRepo(ctx, rc.FullName)
	if err != nil {
		result.Error = err.Error()
		_, saveErr := s.entClient.RepoConfig.UpdateOneID(rc.ID).SetStatus(repoconfig.StatusWebhookFailed).Save(ctx)
		if saveErr != nil {
			return result, fmt.Errorf("repair webhook: verify repo: %v; save webhook_failed: %w", err, saveErr)
		}
		result.Status = string(repoconfig.StatusWebhookFailed)
		return result, nil
	}

	deletedOldWebhook := false
	if req.Force && rc.WebhookID != nil && *rc.WebhookID != "" {
		if err := scmProvider.DeleteWebhook(ctx, rc.FullName, *rc.WebhookID); err != nil && s.logger != nil {
			s.logger.Warn("failed to delete old webhook before repair", zap.Int("repo_config_id", rc.ID), zap.Error(err))
		} else if err == nil {
			deletedOldWebhook = true
		}
	}

	secret, err := generateSecret(32)
	if err != nil {
		return result, fmt.Errorf("repair webhook: generate secret: %w", err)
	}

	webhookID, err := scmProvider.RegisterWebhook(ctx, repoInfo.FullName, []string{"pull_request", "push"}, secret)
	if err != nil {
		result.Error = err.Error()
		update := s.entClient.RepoConfig.UpdateOneID(rc.ID).SetStatus(repoconfig.StatusWebhookFailed)
		if deletedOldWebhook {
			update.ClearWebhookID().ClearWebhookSecret()
		}
		if _, saveErr := update.Save(ctx); saveErr != nil {
			return result, fmt.Errorf("repair webhook: register webhook: %v; save webhook_failed: %w", err, saveErr)
		}
		result.Status = string(repoconfig.StatusWebhookFailed)
		return result, nil
	}

	update := s.entClient.RepoConfig.UpdateOneID(rc.ID).
		SetName(repoInfo.Name).
		SetFullName(repoInfo.FullName).
		SetCloneURL(repoInfo.CloneURL).
		SetDefaultBranch(repoInfo.DefaultBranch).
		SetWebhookID(webhookID).
		SetWebhookSecret(secret).
		SetStatus(repoconfig.StatusActive)
	if _, err := update.Save(ctx); err != nil {
		return result, fmt.Errorf("repair webhook: save repo metadata: %w", err)
	}

	result.Status = string(repoconfig.StatusActive)
	result.WebhookStatus = WebhookRepairRegistered
	result.WebhookID = webhookID
	return result, nil
}
```

Add `newSCMProviderWithCallback` in `backend/internal/repo/service.go` or `factory.go` and make `newSCMProvider` call it:

```go
func (s *Service) newSCMProviderWithCallback(providerType, baseURL string, apiCredential any, callbackURL string) (scm.SCMProvider, error) {
	if s.scmFactory != nil {
		return s.scmFactory(providerType, baseURL, apiCredential, callbackURL)
	}
	switch providerType {
	case string(scmprovider.TypeGithub):
		return newGitHubProvider(baseURL, apiCredential, s.logger, callbackURL)
	case string(scmprovider.TypeBitbucketServer):
		return newBitbucketProvider(baseURL, apiCredential, s.logger, callbackURL)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}
```

- [x] **Step 5: Implement batch repair**

Add `RepairFailedWebhooks` and summary helper:

```go
func (s *Service) RepairFailedWebhooks(ctx context.Context, req RepairWebhookRequest) (*RepairWebhookBatchResult, error) {
	repos, err := s.entClient.RepoConfig.Query().
		Where(
			repoconfig.HasScmProvider(),
			repoconfig.StatusEQ(repoconfig.StatusWebhookFailed),
		).
		Order(ent.Asc(repoconfig.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("repair failed webhooks: list repos: %w", err)
	}

	batch := &RepairWebhookBatchResult{Items: make([]RepairWebhookResult, 0, len(repos))}
	for _, rc := range repos {
		item, err := s.RepairWebhook(ctx, rc.ID, req)
		if err != nil {
			item = RepairWebhookResult{
				RepoConfigID:   rc.ID,
				FullName:       rc.FullName,
				PreviousStatus: string(rc.Status),
				Status:         string(rc.Status),
				WebhookStatus:  WebhookRepairFailed,
				Error:          err.Error(),
			}
		}
		item.addToSummary(&batch.Summary)
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func (r RepairWebhookResult) addToSummary(summary *RepairWebhookSummary) {
	summary.Scanned++
	switch r.WebhookStatus {
	case WebhookRepairRegistered:
		summary.Repaired++
	case WebhookRepairAlreadyRegistered:
		summary.AlreadyRegistered++
	case WebhookRepairFailed:
		summary.Failed++
	}
}
```

- [x] **Step 6: Run repair service tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/repo -run 'TestRepairWebhook|TestRepairFailedWebhooks' -count=1
```

Expected: PASS.

- [x] **Step 7: Commit repair service**

Run:

```bash
git add backend/internal/repo/service.go backend/internal/repo/factory.go backend/internal/repo/webhook_repair.go backend/internal/repo/webhook_repair_test.go
git commit -m "feat(repo): add webhook repair service"
```

---

## Task 3: Expose Admin Repair API Routes

**Files:**
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/router.go`
- Create: `backend/internal/handler/repo_webhook_repair_test.go`

- [x] **Step 1: Write failing handler tests**

Create `backend/internal/handler/repo_webhook_repair_test.go`:

```go
package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent/repoconfig"
)

func TestRepairWebhookRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/1/repair-webhook", map[string]any{"force": false}, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestRepairFailedWebhooksRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user-batch", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/repair-webhooks", map[string]any{"force": false}, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestRepairWebhookUnboundRepoReturnsConflict(t *testing.T) {
	env := setupFullTestEnv(t)
	repo := env.client.RepoConfig.Create().
		SetName("repo").
		SetFullName("PROJ/repo").
		SetCloneURL("https://bitbucket.example.com/scm/proj/repo.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusWebhookFailed).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/"+strconv.Itoa(repo.ID)+"/repair-webhook", map[string]any{"force": false})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo_unbound") {
		t.Fatalf("body = %s, want repo_unbound", w.Body.String())
	}
}

func TestRepairFailedWebhooksEmptyBatchReturnsSummary(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/repair-webhooks", map[string]any{"force": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if int(summary["scanned"].(float64)) != 0 {
		t.Fatalf("summary = %v, want scanned=0", summary)
	}
}
```

Ensure imports include `strconv` if using `strconv.Itoa`.

- [x] **Step 2: Run handler tests and verify failure**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run 'TestRepairWebhook|TestRepairFailedWebhooks' -count=1
```

Expected: FAIL because the routes and handlers do not exist.

- [x] **Step 3: Implement repo handler methods**

In `backend/internal/handler/repo.go`, add an error mapper:

```go
func writeRepairWebhookError(c *gin.Context, err error) {
	switch {
	case ent.IsNotFound(err):
		pkg.Error(c, http.StatusNotFound, "repo not found")
	case errors.Is(err, repo.ErrRepoUnbound):
		pkg.Error(c, http.StatusConflict, "repo_unbound")
	case errors.Is(err, repo.ErrRepoInactive), errors.Is(err, repo.ErrWebhookPublicURLRequired):
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}
```

Add handler methods:

```go
func (h *RepoHandler) RepairWebhook(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req repo.RepairWebhookRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	result, err := h.repoService.RepairWebhook(c.Request.Context(), id, req)
	if err != nil {
		writeRepairWebhookError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *RepoHandler) RepairFailedWebhooks(c *gin.Context) {
	var req repo.RepairWebhookRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	result, err := h.repoService.RepairFailedWebhooks(c.Request.Context(), req)
	if err != nil {
		writeRepairWebhookError(c, err)
		return
	}
	pkg.Success(c, result)
}
```

If `ShouldBindJSON` returns an EOF error type instead of string text in this codebase, replace the string check with `errors.Is(err, io.EOF)` and add `io` to imports.

- [x] **Step 4: Register admin-only routes**

In `backend/internal/handler/router.go`, register routes before `repoGroup.GET("/:id", ...)`:

```go
repoGroup.POST("/repair-webhooks", auth.RequireAdmin(), repoHandler.RepairFailedWebhooks)
repoGroup.POST("/:id/repair-webhook", auth.RequireAdmin(), repoHandler.RepairWebhook)
```

- [x] **Step 5: Run handler tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run 'TestRepairWebhook|TestRepairFailedWebhooks' -count=1
```

Expected: PASS.

- [x] **Step 6: Commit handler routes**

Run:

```bash
git add backend/internal/handler/repo.go backend/internal/handler/router.go backend/internal/handler/repo_webhook_repair_test.go
git commit -m "feat(handler): expose webhook repair endpoints"
```

---

## Task 4: Validate Bitbucket Webhook Signatures

**Files:**
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`
- Modify: `backend/internal/webhook/handler.go`
- Modify: `backend/internal/webhook/webhook_coverage_test.go`

- [x] **Step 1: Write failing Bitbucket parser signature tests**

In `backend/internal/scm/bitbucket/bitbucket_test.go`, add helpers:

```go
func signBitbucketBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newSignedBitbucketRequest(body []byte, secret string) *http.Request {
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "pr:opened")
	req.Header.Set("X-Hub-Signature", signBitbucketBody(body, secret))
	return req
}
```

Add tests:

```go
func TestParseWebhookPayloadValidatesStoredSecretSignature(t *testing.T) {
	body := []byte(`{"actor":{"name":"alice"},"repository":{"slug":"repo","project":{"key":"PROJ"}},"pullRequest":{"id":10,"title":"T","fromRef":{"displayId":"feat","repository":{"slug":"repo","project":{"key":"PROJ"}}},"toRef":{"displayId":"main"},"author":{"user":{"name":"bob"}},"links":{"self":[{"href":"https://bb/pr/10"}]}}}`)
	req := newSignedBitbucketRequest(body, "secret")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "secret")
	if err != nil {
		t.Fatalf("ParseWebhookPayload: %v", err)
	}
	if event.RepoFullName != "PROJ/repo" {
		t.Fatalf("repo = %q", event.RepoFullName)
	}
}

func TestParseWebhookPayloadRejectsMissingSignatureWhenSecretStored(t *testing.T) {
	body := []byte(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:refs_changed")

	p, _ := New("https://bb", "tok", zap.NewNop())
	_, err := p.ParseWebhookPayload(req, "secret")
	if !IsInvalidSignature(err) {
		t.Fatalf("err = %v, want invalid signature", err)
	}
}

func TestParseWebhookPayloadRejectsInvalidSignature(t *testing.T) {
	body := []byte(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:refs_changed")
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")

	p, _ := New("https://bb", "tok", zap.NewNop())
	_, err := p.ParseWebhookPayload(req, "secret")
	if !IsInvalidSignature(err) {
		t.Fatalf("err = %v, want invalid signature", err)
	}
}

func TestParseWebhookPayloadAcceptsUnsignedPayloadWhenNoSecretStored(t *testing.T) {
	body := []byte(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:refs_changed")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatalf("ParseWebhookPayload: %v", err)
	}
	if event.Type != scm.EventPush {
		t.Fatalf("type = %q, want push", event.Type)
	}
}
```

Add required imports if absent:

```go
"crypto/hmac"
"crypto/sha256"
"encoding/hex"
```

- [x] **Step 2: Run parser tests and verify failure**

Run:

```bash
cd backend && go test ./internal/scm/bitbucket -run 'TestParseWebhookPayload.*Signature|TestParseWebhookPayloadAcceptsUnsignedPayloadWhenNoSecretStored' -count=1
```

Expected: FAIL because signature helpers and validation do not exist.

- [x] **Step 3: Implement Bitbucket signature validation**

In `backend/internal/scm/bitbucket/bitbucket.go`, add imports:

```go
"crypto/hmac"
"crypto/sha256"
"encoding/hex"
"errors"
```

Add exported sentinel helpers:

```go
var ErrInvalidSignature = errors.New("invalid bitbucket webhook signature")

func IsInvalidSignature(err error) bool {
	return errors.Is(err, ErrInvalidSignature)
}
```

Add validation:

```go
func validateWebhookSignature(body []byte, secret, header string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return fmt.Errorf("%w: missing X-Hub-Signature", ErrInvalidSignature)
	}
	parts := strings.SplitN(header, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return fmt.Errorf("%w: expected sha256 signature", ErrInvalidSignature)
	}
	got, err := hex.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrInvalidSignature)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return fmt.Errorf("%w: mismatch", ErrInvalidSignature)
	}
	return nil
}
```

Call it in `ParseWebhookPayload` immediately after reading `body`:

```go
if err := validateWebhookSignature(body, secret, r.Header.Get("X-Hub-Signature")); err != nil {
	return nil, err
}
```

- [x] **Step 4: Update webhook handler to return 401 for signature failures**

In `backend/internal/webhook/handler.go`, adjust the Bitbucket parse error branch:

```go
if event, err = bbProvider.ParseWebhookPayload(parseReq, secret); err != nil {
	h.logger.Warn("bitbucket webhook parse failed", zap.String("repo", repoFullName), zap.Error(err))
	h.storeDeadLetter(c, rc.ID, "", eventKey, body, err.Error())
	if bitbucket.IsInvalidSignature(err) {
		pkg.Error(c, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	pkg.Error(c, http.StatusBadRequest, "invalid webhook payload")
	return
}
```

Keep the existing dead-letter call before returning.

- [x] **Step 5: Add handler coverage for invalid signature**

In `backend/internal/webhook/webhook_coverage_test.go`, update `TestHandleBitbucketWithWebhookSecret` to include a valid `X-Hub-Signature` header generated from the exact body.

Add a new test:

```go
func TestHandleBitbucketInvalidSignatureStoresDeadLetter(t *testing.T) {
	h, client := setupWebhookTest(t)
	ctx := context.Background()
	repo := client.RepoConfig.Create().
		SetRepoKey("bitbucket.example.com/proj/repo").
		SetName("repo").
		SetFullName("PROJ/repo").
		SetCloneURL("https://bitbucket.example.com/scm/proj/repo.git").
		SetDefaultBranch("main").
		SetWebhookSecret("secret").
		SaveX(ctx)

	body := []byte(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:refs_changed")
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")
	c.Request = req

	h.HandleBitbucket(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", w.Code, w.Body.String())
	}
	count := client.WebhookDeadLetter.Query().
		Where(webhookdeadletter.HasRepoConfigWith(repoconfig.IDEQ(repo.ID))).
		CountX(ctx)
	if count != 1 {
		t.Fatalf("dead letters = %d, want 1", count)
	}
}
```

Use the existing webhook test helper names if they differ; keep the assertions identical.

- [x] **Step 6: Run Bitbucket and webhook tests**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/scm/bitbucket ./internal/webhook -run 'TestParseWebhookPayload.*Signature|TestParseWebhookPayloadAcceptsUnsignedPayloadWhenNoSecretStored|TestHandleBitbucket.*Signature|TestHandleBitbucketWithWebhookSecret' -count=1
```

Expected: PASS.

- [x] **Step 7: Commit signature validation**

Run:

```bash
git add backend/internal/scm/bitbucket/bitbucket.go backend/internal/scm/bitbucket/bitbucket_test.go backend/internal/webhook/handler.go backend/internal/webhook/webhook_coverage_test.go
git commit -m "fix(webhook): validate bitbucket signatures"
```

---

## Task 5: Add Frontend Repair Actions

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/repo.ts`
- Modify: `frontend/src/views/repos/RepoListView.vue`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

- [x] **Step 1: Write failing API wrapper tests**

In `frontend/src/__tests__/api-modules.test.ts`, add expectations near other repo API tests:

```ts
it('repairFailedWebhooks calls POST /repos/repair-webhooks', async () => {
  const { repairFailedWebhooks } = await import('@/api/repo')
  mockClient.post.mockResolvedValue({ data: { data: { summary: { scanned: 0, repaired: 0, already_registered: 0, failed: 0 }, items: [] } } })

  await repairFailedWebhooks({ force: true })

  expect(mockClient.post).toHaveBeenCalledWith('/repos/repair-webhooks', { force: true })
})

it('repairWebhook calls POST /repos/:id/repair-webhook', async () => {
  const { repairWebhook } = await import('@/api/repo')
  mockClient.post.mockResolvedValue({ data: { data: { repo_config_id: 5, webhook_status: 'registered' } } })

  await repairWebhook(5, { force: false })

  expect(mockClient.post).toHaveBeenCalledWith('/repos/5/repair-webhook', { force: false })
})
```

- [x] **Step 2: Write failing repo list UI tests**

In `frontend/src/__tests__/repo-list-view.test.ts`, update the `@/api/repo` mock:

```ts
repairFailedWebhooks: vi.fn(),
```

Add:

```ts
it('shows batch webhook repair only for admins', async () => {
  const repos = [
    { id: 1, name: 'repo', full_name: 'PROJ/repo', status: 'webhook_failed', binding_state: 'bound', edges: { scm_provider: { id: 1, name: 'Bitbucket', type: 'bitbucket_server' } } },
  ]

  const admin = await mountRepoList(repos, '/repos', { admin: true })
  expect(admin.wrapper.text()).toContain('Repair failed webhooks')

  const user = await mountRepoList(repos, '/repos', { admin: false })
  expect(user.wrapper.text()).not.toContain('Repair failed webhooks')
})

it('runs batch webhook repair and refreshes repo list', async () => {
  const { repairFailedWebhooks, listRepos } = await import('@/api/repo')
  ;(repairFailedWebhooks as any).mockResolvedValue({
    data: {
      data: {
        summary: { scanned: 2, repaired: 1, already_registered: 0, failed: 1 },
        items: [],
      },
    },
  })
  const { wrapper } = await mountRepoList(sampleRepos, '/repos', { admin: true })

  await wrapper.find('[data-testid="repo-repair-webhooks-button"]').trigger('click')
  await flushPromises()

  expect(repairFailedWebhooks).toHaveBeenCalledWith({ force: false })
  expect(listRepos).toHaveBeenCalledTimes(2)
  expect(wrapper.text()).toContain('Webhook repair complete')
  expect(wrapper.text()).toContain('1 repaired')
})
```

- [x] **Step 3: Write failing repo detail UI tests**

In `frontend/src/__tests__/repo-detail-view.test.ts`, update the `@/api/repo` mock:

```ts
repairWebhook: vi.fn(),
```

Add:

```ts
it('shows repair webhook action for admin bound webhook_failed repo', async () => {
  const { getRepo } = await import('@/api/repo')
  ;(getRepo as any).mockResolvedValue({
    data: {
      data: {
        id: 9,
        name: 'repo',
        full_name: 'PROJ/repo',
        clone_url: 'https://bitbucket.example.com/scm/proj/repo.git',
        default_branch: 'main',
        status: 'webhook_failed',
        binding_state: 'bound',
        webhook_id: 'old-hook',
        created_at: '2026-06-06T00:00:00Z',
        edges: { scm_provider: { id: 1, name: 'Bitbucket', type: 'bitbucket_server' } },
      },
    },
  })

  const { wrapper } = await mountRepoDetail({ admin: true })

  expect(wrapper.text()).toContain('Repair webhook')
  expect(wrapper.text()).toContain('Force replace')
})

it('repairs webhook from repo detail', async () => {
  const { repairWebhook } = await import('@/api/repo')
  ;(repairWebhook as any).mockResolvedValue({
    data: { data: { repo_config_id: 9, status: 'active', webhook_status: 'registered', webhook_id: '99' } },
  })

  const { wrapper } = await mountRepoDetail({ admin: true, repoStatus: 'webhook_failed', bindingState: 'bound' })

  await wrapper.find('[data-testid="repo-repair-webhook-button"]').trigger('click')
  await flushPromises()

  expect(repairWebhook).toHaveBeenCalledWith(9, { force: false })
  expect(wrapper.text()).toContain('Webhook repaired')
})
```

Use the local mount helper's existing option names. If they differ, extend the helper so it can set `repo.status`, `repo.binding_state`, and `auth.isAdmin`.

- [x] **Step 4: Run frontend focused tests and verify failure**

Run:

```bash
cd frontend && pnpm test -- api-modules repo-list-view repo-detail-view
```

Expected: FAIL because the API functions, types, and UI controls do not exist.

- [x] **Step 5: Add frontend types and API wrappers**

In `frontend/src/types/index.ts`, add `webhook_id` to `RepoConfig`:

```ts
webhook_id?: string | null
```

Add repair types:

```ts
export interface RepoWebhookRepairRequest {
  force: boolean
}

export interface RepoWebhookRepairSummary {
  scanned: number
  repaired: number
  already_registered: number
  failed: number
}

export interface RepoWebhookRepairItem {
  repo_config_id: number
  full_name: string
  previous_status: string
  status: string
  webhook_status: 'registered' | 'already_registered' | 'failed'
  webhook_id?: string
  callback_url?: string
  error?: string
}

export interface RepoWebhookRepairBatchResult {
  summary: RepoWebhookRepairSummary
  items: RepoWebhookRepairItem[]
}
```

In `frontend/src/api/repo.ts`, update imports and add functions:

```ts
import type { ApiResponse, PagedResponse, RepoAutoBindResult, RepoConfig, RepoWebhookRepairBatchResult, RepoWebhookRepairItem, RepoWebhookRepairRequest } from '@/types'
```

```ts
export function repairFailedWebhooks(data: RepoWebhookRepairRequest = { force: false }) {
  return client.post<ApiResponse<RepoWebhookRepairBatchResult>>('/repos/repair-webhooks', data)
}

export function repairWebhook(id: number, data: RepoWebhookRepairRequest = { force: false }) {
  return client.post<ApiResponse<RepoWebhookRepairItem>>(`/repos/${id}/repair-webhook`, data)
}
```

- [x] **Step 6: Add repo list batch repair UI**

In `frontend/src/views/repos/RepoListView.vue`, import `repairFailedWebhooks`:

```ts
import { autoBindUnboundRepos, createRepoDirect, repairFailedWebhooks } from '@/api/repo'
```

Add state:

```ts
const webhookRepairLoading = ref(false)
const webhookRepairMessage = ref('')
const webhookRepairError = ref('')
```

Add handler:

```ts
async function handleRepairFailedWebhooks() {
  webhookRepairLoading.value = true
  webhookRepairMessage.value = ''
  webhookRepairError.value = ''
  try {
    const res = await repairFailedWebhooks({ force: false })
    const summary = res.data.data?.summary
    if (summary) {
      webhookRepairMessage.value = `${t('repos.webhookRepairComplete')}: ${t('repos.webhookRepairSummary', {
        repaired: summary.repaired,
        alreadyRegistered: summary.already_registered,
        failed: summary.failed,
      })}`
    } else {
      webhookRepairMessage.value = t('repos.webhookRepairComplete')
    }
    await repoStore.fetchRepos()
  } catch (error: any) {
    webhookRepairError.value = error?.response?.data?.message || t('repos.webhookRepairFailed')
  } finally {
    webhookRepairLoading.value = false
  }
}
```

Add the admin button next to auto-bind:

```vue
<button
  v-if="auth.isAdmin"
  data-testid="repo-repair-webhooks-button"
  class="text-sm font-medium text-blue-700 hover:text-blue-900 disabled:opacity-50"
  :disabled="webhookRepairLoading"
  @click="handleRepairFailedWebhooks"
>
  {{ webhookRepairLoading ? t('repos.webhookRepairing') : t('repos.repairWebhooks') }}
</button>
```

Add messages below auto-bind messages:

```vue
<div v-if="webhookRepairMessage" class="mt-4 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800">
  {{ webhookRepairMessage }}
</div>
<div v-if="webhookRepairError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
  {{ webhookRepairError }}
</div>
```

- [x] **Step 7: Add repo detail repair UI**

In `frontend/src/views/repos/RepoDetailView.vue`, import `repairWebhook`:

```ts
import { getRepo, repairWebhook, updateRepo } from '@/api/repo'
```

Add state and computed:

```ts
const webhookRepairing = ref(false)
const webhookRepairForce = ref(false)
const webhookRepairMessage = ref('')
const webhookRepairError = ref('')
const canRepairWebhook = computed(() => auth.isAdmin && repo.value?.binding_state === 'bound' && repo.value?.status === 'webhook_failed')
```

Add handler:

```ts
async function handleRepairWebhook() {
  webhookRepairing.value = true
  webhookRepairMessage.value = ''
  webhookRepairError.value = ''
  try {
    const res = await repairWebhook(repoId, { force: webhookRepairForce.value })
    const item = res.data.data
    webhookRepairMessage.value = item?.webhook_status === 'registered'
      ? t('repoDetail.webhookRepaired')
      : t('repoDetail.webhookRepairComplete')
    await refreshRepo()
  } catch (error: any) {
    webhookRepairError.value = error?.response?.data?.message || t('repoDetail.webhookRepairFailed')
  } finally {
    webhookRepairing.value = false
  }
}
```

In the repo health section action area, add:

```vue
<div v-if="canRepairWebhook" class="mt-4 flex flex-col gap-3 rounded-md border border-amber-200 bg-amber-50 p-3 sm:flex-row sm:items-center sm:justify-between">
  <div class="text-sm text-amber-900">
    <div class="font-medium">{{ t('repoDetail.webhookRepairNeeded') }}</div>
    <label v-if="repo.webhook_id" class="mt-2 inline-flex items-center gap-2 text-xs">
      <input v-model="webhookRepairForce" type="checkbox" class="rounded border-amber-300" />
      <span>{{ t('repoDetail.forceReplaceWebhook') }}</span>
    </label>
  </div>
  <button
    data-testid="repo-repair-webhook-button"
    class="rounded-md bg-amber-700 px-3 py-2 text-sm font-medium text-white hover:bg-amber-800 disabled:opacity-50"
    :disabled="webhookRepairing"
    @click="handleRepairWebhook"
  >
    {{ webhookRepairing ? t('repoDetail.webhookRepairing') : t('repoDetail.repairWebhook') }}
  </button>
</div>
<div v-if="webhookRepairMessage" class="mt-3 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800">{{ webhookRepairMessage }}</div>
<div v-if="webhookRepairError" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ webhookRepairError }}</div>
```

- [x] **Step 8: Add i18n strings**

In `frontend/src/i18n.ts`, add English strings:

```ts
'repos.repairWebhooks': 'Repair failed webhooks',
'repos.webhookRepairing': 'Repairing webhooks...',
'repos.webhookRepairComplete': 'Webhook repair complete',
'repos.webhookRepairFailed': 'Webhook repair failed',
'repos.webhookRepairSummary': '{repaired} repaired · {alreadyRegistered} already registered · {failed} failed',
'repoDetail.repairWebhook': 'Repair webhook',
'repoDetail.webhookRepairing': 'Repairing webhook...',
'repoDetail.webhookRepairNeeded': 'Webhook registration failed for this bound repository.',
'repoDetail.forceReplaceWebhook': 'Force replace',
'repoDetail.webhookRepaired': 'Webhook repaired',
'repoDetail.webhookRepairComplete': 'Webhook repair complete',
'repoDetail.webhookRepairFailed': 'Webhook repair failed',
```

Add Chinese strings:

```ts
'repos.repairWebhooks': '修复失败的 Webhook',
'repos.webhookRepairing': 'Webhook 修复中...',
'repos.webhookRepairComplete': 'Webhook 修复完成',
'repos.webhookRepairFailed': 'Webhook 修复失败',
'repos.webhookRepairSummary': '已修复 {repaired} · 已存在 {alreadyRegistered} · 失败 {failed}',
'repoDetail.repairWebhook': '修复 Webhook',
'repoDetail.webhookRepairing': 'Webhook 修复中...',
'repoDetail.webhookRepairNeeded': '此已绑定仓库的 Webhook 注册失败。',
'repoDetail.forceReplaceWebhook': '强制替换',
'repoDetail.webhookRepaired': 'Webhook 已修复',
'repoDetail.webhookRepairComplete': 'Webhook 修复完成',
'repoDetail.webhookRepairFailed': 'Webhook 修复失败',
```

- [x] **Step 9: Run frontend focused tests**

Run:

```bash
cd frontend && pnpm test -- api-modules repo-list-view repo-detail-view
```

Expected: PASS.

- [x] **Step 10: Commit frontend repair UI**

Run:

```bash
git add frontend/src/types/index.ts frontend/src/api/repo.ts frontend/src/views/repos/RepoListView.vue frontend/src/views/repos/RepoDetailView.vue frontend/src/i18n.ts frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/repo-list-view.test.ts frontend/src/__tests__/repo-detail-view.test.ts
git commit -m "feat(frontend): add webhook repair actions"
```

---

## Task 6: Update Deploy Config, Docs, And Run Verification

**Files:**
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md`
- Modify: `docs/superpowers/specs/2026-06-06-repo-webhook-repair-design.md`

- [ ] **Step 1: Update deploy examples**

In `deploy/config.example.yaml`, add under `server`:

```yaml
  # Public backend origin reachable by GitHub/Bitbucket webhooks.
  # Production deployments should set this to the externally routed HTTPS origin.
  public_url: "http://localhost:8081"
```

In `deploy/.env.example`, add:

```dotenv
AE_SERVER_PUBLIC_URL=http://localhost:8081
```

In each production-style compose file (`deploy/docker-compose.yml`, `deploy/docker-compose.bootstrap.yml`, `deploy/docker-compose.external.yml`), add the backend environment entry beside `AE_SERVER_FRONTEND_URL`:

```yaml
      AE_SERVER_PUBLIC_URL: "${AE_SERVER_PUBLIC_URL:-}"
```

In `deploy/docker-compose.dev.yml` and `deploy/docker-compose.local.yml`, add:

```yaml
      AE_SERVER_PUBLIC_URL: "http://localhost:${LOCAL_SERVER_PORT:-18081}"
```

- [ ] **Step 2: Update architecture docs**

In `docs/architecture.md`, update the SCM/repo operations section with:

```md
- Repository webhook registration uses `server.public_url` / `AE_SERVER_PUBLIC_URL` as the externally reachable backend origin. Callback URLs are derived as `/api/v1/webhooks/github` and `/api/v1/webhooks/bitbucket`; repair and registration must not derive these URLs from request `Host` headers.
- `webhook_failed` is an operational repo health status, not an attribution opt-out. Bound repos in this state remain eligible for local hook reporting, and admins can repair them through the webhook repair endpoints without deleting repo history.
- Bitbucket Server webhooks with a stored secret require `X-Hub-Signature: sha256=<hex>` validation over the exact request body.
```

Place the text in the current repo/SCM module description, not in historical design narrative.

- [ ] **Step 3: Update related specs**

In `docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md`, add a short relationship note near the webhook registration section:

```md
Webhook registration remains best-effort during auto-bind. Auto-bind only processes unbound repos; already-bound repos in `webhook_failed` are repaired by the follow-up webhook repair contract in `2026-06-06-repo-webhook-repair-design.md`.
```

In `docs/superpowers/specs/2026-06-06-repo-webhook-repair-design.md`, change status after implementation and verification:

```md
**Status:** Implemented
```

- [ ] **Step 4: Run backend focused verification**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/config ./internal/scm/bitbucket ./internal/scm/github ./internal/repo ./internal/handler ./internal/webhook -count=1
```

Expected: PASS.

- [ ] **Step 5: Run backend full verification**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 6: Run frontend focused verification**

Run:

```bash
cd frontend && pnpm test -- api-modules repo-list-view repo-detail-view
```

Expected: PASS.

- [ ] **Step 7: Run frontend full verification**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [ ] **Step 8: Run static diff checks**

Run:

```bash
git diff --check
git status --short
```

Expected:

- `git diff --check` exits 0.
- `git status --short` shows only the intended implementation files plus any pre-existing unrelated dirty files that were present before this work.

- [ ] **Step 9: Manual Bitbucket verification on a real admin PAT**

Use a Bitbucket Server repo where the PAT user has repository admin permission.

```bash
export AE_BASE_URL="https://ai-efficiency.example.com"
export AE_TOKEN="<admin-login-token>"
curl -sS -X POST "$AE_BASE_URL/api/v1/repos/<repo_config_id>/repair-webhook" \
  -H "Authorization: Bearer $AE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"force":false}' | jq .
```

Expected response fields:

```json
{
  "webhook_status": "registered",
  "status": "active",
  "callback_url": "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket"
}
```

Then verify in Bitbucket Server:

```bash
curl -sS "$BITBUCKET_BASE_URL/rest/api/1.0/projects/<PROJECT>/repos/<repo>/webhooks" \
  -H "Authorization: Bearer $BITBUCKET_PAT" | jq '.values[] | {id, name, url, active}'
```

Expected:

- One active webhook named `ai-efficiency`.
- URL ends with `/api/v1/webhooks/bitbucket`.
- A test delivery or PR event reaches ai-efficiency.
- A manually replayed payload with an invalid `X-Hub-Signature` receives HTTP `401`.

- [ ] **Step 10: Commit deploy/docs and final verification notes**

Run:

```bash
git add deploy/config.example.yaml deploy/.env.example deploy/docker-compose.yml deploy/docker-compose.bootstrap.yml deploy/docker-compose.external.yml deploy/docker-compose.dev.yml deploy/docker-compose.local.yml docs/architecture.md docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md docs/superpowers/specs/2026-06-06-repo-webhook-repair-design.md
git commit -m "docs(webhook): document repair runtime configuration"
```

---

## Final Verification Checklist

- [ ] Backend focused tests pass.
- [ ] Backend full `go test ./...` passes.
- [ ] Frontend focused tests pass.
- [ ] Frontend full `pnpm test` passes.
- [ ] `git diff --check` passes.
- [ ] No webhook secret appears in frontend types, API response rendering, logs, docs examples, or committed fixtures.
- [ ] `webhook_failed` repos remain eligible for local hook reporting.
- [ ] Single repair rejects unbound repos with `409 repo_unbound`.
- [ ] Batch repair scans only bound `webhook_failed` repos.
- [ ] Bitbucket webhook registration uses a non-empty callback URL.
- [ ] Bitbucket inbound requests with stored secrets require valid `X-Hub-Signature`.
- [ ] Real Bitbucket Server manual verification is completed or explicitly documented as not run due to environment access.
