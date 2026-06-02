# Repo Auto Binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically bind newly discovered and admin-repaired unbound repos to a deterministic Code Platform when exactly one active provider matches the repo remote host.

**Architecture:** Keep `RepoConfig -> ScmProvider` as the only repo/provider binding and add focused auto-binding logic inside `backend/internal/repo`. Auto-binding runs only on repo creation and an admin batch repair endpoint; hook eligibility paths stay read-only. The frontend adds a small admin-only batch action on the repo list and keeps the existing manual binding control for skipped repos.

**Tech Stack:** Go, Gin, Ent, zap, Vue 3, Pinia, TypeScript, Vitest, TailwindCSS.

**Status:** In progress. Task 1 is being executed in this session.

---

## File Map

### Backend Auto-Binding Core

- Create: `backend/internal/repo/auto_bind.go`
  - Own host canonicalization, deterministic provider matching, batch result types, auto-bind orchestration, and post-bind metadata/webhook work.
- Create: `backend/internal/repo/auto_bind_test.go`
  - Cover host matching, GitHub SaaS normalization, no-match, ambiguity, inactive providers, batch repair, and post-bind result accounting.
- Modify: `backend/internal/repo/service.go`
  - Add an optional test hook for post-bind work, call auto-bind from new repo creation and `CreateDirect` when no provider was explicitly supplied.
- Modify: `backend/internal/repo/repo_test.go`
  - Cover `EnsureFromRemote` and `CreateDirect` auto-binding integration without real SCM network calls.

### Backend API

- Modify: `backend/internal/handler/repo.go`
  - Add `AutoBindUnbound` handler method.
- Modify: `backend/internal/handler/router.go`
  - Add admin-only `POST /api/v1/repos/auto-bind-unbound` before `/:id` routes.
- Create: `backend/internal/handler/repo_auto_bind_test.go`
  - Cover admin route contract, non-admin rejection, and no-match response shape.

### Frontend

- Modify: `frontend/src/types/index.ts`
  - Add response types for repo auto-bind summary/items.
- Modify: `frontend/src/api/repo.ts`
  - Add `autoBindUnboundRepos()`.
- Modify: `frontend/src/views/repos/RepoListView.vue`
  - Add admin-only auto-bind action, loading state, result summary, and list refresh.
- Modify: `frontend/src/i18n.ts`
  - Add English and Chinese strings for the action and summary.
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
  - Cover admin visibility, non-admin hidden state, API call, and summary rendering.

### Docs And Verification

- Modify: `docs/architecture.md`
  - Update current repo module responsibility/runtime wording once implementation lands.
- Run backend focused tests:
  - `cd backend && go test ./internal/repo ./internal/handler`
- Run backend full tests:
  - `cd backend && go test ./...`
- Run frontend repo-list test:
  - `cd frontend && pnpm test -- repo-list-view`
- Run frontend full tests:
  - `cd frontend && pnpm test`

---

## Task 1: Add Deterministic Provider Matching

**Files:**
- Create: `backend/internal/repo/auto_bind.go`
- Create: `backend/internal/repo/auto_bind_test.go`

- [x] **Step 1: Write failing tests for host canonicalization and matching**

Create `backend/internal/repo/auto_bind_test.go` with:

```go
package repo

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func newAutoBindTestService(t *testing.T) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	return client, svc
}

func createAutoBindProvider(t *testing.T, client *ent.Client, name string, providerType scmprovider.Type, baseURL string, status scmprovider.Status) *ent.ScmProvider {
	t.Helper()
	provider, err := client.ScmProvider.Create().
		SetName(name).
		SetType(providerType).
		SetBaseURL(baseURL).
		SetStatus(status).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider %s: %v", name, err)
	}
	return provider
}

func createAutoBindRepo(t *testing.T, client *ent.Client, repoKey, fullName, cloneURL string, status repoconfig.Status) *ent.RepoConfig {
	t.Helper()
	repo, err := client.RepoConfig.Create().
		SetRepoKey(repoKey).
		SetName("platform").
		SetFullName(fullName).
		SetCloneURL(cloneURL).
		SetDefaultBranch("main").
		SetStatus(status).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo %s: %v", repoKey, err)
	}
	return repo
}

func TestCanonicalProviderHostMapsGitHubAPIToCloneHost(t *testing.T) {
	host, ok := canonicalProviderHost(&ent.ScmProvider{
		Type:    scmprovider.TypeGithub,
		BaseURL: "https://api.github.com",
	})
	if !ok {
		t.Fatal("canonicalProviderHost returned not ok")
	}
	if host != "github.com" {
		t.Fatalf("host = %q, want github.com", host)
	}
}

func TestCanonicalRepoHostHandlesGitHubSSHRemote(t *testing.T) {
	host, ok := canonicalRepoHost(&ent.RepoConfig{
		RepoKey:  "github.com/acme/platform",
		CloneURL: "git@github.com:acme/platform.git",
	})
	if !ok {
		t.Fatal("canonicalRepoHost returned not ok")
	}
	if host != "github.com" {
		t.Fatalf("host = %q, want github.com", host)
	}
}

func TestFindAutoBindProviderMatchesSingleGitHubSaaSProvider(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	want := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if reason != AutoBindMatched {
		t.Fatalf("reason = %q, want %q", reason, AutoBindMatched)
	}
	if provider == nil || provider.ID != want.ID {
		t.Fatalf("provider = %#v, want id %d", provider, want.ID)
	}
}

func TestFindAutoBindProviderSkipsNoMatch(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "Bitbucket", scmprovider.TypeBitbucketServer, "https://bitbucket.example.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindNoMatch {
		t.Fatalf("reason = %q, want %q", reason, AutoBindNoMatch)
	}
}

func TestFindAutoBindProviderSkipsAmbiguousSameHost(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub A", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	createAutoBindProvider(t, client, "GitHub B", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindAmbiguous {
		t.Fatalf("reason = %q, want %q", reason, AutoBindAmbiguous)
	}
}

func TestFindAutoBindProviderIgnoresInactiveProviders(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusInactive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindNoMatch {
		t.Fatalf("reason = %q, want %q", reason, AutoBindNoMatch)
	}
}
```

