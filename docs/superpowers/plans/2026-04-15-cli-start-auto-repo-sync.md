# CLI Start Auto Repo Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `ae-cli start` bootstrap sessions for repos that are not yet in backend storage by auto-creating an unbound `repo_config`, while keeping repo-to-`scm_provider` binding admin-managed.

**Architecture:** Backend adds a stable `repo_key`, moves session bootstrap to a repo `find-or-create` flow, and treats missing SCM binding as an explicit `repo_unbound` runtime state instead of a bootstrap blocker. Frontend surfaces that binding state, lets admins bind or clear a provider on the repo detail page, and disables SCM-dependent actions when the repo is still unbound.

**Tech Stack:** Go, Gin, Ent, Vue 3, Pinia, Vitest

**Status:** ✅ 已完成（2026-04-15）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前 repo identity/bootstrap/unbound UX 代码和最新 spec 重写执行计划。

> **Updated:** 2026-04-15 — 基于 repo/bootstrap/analysis/handler tests、repo-focused frontend tests 与 ae-cli session/client tests 回填 checkbox。

---

## File Map

- Modify: `backend/ent/schema/repoconfig.go`
  - Add `repo_key` and make the `scm_provider` edge optional.
- Create: `backend/internal/repo/identity.go`
  - Normalize Git remotes into a stable `repo_key` and best-effort repo metadata.
- Create: `backend/internal/repo/identity_test.go`
  - Pure normalization tests for GitHub and Bitbucket remotes.
- Modify: `backend/internal/repo/service.go`
  - Add `FindOrCreateFromRemote`, optional provider handling, and typed `repo_unbound` errors.
- Modify: `backend/internal/repo/repo_test.go`
  - Cover unbound repo creation, `repo_key` uniqueness, and `GetSCMProvider` unbound behavior.
- Modify: `backend/internal/sessionbootstrap/service.go`
  - Replace lookup-only repo resolution with `find-or-create`.
- Modify: `backend/internal/sessionbootstrap/service_test.go`
  - Cover bootstrap auto-creating an unbound repo.
- Modify: `backend/internal/handler/session_bootstrap_http_test.go`
  - Lock the HTTP contract for auto-create bootstrap.
- Modify: `backend/cmd/server/main.go`
  - Pass the repo service into session bootstrap wiring.
- Modify: `backend/internal/analysis/service.go`
  - Return `repo_unbound` before clone/scan for unbound repos.
- Modify: `backend/internal/analysis/analysis_service_test.go`
  - Cover `RunScan` on an unbound repo.
- Modify: `backend/internal/handler/analysis.go`
  - Map `repo_unbound` to `409` with a stable error code.
- Modify: `backend/internal/handler/pr.go`
  - Map `repo_unbound` to `409` for PR sync and settle.
- Modify: `backend/internal/handler/repo.go`
  - Accept optional `scm_provider_id` updates and return repo binding state.
- Modify: `backend/internal/handler/handler_mock_test.go`
  - Cover `409 repo_unbound` in optimize/preview/confirm/sync handlers.
- Modify: `frontend/src/types/index.ts`
  - Add `repo_key`, `binding_state`, and nullable `scm_provider_id`.
- Modify: `frontend/src/api/repo.ts`
  - Keep repo update payloads compatible with `scm_provider_id`.
- Modify: `frontend/src/views/repos/RepoListView.vue`
  - Show `Unbound` badges and binding-state filtering.
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
  - Add admin binding controls and disable SCM-dependent actions for unbound repos.
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
  - Cover `Unbound` badge/filter rendering.
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`
  - Cover admin binding controls and disabled unbound actions.
- Modify: `docs/architecture.md`
  - Update current runtime flow to reflect bootstrap `find-or-create` and optional SCM binding.

### Task 1: Add Repo Identity Normalization

**Files:**
- Create: `backend/internal/repo/identity.go`
- Test: `backend/internal/repo/identity_test.go`

- [x] **Step 1: Write the failing test**

```go
package repo

import "testing"