- [x] **Step 2: Run the focused repo tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestCanonical|TestFindAutoBindProvider' -v
```

Expected: FAIL with errors for undefined names such as `canonicalProviderHost`, `canonicalRepoHost`, `AutoBindMatched`, and `findAutoBindProvider`.

- [x] **Step 3: Add the matching implementation**

Create `backend/internal/repo/auto_bind.go` with:

```go
package repo

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"go.uber.org/zap"
)

const (
	AutoBindMatched         = "matched"
	AutoBindAlreadyBound    = "already_bound"
	AutoBindNoMatch         = "no_match"
	AutoBindAmbiguous       = "ambiguous"
	AutoBindInvalidRepoHost = "invalid_repo_host"
	AutoBindProviderError   = "provider_error"

	AutoBindWebhookSkipped    = "skipped"
	AutoBindWebhookRegistered = "registered"
	AutoBindWebhookFailed     = "failed"
)

type AutoBindSummary struct {
	Scanned          int `json:"scanned"`
	Bound            int `json:"bound"`
	AlreadyBound     int `json:"already_bound"`
	SkippedNoMatch   int `json:"skipped_no_match"`
	SkippedAmbiguous int `json:"skipped_ambiguous"`
	WebhookFailed    int `json:"webhook_failed"`
	Errors           int `json:"errors"`
}

type AutoBindBatchResult struct {
	Summary AutoBindSummary  `json:"summary"`
	Items   []AutoBindResult `json:"items"`
}

type AutoBindResult struct {
	RepoConfigID    int    `json:"repo_config_id"`
	RepoKey         string `json:"repo_key,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	Result          string `json:"result"`
	SCMProviderID   int    `json:"scm_provider_id,omitempty"`
	SCMProviderName string `json:"scm_provider_name,omitempty"`
	WebhookStatus   string `json:"webhook_status,omitempty"`
	Error           string `json:"error,omitempty"`
}

type autoBindPostBindFunc func(ctx context.Context, repoID, providerID int) (string, error)

func canonicalRepoHost(repo *ent.RepoConfig) (string, bool) {
	if repo == nil {
		return "", false
	}
	if identity, err := DeriveRepoIdentity(repo.CloneURL); err == nil {
		return hostFromRepoKey(identity.RepoKey)
	}
	return hostFromRepoKey(repo.RepoKey)
}

func canonicalProviderHost(provider *ent.ScmProvider) (string, bool) {
	if provider == nil {
		return "", false
	}
	host, ok := hostFromURL(provider.BaseURL)
	if !ok {
		return "", false
	}
	if provider.Type == scmprovider.TypeGithub && host == "api.github.com" {
		host = "github.com"
	}
	return host, true
}

func hostFromRepoKey(repoKey string) (string, bool) {
	repoKey = strings.Trim(strings.TrimSpace(repoKey), "/")
	if repoKey == "" {
		return "", false
	}
	parts := strings.Split(repoKey, "/")
	if parts[0] == "" {
		return "", false
	}
	return normalizeHost(parts[0]), true
}

func hostFromURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.Contains(raw, "@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		userSplit := strings.SplitN(raw, "@", 2)
		hostPath := userSplit[1]
		hostSplit := strings.SplitN(hostPath, ":", 2)
		if hostSplit[0] == "" {
			return "", false
		}
		return normalizeHost(hostSplit[0]), true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	return normalizeHost(parsed.Host), true
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if withoutPort, port, err := net.SplitHostPort(host); err == nil && (port == "80" || port == "443") {
		host = withoutPort
	}
	host = strings.TrimSuffix(host, ".")
	return host
}

func (s *Service) findAutoBindProvider(ctx context.Context, repo *ent.RepoConfig) (*ent.ScmProvider, string, error) {
	repoHost, ok := canonicalRepoHost(repo)
	if !ok {
		return nil, AutoBindInvalidRepoHost, nil
	}
	providers, err := s.entClient.ScmProvider.Query().
		Where(scmprovider.StatusEQ(scmprovider.StatusActive)).
		All(ctx)
	if err != nil {
		return nil, AutoBindProviderError, fmt.Errorf("list active scm providers: %w", err)
	}

	matches := make([]*ent.ScmProvider, 0, 1)
	for _, provider := range providers {
		providerHost, ok := canonicalProviderHost(provider)
		if !ok {
			continue
		}
		if providerHost == repoHost {
			matches = append(matches, provider)
		}
	}

	switch len(matches) {
	case 0:
		return nil, AutoBindNoMatch, nil
	case 1:
		return matches[0], AutoBindMatched, nil
	default:
		return nil, AutoBindAmbiguous, nil
	}
}

func baseAutoBindResult(repo *ent.RepoConfig, result string) AutoBindResult {
	item := AutoBindResult{Result: result, WebhookStatus: AutoBindWebhookSkipped}
	if repo != nil {
		item.RepoConfigID = repo.ID
		item.RepoKey = repo.RepoKey
		item.FullName = repo.FullName
	}
	return item
}

func (r AutoBindResult) addToSummary(summary *AutoBindSummary) {
	summary.Scanned++
	switch r.Result {
	case AutoBindMatched:
		summary.Bound++
	case AutoBindAlreadyBound:
		summary.AlreadyBound++
	case AutoBindNoMatch, AutoBindInvalidRepoHost:
		summary.SkippedNoMatch++
	case AutoBindAmbiguous:
		summary.SkippedAmbiguous++
	case AutoBindProviderError:
		summary.Bound++
		summary.Errors++
	}
	if r.WebhookStatus == AutoBindWebhookFailed {
		summary.WebhookFailed++
	}
}

func (s *Service) logAutoBindResult(result AutoBindResult) {
	if s == nil || s.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.Int("repo_config_id", result.RepoConfigID),
		zap.String("repo_key", result.RepoKey),
		zap.String("full_name", result.FullName),
		zap.String("result", result.Result),
		zap.Int("scm_provider_id", result.SCMProviderID),
		zap.String("webhook_status", result.WebhookStatus),
	}
	if result.Error != "" {
		fields = append(fields, zap.String("error", result.Error))
	}
	s.logger.Info("repo auto-bind result", fields...)
}
```

- [x] **Step 4: Run matching tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestCanonical|TestFindAutoBindProvider' -v
```

Expected: PASS.

- [x] **Step 5: Commit matching helpers**

```bash
git add backend/internal/repo/auto_bind.go backend/internal/repo/auto_bind_test.go
git commit -m "feat(repo): match repos to code platforms by host"
```

---

## Task 2: Add Repo Auto-Bind Service And Batch Repair

**Files:**
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/auto_bind.go`
- Modify: `backend/internal/repo/auto_bind_test.go`

- [ ] **Step 1: Write failing service tests for binding, ambiguity, and batch repair**

First update the import block in `backend/internal/repo/auto_bind_test.go` so it includes `fmt`:

```go
import (
	"context"
	"fmt"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)
```

Then update `newAutoBindTestService` in the same file:

```go
func newAutoBindTestService(t *testing.T) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookRegistered, nil
	}
	return client, svc
}
```

Append to `backend/internal/repo/auto_bind_test.go`:

```go
func TestAutoBindRepoBindsSingleMatchedProvider(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindMatched {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindMatched)
	}
	if result.SCMProviderID != provider.ID {
		t.Fatalf("provider id = %d, want %d", result.SCMProviderID, provider.ID)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("loaded provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func TestAutoBindRepoKeepsAmbiguousRepoUnbound(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub A", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	createAutoBindProvider(t, client, "GitHub B", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindAmbiguous {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindAmbiguous)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider != nil {
		t.Fatalf("provider = %#v, want nil", loaded.Edges.ScmProvider)
	}
}

func TestAutoBindRepoKeepsBindingOnPostBindError(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookFailed, fmt.Errorf("provider verification failed")
	}
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindProviderError {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindProviderError)
	}
	if result.WebhookStatus != AutoBindWebhookFailed {
		t.Fatalf("webhook status = %q, want %q", result.WebhookStatus, AutoBindWebhookFailed)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func TestAutoBindUnboundProcessesOnlyUnboundActiveRepos(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	activeRepo := createAutoBindRepo(t, client, "github.com/acme/active", "acme/active", "https://github.com/acme/active.git", repoconfig.StatusActive)
	inactiveRepo := createAutoBindRepo(t, client, "github.com/acme/inactive", "acme/inactive", "https://github.com/acme/inactive.git", repoconfig.StatusInactive)
	boundRepo := createAutoBindRepo(t, client, "github.com/acme/bound", "acme/bound", "https://github.com/acme/bound.git", repoconfig.StatusActive)
	client.RepoConfig.UpdateOneID(boundRepo.ID).SetScmProviderID(provider.ID).SaveX(ctx)

	result, err := svc.AutoBindUnbound(ctx)
	if err != nil {
		t.Fatalf("AutoBindUnbound: %v", err)
	}
	if result.Summary.Scanned != 1 || result.Summary.Bound != 1 {
		t.Fatalf("summary = %+v, want scanned=1 bound=1", result.Summary)
	}
	if len(result.Items) != 1 || result.Items[0].RepoConfigID != activeRepo.ID {
		t.Fatalf("items = %+v, want only active repo %d", result.Items, activeRepo.ID)
	}

	if client.RepoConfig.Query().Where(repoconfig.IDEQ(inactiveRepo.ID), repoconfig.HasScmProvider()).CountX(ctx) != 0 {
		t.Fatal("inactive repo should remain unbound")
	}
}
```

- [ ] **Step 2: Run service tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestAutoBindRepo|TestAutoBindUnbound' -v
```

Expected: FAIL with undefined `AutoBindRepo`, `AutoBindUnbound`, or `autoBindPostBind`.

- [ ] **Step 3: Add the test hook to `Service`**

Modify `backend/internal/repo/service.go`:

```go
type Service struct {
	entClient        *ent.Client
	encryptionKey    string
	logger           *zap.Logger
	autoBindPostBind autoBindPostBindFunc
}
```

Keep `NewService` returning the same public shape:

```go
func NewService(entClient *ent.Client, encryptionKey string, logger *zap.Logger) *Service {
	return &Service{
		entClient:     entClient,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}
```

- [ ] **Step 4: Implement `AutoBindRepo`, `AutoBindUnbound`, and default post-bind work**

Append to `backend/internal/repo/auto_bind.go`:

```go
func (s *Service) AutoBindRepo(ctx context.Context, repoID int) (AutoBindResult, error) {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		WithScmProvider().
		Only(ctx)
	if err != nil {
		return AutoBindResult{}, fmt.Errorf("auto-bind repo: load repo: %w", err)
	}
	if repo.Edges.ScmProvider != nil {
		result := baseAutoBindResult(repo, AutoBindAlreadyBound)
		result.SCMProviderID = repo.Edges.ScmProvider.ID
		result.SCMProviderName = repo.Edges.ScmProvider.Name
		return result, nil
	}

	provider, reason, err := s.findAutoBindProvider(ctx, repo)
	if err != nil {
		result := baseAutoBindResult(repo, reason)
		result.Error = err.Error()
		s.logAutoBindResult(result)
		return result, nil
	}
	if provider == nil {
		result := baseAutoBindResult(repo, reason)
		s.logAutoBindResult(result)
		return result, nil
	}

	if _, err := s.entClient.RepoConfig.UpdateOneID(repo.ID).SetScmProviderID(provider.ID).Save(ctx); err != nil {
		return AutoBindResult{}, fmt.Errorf("auto-bind repo: set scm provider: %w", err)
	}

	result := baseAutoBindResult(repo, AutoBindMatched)
	result.SCMProviderID = provider.ID
	result.SCMProviderName = provider.Name
	result.WebhookStatus = AutoBindWebhookSkipped

	webhookStatus, postErr := s.runAutoBindPostBind(ctx, repo.ID, provider.ID)
	if webhookStatus != "" {
		result.WebhookStatus = webhookStatus
	}
	if postErr != nil {
		result.Result = AutoBindProviderError
		result.Error = postErr.Error()
	}
	s.logAutoBindResult(result)
	return result, nil
}

func (s *Service) AutoBindUnbound(ctx context.Context) (*AutoBindBatchResult, error) {
	repos, err := s.entClient.RepoConfig.Query().
		Where(
			repoconfig.Not(repoconfig.HasScmProvider()),
			repoconfig.StatusIn(repoconfig.StatusActive, repoconfig.StatusWebhookFailed),
		).
		Order(ent.Asc(repoconfig.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("auto-bind unbound repos: list repos: %w", err)
	}

	batch := &AutoBindBatchResult{Items: make([]AutoBindResult, 0, len(repos))}
	for _, repo := range repos {
		item, err := s.AutoBindRepo(ctx, repo.ID)
		if err != nil {
			item = baseAutoBindResult(repo, AutoBindProviderError)
			item.Error = err.Error()
		}
		item.addToSummary(&batch.Summary)
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func (s *Service) runAutoBindPostBind(ctx context.Context, repoID, providerID int) (string, error) {
	if s.autoBindPostBind != nil {
		return s.autoBindPostBind(ctx, repoID, providerID)
	}
	return s.defaultAutoBindPostBind(ctx, repoID, providerID)
}

func (s *Service) defaultAutoBindPostBind(ctx context.Context, repoID, providerID int) (string, error) {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		Only(ctx)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("load bound repo: %w", err)
	}
	provider, err := s.entClient.ScmProvider.Query().
		Where(scmprovider.IDEQ(providerID)).
		WithAPICredential().
		Only(ctx)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("load scm provider: %w", err)
	}
	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("resolve api credential: %w", err)
	}
	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, apiPayload)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("create scm provider: %w", err)
	}

	repoInfo, err := scmProvider.GetRepo(ctx, repo.FullName)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("verify repo with scm provider: %w", err)
	}

	update := s.entClient.RepoConfig.UpdateOneID(repo.ID).
		SetName(repoInfo.Name).
		SetFullName(repoInfo.FullName).
		SetCloneURL(repoInfo.CloneURL).
		SetDefaultBranch(repoInfo.DefaultBranch)

	webhookSecret, err := generateSecret(32)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("generate webhook secret: %w", err)
	}
	webhookID, err := scmProvider.RegisterWebhook(ctx, repoInfo.FullName, []string{"pull_request", "push"}, webhookSecret)
	if err != nil {
		if _, saveErr := update.SetStatus(repoconfig.StatusWebhookFailed).Save(ctx); saveErr != nil {
			return AutoBindWebhookFailed, fmt.Errorf("register webhook: %v; save webhook_failed status: %w", err, saveErr)
		}
		return AutoBindWebhookFailed, err
	}

	if webhookID != "" {
		update.SetWebhookID(webhookID).SetWebhookSecret(webhookSecret)
	}
	if _, err := update.SetStatus(repoconfig.StatusActive).Save(ctx); err != nil {
		return AutoBindWebhookRegistered, fmt.Errorf("save post-bind repo metadata: %w", err)
	}
	return AutoBindWebhookRegistered, nil
}
```

Ensure `backend/internal/repo/auto_bind.go` imports `github.com/ai-efficiency/backend/ent` and both generated predicate packages already shown in Task 1.

At this point the import block in `backend/internal/repo/auto_bind.go` should include `repoconfig`:

```go
import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"go.uber.org/zap"
)
```

- [ ] **Step 5: Run service tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestAutoBindRepo|TestAutoBindUnbound|TestCanonical|TestFindAutoBindProvider' -v
```