func TestDeriveRepoIdentityNormalizesCommonRemotes(t *testing.T) {
	cases := []struct {
		name     string
		remote   string
		repoKey  string
		fullName string
		repoName string
	}{
		{
			name:     "github https",
			remote:   "https://github.com/acme/platform.git",
			repoKey:  "github.com/acme/platform",
			fullName: "acme/platform",
			repoName: "platform",
		},
		{
			name:     "github ssh",
			remote:   "git@github.com:acme/platform.git",
			repoKey:  "github.com/acme/platform",
			fullName: "acme/platform",
			repoName: "platform",
		},
		{
			name:     "bitbucket http clone",
			remote:   "https://bitbucket.example.com/scm/PROJ/platform.git",
			repoKey:  "bitbucket.example.com/proj/platform",
			fullName: "PROJ/platform",
			repoName: "platform",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveRepoIdentity(tc.remote)
			if err != nil {
				t.Fatalf("DeriveRepoIdentity(%q): %v", tc.remote, err)
			}
			if got.RepoKey != tc.repoKey {
				t.Fatalf("RepoKey = %q, want %q", got.RepoKey, tc.repoKey)
			}
			if got.FullName != tc.fullName {
				t.Fatalf("FullName = %q, want %q", got.FullName, tc.fullName)
			}
			if got.Name != tc.repoName {
				t.Fatalf("Name = %q, want %q", got.Name, tc.repoName)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/repo -run 'TestDeriveRepoIdentityNormalizesCommonRemotes' -v`
Expected: FAIL with `undefined: DeriveRepoIdentity`

- [x] **Step 3: Write minimal implementation**

```go
package repo

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

type RepoIdentity struct {
	RepoKey  string
	Name     string
	FullName string
	CloneURL string
}

func DeriveRepoIdentity(remoteURL string) (RepoIdentity, error) {
	normalized, err := normalizeRemoteURL(remoteURL)
	if err != nil {
		return RepoIdentity{}, err
	}
	segments := splitRepoPath(normalized.Path)
	if len(segments) < 2 {
		return RepoIdentity{}, fmt.Errorf("derive repo identity: unsupported remote path %q", normalized.Path)
	}

	repoName := segments[len(segments)-1]
	fullName := strings.Join(segments[len(segments)-2:], "/")
	if strings.Contains(normalized.Path, "/scm/") && len(segments) >= 2 {
		fullName = strings.ToUpper(segments[len(segments)-2]) + "/" + repoName
	}

	return RepoIdentity{
		RepoKey:  normalized.Host + "/" + strings.Join(segments, "/"),
		Name:     repoName,
		FullName: fullName,
		CloneURL: strings.TrimSpace(remoteURL),
	}, nil
}

func normalizeRemoteURL(remoteURL string) (*url.URL, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if strings.HasPrefix(remoteURL, "git@") && strings.Contains(remoteURL, ":") {
		parts := strings.SplitN(strings.TrimPrefix(remoteURL, "git@"), ":", 2)
		remoteURL = "ssh://" + parts[0] + "/" + parts[1]
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("parse remote url: %w", err)
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(path.Clean(strings.TrimSuffix(u.Path, ".git")), "/")
	return u, nil
}

func splitRepoPath(p string) []string {
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")
	switch {
	case len(parts) >= 3 && parts[0] == "scm":
		return []string{strings.ToLower(parts[1]), strings.ToLower(parts[2])}
	case len(parts) >= 4 && parts[0] == "projects" && parts[2] == "repos":
		return []string{strings.ToLower(parts[1]), strings.ToLower(parts[3])}
	default:
		return []string{strings.ToLower(parts[len(parts)-2]), strings.ToLower(parts[len(parts)-1])}
	}
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/repo -run 'TestDeriveRepoIdentityNormalizesCommonRemotes' -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/internal/repo/identity.go backend/internal/repo/identity_test.go
git commit -m "feat(backend): add repo identity normalization"
```

### Task 2: Support Unbound Repo Persistence and Bootstrap Find-Or-Create

**Files:**
- Modify: `backend/ent/schema/repoconfig.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/sessionbootstrap/service.go`
- Modify: `backend/internal/sessionbootstrap/service_test.go`
- Modify: `backend/internal/handler/session_bootstrap_http_test.go`
- Modify: `backend/cmd/server/main.go`
- Generate: `backend/ent/*`

- [x] **Step 1: Write the failing bootstrap regression test**

```go
func TestBootstrapCreatesUnboundRepoWhenMissing(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SaveX(ctx)

	rp := &fakeRelayProvider{
		findUserByUsernameFn: func(_ context.Context, username string) (*relay.User, error) {
			return &relay.User{ID: 99, Username: username}, nil
		},
	}
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	svc := NewService(client, repoSvc, rp, auth.NewRelayIdentityResolver(rp, "ldap.local"), "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	_, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   "git@github.com:acme/platform.git",
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

	got := client.RepoConfig.Query().OnlyX(ctx)
	if got.CloneURL != "git@github.com:acme/platform.git" {
		t.Fatalf("clone_url = %q, want remote URL", got.CloneURL)
	}
	if got.FullName != "acme/platform" {
		t.Fatalf("full_name = %q, want %q", got.FullName, "acme/platform")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/sessionbootstrap -run 'TestBootstrapCreatesUnboundRepoWhenMissing' -v`
Expected: FAIL with `bootstrap: repo not found`

- [x] **Step 3: Write minimal implementation**

```go
// backend/ent/schema/repoconfig.go
field.String("repo_key").
	NotEmpty(),

edge.From("scm_provider", ScmProvider.Type).
	Ref("repo_configs").
	Unique(),

// backend/internal/repo/service.go
var ErrRepoUnbound = errors.New("repo is not bound to an scm provider")

type CreateDirectRequest struct {
	SCMProviderID *int   `json:"scm_provider_id"`
	RepoKey       string `json:"repo_key" binding:"required"`
	Name          string `json:"name" binding:"required"`
	FullName      string `json:"full_name" binding:"required"`
	CloneURL      string `json:"clone_url" binding:"required"`
	DefaultBranch string `json:"default_branch" binding:"required"`
}

func (s *Service) FindOrCreateFromRemote(ctx context.Context, remoteURL, branch string) (*ent.RepoConfig, error) {
	identity, err := DeriveRepoIdentity(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("find or create repo from remote: %w", err)
	}
	if existing, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.RepoKeyEQ(identity.RepoKey)).
		WithScmProvider().
		Only(ctx); err == nil {
		update := s.entClient.RepoConfig.UpdateOneID(existing.ID).
			SetCloneURL(identity.CloneURL).
			SetName(identity.Name).
			SetFullName(identity.FullName)
		if strings.TrimSpace(branch) != "" {
			update.SetDefaultBranch(branch)
		}
		return update.Save(ctx)
	}
	create := s.entClient.RepoConfig.Create().
		SetRepoKey(identity.RepoKey).
		SetName(identity.Name).
		SetFullName(identity.FullName).
		SetCloneURL(identity.CloneURL).
		SetStatus(repoconfig.StatusActive)
	defaultBranch := strings.TrimSpace(branch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	create.SetDefaultBranch(defaultBranch)
	return create.Save(ctx)
}

func (s *Service) CreateDirect(ctx context.Context, req CreateDirectRequest) (*ent.RepoConfig, error) {
	create := s.entClient.RepoConfig.Create().
		SetRepoKey(req.RepoKey).
		SetName(req.Name).
		SetFullName(req.FullName).
		SetCloneURL(req.CloneURL).
		SetDefaultBranch(req.DefaultBranch).
		SetStatus(repoconfig.StatusActive)
	if req.SCMProviderID != nil {
		create.SetScmProviderID(*req.SCMProviderID)
	}
	return create.Save(ctx)
}

// backend/internal/sessionbootstrap/service.go
type repoBootstrapService interface {
	FindOrCreateFromRemote(ctx context.Context, remoteURL, branch string) (*ent.RepoConfig, error)
}

rc, err := s.repoService.FindOrCreateFromRemote(ctx, req.RepoFullName, req.BranchSnapshot)
if err != nil {
	return nil, fmt.Errorf("bootstrap: resolve repo: %w", err)
}
```

Then regenerate Ent code:

```bash
cd backend && go generate ./ent
```

And update wiring:

```go
// backend/cmd/server/main.go
sessionBootstrapSvc = sessionbootstrap.NewService(
	entClient,
	repoService,
	relayProvider,
	relayIdentityResolver,
	cfg.Relay.Provider,
	cfg.Relay.URL,
	cfg.Relay.DefaultGroupID,
	24*time.Hour,
	cfg.Encryption.Key,
)
```

- [x] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/repo ./internal/sessionbootstrap ./internal/handler -run 'TestBootstrapCreatesUnboundRepoWhenMissing|TestSessionBootstrapHTTP' -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/ent/schema/repoconfig.go backend/ent backend/internal/repo/service.go backend/internal/repo/repo_test.go backend/internal/sessionbootstrap/service.go backend/internal/sessionbootstrap/service_test.go backend/internal/handler/session_bootstrap_http_test.go backend/cmd/server/main.go
git commit -m "feat(backend): auto-create repos during session bootstrap"
```

### Task 3: Return Explicit `repo_unbound` Errors for SCM-Dependent Features

**Files:**
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/analysis/service.go`
- Modify: `backend/internal/analysis/analysis_service_test.go`
- Modify: `backend/internal/handler/analysis.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/handler_mock_test.go`

- [x] **Step 1: Write the failing tests**

```go
func TestRunScan_UnboundRepoReturnsRepoUnbound(t *testing.T) {
	client := testdb.Open(t)
	svc := NewService(client, NewCloner(t.TempDir(), zap.NewNop()), nil, zap.NewNop(), "0000000000000000000000000000000000000000000000000000000000000000")
	rc := client.RepoConfig.Create().
		SetRepoKey("github.com/acme/platform").
		SetName("platform").
		SetFullName("acme/platform").
		SetCloneURL("https://github.com/acme/platform.git").
		SetDefaultBranch("main").
		SaveX(context.Background())

	_, err := svc.RunScan(context.Background(), rc.ID)
	if !errors.Is(err, repo.ErrRepoUnbound) {
		t.Fatalf("RunScan error = %v, want repo.ErrRepoUnbound", err)
	}
}

func TestSyncPRs_GetSCMProviderUnboundReturns409(t *testing.T) {
	env := setupMockTestEnv(t, nil, nil, &mockRepoSCMProvider{
		getSCMProviderFn: func(context.Context, int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, repo.ErrRepoUnbound
		},
	}, &mockPRSyncer{})

	w := doMockRequest(env, "POST", "/api/v1/repos/1/sync-prs", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if !strings.Contains(w.Body.String(), "repo_unbound") {
		t.Fatalf("body = %s, want repo_unbound details", w.Body.String())
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/analysis ./internal/handler -run 'TestRunScan_UnboundRepoReturnsRepoUnbound|TestSyncPRs_GetSCMProviderUnboundReturns409' -v`
Expected:
- `TestRunScan_UnboundRepoReturnsRepoUnbound`: FAIL because `RunScan` tries to clone without a provider
- `TestSyncPRs_GetSCMProviderUnboundReturns409`: FAIL because handler still returns `500`

- [x] **Step 3: Write minimal implementation**

```go
// backend/internal/repo/service.go
func IsRepoUnbound(err error) bool {
	return errors.Is(err, ErrRepoUnbound)
}

func (s *Service) GetSCMProvider(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error) {
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoConfigID)).
		WithScmProvider(func(query *ent.ScmProviderQuery) { query.WithAPICredential() }).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get repo config: %w", err)
	}
	if rc.Edges.ScmProvider == nil {
		return nil, rc, ErrRepoUnbound
	}
	// existing provider construction...
}

// backend/internal/analysis/service.go
if rc.Edges.ScmProvider == nil {
	return nil, repo.ErrRepoUnbound
}

// backend/internal/handler/analysis.go and backend/internal/handler/pr.go
func repoUnboundResponse(c *gin.Context) {
	pkg.ErrorWithDetails(c, http.StatusConflict, "repo is not bound to an scm provider", gin.H{
		"error_code": "repo_unbound",
	})
}

if repo.IsRepoUnbound(err) {
	repoUnboundResponse(c)
	return
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/repo ./internal/analysis ./internal/handler -run 'UnboundRepo|GetSCMProviderUnboundReturns409' -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/internal/repo/service.go backend/internal/analysis/service.go backend/internal/analysis/analysis_service_test.go backend/internal/handler/analysis.go backend/internal/handler/pr.go backend/internal/handler/handler_mock_test.go
git commit -m "fix(backend): return explicit repo unbound errors"
```

### Task 4: Add Admin Binding Controls and Unbound Repo UX in the Frontend

**Files:**
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/repo.ts`
- Modify: `frontend/src/views/repos/RepoListView.vue`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

- [x] **Step 1: Write the failing frontend tests**

```ts
it('shows an Unbound badge for repos without scm_provider', async () => {
  const { wrapper } = await mountRepoList([
    {
      id: 7,
      name: 'repo-unbound',
      repo_key: 'github.com/acme/repo-unbound',
      full_name: 'acme/repo-unbound',
      clone_url: 'https://github.com/acme/repo-unbound.git',
      default_branch: 'main',
      ai_score: 0,
      status: 'active',
      binding_state: 'unbound',
      last_scan_at: null,
      created_at: '2026-01-01T00:00:00Z',
      edges: {},
    },
  ])
  expect(wrapper.text()).toContain('Unbound')
})

it('disables scan and shows binding controls for admin on an unbound repo', async () => {
  const auth = useAuthStore()
  auth.user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin', auth_source: 'sso' }

  const { getRepo } = await import('@/api/repo')
  ;(getRepo as any).mockResolvedValue({
    data: { data: { id: 9, name: 'repo-a', full_name: 'org/repo-a', clone_url: 'https://github.com/org/repo-a.git', default_branch: 'main', ai_score: 82, status: 'active', binding_state: 'unbound', created_at: '2026-01-01T00:00:00Z', edges: {} } },
  })

  const { wrapper } = await mountRepoDetail()
  expect(wrapper.text()).toContain('SCM Provider Binding')
  expect(wrapper.find('button:disabled').text()).toContain('Run Scan')
})
```

- [x] **Step 2: Run tests to verify they fail**

Run: `cd frontend && pnpm test -- --run src/__tests__/repo-list-view.test.ts src/__tests__/repo-detail-view.test.ts`
Expected:
- `shows an Unbound badge...`: FAIL because no badge is rendered
- `disables scan...`: FAIL because the detail page has no binding panel and scan remains enabled

- [x] **Step 3: Write minimal implementation**

```go
// backend/internal/repo/service.go
type UpdateRequest struct {
	Name               string            `json:"name"`
	SCMProviderID      *int              `json:"scm_provider_id"`
	ClearSCMProvider   bool              `json:"clear_scm_provider"`
	RelayProviderName  *string           `json:"relay_provider_name"`
	RelayGroupID       *string           `json:"relay_group_id"`
	ScanPromptOverride map[string]string `json:"scan_prompt_override,omitempty"`
	ClearScanPrompt    bool              `json:"clear_scan_prompt,omitempty"`
}

if req.ClearSCMProvider {
	update.ClearScmProvider()
} else if req.SCMProviderID != nil {
	update.SetScmProviderID(*req.SCMProviderID)
}

// backend/internal/handler/repo.go
type repoResponse struct {
	ID            int             `json:"id"`
	RepoKey       string          `json:"repo_key"`
	Name          string          `json:"name"`
	FullName      string          `json:"full_name"`
	CloneURL      string          `json:"clone_url"`
	DefaultBranch string          `json:"default_branch"`
	Status        string          `json:"status"`
	BindingState  string          `json:"binding_state"`
	SCMProviderID *int            `json:"scm_provider_id,omitempty"`
	Edges         map[string]any  `json:"edges,omitempty"`
}
```

```ts
// frontend/src/types/index.ts
export interface RepoConfig {
  id: number
  repo_key: string
  name: string
  full_name: string
  clone_url: string
  default_branch: string
  ai_score: number
  status: string
  binding_state: 'bound' | 'unbound'
  scm_provider_id?: number | null
  last_scan_at: string | null
  created_at: string
  edges?: {
    scm_provider?: SCMProvider
  }
}
```

```vue
<!-- frontend/src/views/repos/RepoListView.vue -->
<select v-model="bindingFilter" class="rounded-md border border-gray-300 px-3 py-2 text-sm">
  <option value="all">All</option>
  <option value="bound">Bound</option>
  <option value="unbound">Unbound</option>
</select>

<span
  v-if="repo.binding_state === 'unbound'"
  class="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800"
>
  Unbound
</span>
```

```vue
<!-- frontend/src/views/repos/RepoDetailView.vue -->
<button
  class="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
  :disabled="scanning || repo?.binding_state === 'unbound'"
  @click="handleScan"
>
  {{ scanning ? 'Scanning...' : 'Run Scan' }}
</button>

<section v-if="auth.isAdmin" class="rounded-lg bg-white p-4 shadow">
  <h2 class="text-sm font-semibold text-gray-900">SCM Provider Binding</h2>
  <select v-model="selectedProviderId" class="mt-3 w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
    <option :value="null">Unbound</option>
    <option v-for="provider in providers" :key="provider.id" :value="provider.id">
      {{ provider.name }}
    </option>
  </select>
  <div class="mt-3 flex gap-2">
    <button class="rounded-md bg-gray-900 px-3 py-1.5 text-sm font-medium text-white" @click="saveBinding">
      Save Binding
    </button>
    <button class="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700" @click="clearBinding">
      Clear Binding
    </button>
  </div>
  <p v-if="repo?.binding_state === 'unbound'" class="mt-2 text-sm text-amber-700">
    This repo was auto-discovered by `ae-cli start` and still needs an SCM provider binding.
  </p>
</section>
```

- [x] **Step 4: Run tests to verify they pass**

Run: `cd frontend && pnpm test -- --run src/__tests__/repo-list-view.test.ts src/__tests__/repo-detail-view.test.ts`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/internal/handler/repo.go backend/internal/repo/service.go frontend/src/types/index.ts frontend/src/api/repo.ts frontend/src/views/repos/RepoListView.vue frontend/src/views/repos/RepoDetailView.vue frontend/src/__tests__/repo-list-view.test.ts frontend/src/__tests__/repo-detail-view.test.ts
git commit -m "feat(frontend): surface unbound repo binding state"
```

### Task 5: Update Architecture Docs and Run End-to-End Verification

**Files:**
- Modify: `docs/architecture.md`

- [x] **Step 1: Update the architecture document**

```md
### Runtime Boundaries

- `ae-cli start` now treats repo discovery as part of session bootstrap. If the backend does not already know the repo, bootstrap auto-creates an unbound `repo_config` using the local Git remote and continues.
- Repo-to-`scm_provider` binding is now an admin-managed lifecycle step rather than a hard precondition for starting a session.
- SCM-dependent features such as scan, PR sync, optimize, and webhook registration require a bound repo and return `repo_unbound` when invoked before binding.
```

- [x] **Step 2: Run backend verification**

Run: `cd backend && go test ./internal/repo ./internal/sessionbootstrap ./internal/analysis ./internal/handler`
Expected: PASS

- [x] **Step 3: Run frontend verification**

Run: `cd frontend && pnpm test -- --run src/__tests__/repo-list-view.test.ts src/__tests__/repo-detail-view.test.ts`
Expected: PASS

- [x] **Step 4: Run ae-cli verification**

Run: `cd ae-cli && go test ./internal/session ./internal/client`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): reflect auto repo sync bootstrap flow"
```