Expected: PASS.

- [ ] **Step 6: Commit service auto-binding**

```bash
git add backend/internal/repo/service.go backend/internal/repo/auto_bind.go backend/internal/repo/auto_bind_test.go
git commit -m "feat(repo): auto-bind unbound repositories"
```

---

## Task 3: Integrate Auto-Binding Into Repo Creation And Admin API

**Files:**
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/router.go`
- Create: `backend/internal/handler/repo_auto_bind_test.go`

- [ ] **Step 1: Write failing integration tests for creation paths**

Append to `backend/internal/repo/repo_test.go`:

```go
func TestEnsureFromRemote_AutoBindsSingleMatchedProvider(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	provider := client.ScmProvider.Create().
		SetName("GitHub").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetStatus("active").
		SaveX(ctx)
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookRegistered, nil
	}

	rc, err := svc.EnsureFromRemote(ctx, "https://github.com/acme/platform.git", "main")
	if err != nil {
		t.Fatalf("EnsureFromRemote: %v", err)
	}
	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(rc.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func TestCreateDirect_AutoBindsWhenProviderOmitted(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	provider := client.ScmProvider.Create().
		SetName("GitHub").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetStatus("active").
		SaveX(ctx)
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookRegistered, nil
	}

	rc, err := svc.CreateDirect(ctx, CreateDirectRequest{
		Name:          "platform",
		FullName:      "acme/platform",
		CloneURL:      "https://github.com/acme/platform.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(rc.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}
```

- [ ] **Step 2: Run creation integration tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestEnsureFromRemote_AutoBinds|TestCreateDirect_AutoBinds' -v
```

Expected: FAIL because `EnsureFromRemote` and `CreateDirect` do not yet invoke `AutoBindRepo`.

- [ ] **Step 3: Call auto-bind from new repo creation paths**

Modify the end of `FindOrCreateFromRemote` in `backend/internal/repo/service.go` after the create save succeeds:

```go
	if _, bindErr := s.AutoBindRepo(ctx, rc.ID); bindErr != nil {
		s.logger.Warn("auto-bind newly discovered repo failed", zap.Int("repo_config_id", rc.ID), zap.Error(bindErr))
	}
	return s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(rc.ID)).
		WithScmProvider().
		Only(ctx)
```

Modify `CreateDirect` after `create.Save(ctx)`:

```go
	rc, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("save repo config: %w", err)
	}
	if req.SCMProviderID <= 0 {
		if _, bindErr := s.AutoBindRepo(ctx, rc.ID); bindErr != nil {
			s.logger.Warn("auto-bind direct repo failed", zap.Int("repo_config_id", rc.ID), zap.Error(bindErr))
		}
		return s.entClient.RepoConfig.Query().
			Where(repoconfig.IDEQ(rc.ID)).
			WithScmProvider().
			Only(ctx)
	}
	return rc, nil
```

Keep the existing constraint-conflict requery behavior in `FindOrCreateFromRemote`; only add auto-binding to the branch that actually creates a new repo.

- [ ] **Step 4: Run creation integration tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/repo -run 'TestEnsureFromRemote_AutoBinds|TestCreateDirect_AutoBinds|TestEnsureFromRemote_CreatesUnboundRepo' -v
```

Expected: PASS.

- [ ] **Step 5: Write failing handler tests for the batch endpoint**

Create `backend/internal/handler/repo_auto_bind_test.go` with:

```go
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/auth"
)

func TestAutoBindUnboundRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/auto-bind-unbound", nil, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestAutoBindUnboundRouteReturnsNoMatchSummary(t *testing.T) {
	env := setupFullTestEnv(t)
	env.client.RepoConfig.Create().
		SetName("platform").
		SetFullName("acme/platform").
		SetCloneURL("https://github.com/acme/platform.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/auto-bind-unbound", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if int(summary["scanned"].(float64)) != 1 {
		t.Fatalf("summary = %v, want scanned=1", summary)
	}
	if int(summary["skipped_no_match"].(float64)) != 1 {
		t.Fatalf("summary = %v, want skipped_no_match=1", summary)
	}
}
```

Also add this helper in the same file:

```go
func issueFullTokenForRole(t *testing.T, env *fullTestEnv, username, role string) string {
	t.Helper()
	user := env.client.User.Create().
		SetUsername(username).
		SetEmail(username + "@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(role).
		SaveX(context.Background())
	token, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       user.ID,
		Username: username,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token.AccessToken
}
```

- [ ] **Step 6: Run handler tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestAutoBindUnboundRoute' -v
```

Expected: FAIL with 404 or missing route/handler.

- [ ] **Step 7: Add the handler method and route**

Add to `backend/internal/handler/repo.go`:

```go
// AutoBindUnbound handles POST /api/v1/repos/auto-bind-unbound.
func (h *RepoHandler) AutoBindUnbound(c *gin.Context) {
	result, err := h.repoService.AutoBindUnbound(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, result)
}
```

Add this route in `backend/internal/handler/router.go` inside `repoGroup`, before `repoGroup.GET("/:id", repoHandler.Get)`:

```go
repoGroup.POST("/auto-bind-unbound", auth.RequireAdmin(), repoHandler.AutoBindUnbound)
```

- [ ] **Step 8: Run backend focused tests**

Run:

```bash
cd backend && go test ./internal/repo ./internal/handler
```

Expected: PASS.

- [ ] **Step 9: Commit backend API integration**

```bash
git add backend/internal/repo/service.go backend/internal/repo/repo_test.go backend/internal/handler/repo.go backend/internal/handler/router.go backend/internal/handler/repo_auto_bind_test.go
git commit -m "feat(repo): expose admin auto-bind repair"
```

---

## Task 4: Add Frontend Batch Auto-Bind Action

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/repo.ts`
- Modify: `frontend/src/views/repos/RepoListView.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`

- [ ] **Step 1: Write failing frontend tests**

Modify the `vi.mock('@/api/repo'...)` block in `frontend/src/__tests__/repo-list-view.test.ts`:

```ts
vi.mock('@/api/repo', () => ({
  listRepos: vi.fn().mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } }),
  createRepo: vi.fn(),
  createRepoDirect: vi.fn(),
  deleteRepo: vi.fn(),
  autoBindUnboundRepos: vi.fn(),
}))
```

Import the auth store near the existing imports:

```ts
import { useAuthStore } from '@/stores/auth'
```

Change `mountRepoList` to accept an admin flag and seed the auth store:

```ts
async function mountRepoList(repos?: any[], path = '/repos', options?: { admin?: boolean }) {
  const { listRepos } = await import('@/api/repo')
  if (repos) {
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: repos, total: repos.length, page: 1, page_size: 20 } },
    })
  }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = {
    id: 1,
    username: options?.admin ? 'admin' : 'alice',
    email: options?.admin ? 'admin@example.com' : 'alice@example.com',
    role: options?.admin ? 'admin' : 'user',
    auth_source: 'sso',
  }

  const wrapper = mount(RepoListView, {
    global: { plugins: [pinia, router] },
  })

  await flushPromises()
  await wrapper.vm.$nextTick()

  return { wrapper, router }
}
```

Append these tests:

```ts
it('shows auto-bind action only for admins', async () => {
  const admin = await mountRepoList(sampleRepos, '/repos', { admin: true })
  expect(admin.wrapper.find('[data-testid="repo-auto-bind-button"]').exists()).toBe(true)

  const user = await mountRepoList(sampleRepos, '/repos', { admin: false })
  expect(user.wrapper.find('[data-testid="repo-auto-bind-button"]').exists()).toBe(false)
})

it('runs auto-bind and shows a summary', async () => {
  const { autoBindUnboundRepos, listRepos } = await import('@/api/repo')
  ;(autoBindUnboundRepos as any).mockResolvedValue({
    data: {
      data: {
        summary: {
          scanned: 3,
          bound: 1,
          already_bound: 0,
          skipped_no_match: 1,
          skipped_ambiguous: 1,
          webhook_failed: 0,
          errors: 0,
        },
        items: [],
      },
    },
  })

  const { wrapper } = await mountRepoList(sampleRepos, '/repos', { admin: true })
  await wrapper.get('[data-testid="repo-auto-bind-button"]').trigger('click')
  await flushPromises()

  expect(autoBindUnboundRepos).toHaveBeenCalledTimes(1)
  expect(listRepos).toHaveBeenCalled()
  expect(wrapper.text()).toContain('Auto-bind complete')
  expect(wrapper.text()).toContain('1 bound')
  expect(wrapper.text()).toContain('1 no match')
  expect(wrapper.text()).toContain('1 ambiguous')
})
```

- [ ] **Step 2: Run repo list tests and verify they fail**

Run:

```bash
cd frontend && pnpm test -- repo-list-view
```

Expected: FAIL because `autoBindUnboundRepos`, auth-gated button, and summary text are not implemented.

- [ ] **Step 3: Add frontend response types and API function**

Add to `frontend/src/types/index.ts` after `RepoConfig`:

```ts
export interface RepoAutoBindSummary {
  scanned: number
  bound: number
  already_bound: number
  skipped_no_match: number
  skipped_ambiguous: number
  webhook_failed: number
  errors: number
}

export interface RepoAutoBindItem {
  repo_config_id: number
  repo_key?: string
  full_name?: string
  result: 'matched' | 'already_bound' | 'no_match' | 'ambiguous' | 'invalid_repo_host' | 'provider_error'
  scm_provider_id?: number
  scm_provider_name?: string
  webhook_status?: 'skipped' | 'registered' | 'failed'
  error?: string
}

export interface RepoAutoBindResult {
  summary: RepoAutoBindSummary
  items: RepoAutoBindItem[]
}
```

Modify imports and add the function in `frontend/src/api/repo.ts`:

```ts
import type { ApiResponse, PagedResponse, RepoAutoBindResult, RepoConfig } from '@/types'
```

```ts
export function autoBindUnboundRepos() {
  return client.post<ApiResponse<RepoAutoBindResult>>('/repos/auto-bind-unbound')
}
```

- [ ] **Step 4: Add i18n strings**

Add English keys near the existing `repos.*` entries in `frontend/src/i18n.ts`:

```ts
'repos.autoBind': 'Auto-bind repositories',
'repos.autoBinding': 'Auto-binding...',
'repos.autoBindComplete': 'Auto-bind complete',
'repos.autoBindFailed': 'Auto-bind failed',
'repos.autoBindSummary': '{bound} bound · {noMatch} no match · {ambiguous} ambiguous · {webhookFailed} webhook failed · {errors} errors',
```

Add Chinese keys near the existing Chinese `repos.*` entries:

```ts
'repos.autoBind': '自动绑定仓库',
'repos.autoBinding': '自动绑定中...',
'repos.autoBindComplete': '自动绑定完成',
'repos.autoBindFailed': '自动绑定失败',
'repos.autoBindSummary': '已绑定 {bound} · 未匹配 {noMatch} · 多重匹配 {ambiguous} · Webhook 失败 {webhookFailed} · 错误 {errors}',
```

- [ ] **Step 5: Add the repo list action and summary state**

Modify imports in `frontend/src/views/repos/RepoListView.vue`:

```ts
import { createRepoDirect, autoBindUnboundRepos } from '@/api/repo'
import { useAuthStore } from '@/stores/auth'
```

Add state near the other refs:

```ts
const auth = useAuthStore()
const autoBindLoading = ref(false)
const autoBindMessage = ref('')
const autoBindError = ref('')
```

Add this function near the other action handlers:

```ts
async function handleAutoBindUnbound() {
  autoBindLoading.value = true
  autoBindMessage.value = ''
  autoBindError.value = ''
  try {
    const res = await autoBindUnboundRepos()
    const summary = res.data.data?.summary
    if (summary) {
      autoBindMessage.value = `${t('repos.autoBindComplete')}: ${t('repos.autoBindSummary', {
        bound: summary.bound,
        noMatch: summary.skipped_no_match,
        ambiguous: summary.skipped_ambiguous,
        webhookFailed: summary.webhook_failed,
        errors: summary.errors,
      })}`
    } else {
      autoBindMessage.value = t('repos.autoBindComplete')
    }
    await repoStore.fetchRepos()
  } catch (error: any) {
    autoBindError.value = error?.response?.data?.message || t('repos.autoBindFailed')
  } finally {
    autoBindLoading.value = false
  }
}
```

In the health section header, replace the single review button block:

```vue
<button class="text-sm font-medium text-blue-700 hover:text-blue-900" @click="applyBindingFilter('unbound')">
  {{ t('repos.reviewNeedsBinding') }}
</button>
```

with:

```vue
<div class="flex flex-wrap gap-3">
  <button
    v-if="auth.isAdmin"
    data-testid="repo-auto-bind-button"
    class="text-sm font-medium text-emerald-700 hover:text-emerald-900 disabled:opacity-50"
    :disabled="autoBindLoading"
    @click="handleAutoBindUnbound"
  >
    {{ autoBindLoading ? t('repos.autoBinding') : t('repos.autoBind') }}
  </button>
  <button class="text-sm font-medium text-blue-700 hover:text-blue-900" @click="applyBindingFilter('unbound')">
    {{ t('repos.reviewNeedsBinding') }}
  </button>
</div>
```

Add this message block after the health summary cards:

```vue
<div v-if="autoBindMessage" class="mt-4 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800">
  {{ autoBindMessage }}
</div>
<div v-if="autoBindError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
  {{ autoBindError }}
</div>
```

- [ ] **Step 6: Run repo list tests and verify they pass**

Run:

```bash
cd frontend && pnpm test -- repo-list-view
```

Expected: PASS.

- [ ] **Step 7: Commit frontend auto-bind action**

```bash
git add frontend/src/types/index.ts frontend/src/api/repo.ts frontend/src/views/repos/RepoListView.vue frontend/src/i18n.ts frontend/src/__tests__/repo-list-view.test.ts
git commit -m "feat(frontend): add repository auto-bind action"
```

---

## Task 5: Update Architecture Docs And Run Full Verification

**Files:**
- Modify: `docs/architecture.md`

- [ ] **Step 1: Update current architecture wording**

In `docs/architecture.md`, update the runtime boundary bullets around repo binding to include deterministic auto-binding. Use this wording:

```md
- Repo-to-`scm_provider` binding remains admin-managed, but the backend now performs deterministic auto-binding when exactly one active Code Platform matches a newly created repo's canonical remote host. GitHub SaaS provider URLs such as `https://api.github.com` match `github.com` remotes, while GitHub Enterprise and Bitbucket Server match by canonical host. Existing unbound repos can be repaired through an admin-only batch action; ambiguous and no-match repos remain manually bindable.
```

In the module responsibility table row for `Repo and efficiency`, use this responsibility text:

```md
Explicit repo registration, read-only hook eligibility resolution, deterministic repo binding from configured SCM metadata, PR labeling, and dashboard-facing summary inputs
```

- [ ] **Step 2: Run backend focused verification**

Run:

```bash
cd backend && go test ./internal/repo ./internal/handler
```

Expected: PASS.

- [ ] **Step 3: Run backend full verification**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run frontend focused verification**

Run:

```bash
cd frontend && pnpm test -- repo-list-view
```

Expected: PASS.

- [ ] **Step 5: Run frontend full verification**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [ ] **Step 6: Check worktree and commit docs/verification updates**

Run:

```bash
git status --short
```

Expected: only intentional files from this implementation are modified.

Commit:

```bash
git add docs/architecture.md
git commit -m "docs(architecture): document repo auto binding"
```

- [ ] **Step 7: Final implementation review**

Run:

```bash
git log --oneline -5
git status --short
```

Expected:

- recent commits include the matching, backend API, frontend action, and architecture doc commits
- `git status --short` is empty

Review the implementation against `docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md` and confirm:

- new repo creation attempts deterministic auto-binding
- existing unbound repos are repaired only through the admin batch endpoint
- `resolve-remote` and `hook-eligible` remain read-only
- ambiguous/no-match repos remain unbound
- webhook and provider post-bind failures keep the deterministic binding
- no test, fixture, or example uses real user data or secret material
