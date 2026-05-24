# Global Git Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the global and repo-local AE Git hook contract from `docs/superpowers/specs/2026-05-23-global-git-hooks-design.md`.

**Architecture:** Backend repo eligibility becomes read-only and explicit: hook paths resolve backend-known reporting-enabled repositories before any checkpoint, rewrite, or usage upload, and upload payloads carry `repo_config_id`. The CLI owns hook installation, eligibility cache, observed repo state, install registry, and attribution replay state under `~/.ae-cli/`, while managed shell scripts stay thin and resolve the current `ae-cli` binary at runtime. The active hook path derives workspace identity from Git context only and ignores historical `.ae` workspace metadata and old user-level state roots.

**Tech Stack:** Go 1.x, Cobra, Gin, Ent, local JSON state files, POSIX shell hook scripts, PowerShell installer, Git CLI.

---

## File Structure

Create or modify these files. Keep package boundaries focused; do not move unrelated code.

- Create: `backend/internal/repo/eligibility.go` - read-only repo resolve and batch eligibility service logic.
- Modify: `backend/internal/repo/service.go` - shared repo lookup helpers if needed by eligibility.
- Modify: `backend/internal/handler/repo.go` - request/response structs and handlers for `resolve-remote` and `hook-eligible`.
- Modify: `backend/internal/handler/router.go` - route registration for the new protected repo endpoints.
- Test: `backend/internal/handler/repo_eligibility_test.go`.
- Modify: `backend/internal/checkpoint/service.go` - `repo_config_id` support and no-create ingest path.
- Modify: `backend/internal/handler/checkpoint.go` - pass `repo_config_id` through handler requests.
- Test: `backend/internal/checkpoint/service_test.go`.
- Test: `backend/internal/handler/checkpoint_test.go`.
- Modify: `backend/internal/toolusage/service.go` - bind managed usage by `repo_config_id + authenticated user`.
- Modify: `backend/internal/handler/tool_usage.go` - accept `repo_config_id`.
- Test: `backend/internal/toolusage/service_test.go`.
- Test: `backend/internal/handler/tool_usage_test.go`.
- Modify: `ae-cli/internal/client/client.go` - add resolve, batch eligibility, `repo_config_id` request fields, and client timeout hooks.
- Test: `ae-cli/internal/client/client_test.go`.
- Create: `ae-cli/internal/repoidentity/identity.go` - CLI mirror of backend repo identity derivation.
- Test: `ae-cli/internal/repoidentity/identity_test.go`.
- Create: `ae-cli/internal/clistate/state.go` - `~/.ae-cli` root and atomic JSON helpers.
- Create: `ae-cli/internal/hookstate/context.go` - normalized server/account/repo context binding.
- Create: `ae-cli/internal/hookstate/eligibility.go` - `repos.json` cache.
- Create: `ae-cli/internal/hookstate/observed.go` - `observed-repos.json`.
- Create: `ae-cli/internal/hookstate/installations.go` - `installations.json`.
- Test: `ae-cli/internal/hookstate/*_test.go`.
- Modify: `ae-cli/internal/auth/token.go` - persist stable non-secret `auth_subject` in token file and derive from JWT claims.
- Test: `ae-cli/internal/auth/token_test.go`.
- Modify: `ae-cli/cmd/login.go` and `ae-cli/cmd/root.go` - store and preserve `auth_subject` during login and refresh.
- Test: `ae-cli/cmd/login_test.go` and `ae-cli/cmd/root_test.go`.
- Modify: `ae-cli/internal/attributionlocal/state.go` - move active attribution root to `~/.ae-cli/state/attribution`.
- Modify: `ae-cli/internal/attributionlocal/sync.go` and `ae-cli/internal/attributionlocal/types.go` - context-bound spool/replay, managed payload redaction, `repo_config_id`.
- Test: `ae-cli/internal/attributionlocal/sync_test.go`.
- Modify: `ae-cli/internal/collector/collector.go` - collector cache path follows new attribution root.
- Modify: `ae-cli/internal/hooks/queue.go` - context-bound hook queue and upload ledger support.
- Create: `ae-cli/internal/hooks/gitcontext.go` - Git root, branch, remote, git dir, common dir, default hook dir, and workspace ID derivation.
- Create: `ae-cli/internal/hooks/script.go` - managed hook script rendering and template header parsing.
- Create: `ae-cli/internal/hooks/config.go` - global/local/worktree `core.hooksPath` inspection and mutation.
- Modify: `ae-cli/internal/hooks/install.go` - replace legacy chaining installer with explicit global/repo-local enable, disable, status, refresh helpers.
- Test: `ae-cli/internal/hooks/install_test.go`, `ae-cli/internal/hooks/gitcontext_test.go`, `ae-cli/internal/hooks/script_test.go`, `ae-cli/internal/hooks/config_test.go`, `ae-cli/internal/hooks/queue_test.go`.
- Modify: `ae-cli/internal/hooks/handler.go` - accept resolved eligibility context, remove `.ae` marker reads/writes from active hook path, remove `EnsureRepoFromRemote`.
- Modify: `ae-cli/internal/hooks/uploader_backend.go` - forward `repo_config_id`.
- Test: `ae-cli/internal/hooks/handler_test.go` and `ae-cli/internal/hooks/uploader_backend_test.go`.
- Modify: `ae-cli/cmd/hook.go` - hidden dispatcher resolves eligibility before handler execution.
- Create: `ae-cli/cmd/hooks.go` - public `ae-cli hooks enable|disable|status|refresh` command surface.
- Modify: `ae-cli/cmd/init.go` - default to `--hooks none`, register repo via ensure only for explicit init.
- Modify: `ae-cli/cmd/sync.go` - resolve-only before sync and include `repo_config_id`.
- Modify: `ae-cli/cmd/doctor.go` - include hook status and cache diagnostics.
- Test: `ae-cli/cmd/hook_test.go`, `ae-cli/cmd/hooks_test.go`, `ae-cli/cmd/init_test.go`, `ae-cli/cmd/sync_test.go`, `ae-cli/cmd/doctor_test.go`.
- Modify: `ae-cli/install.sh` and `ae-cli/install.ps1` - run post-install hook template refresh with the installed binary.
- Test: `ae-cli/test/install-test.sh`.
- Modify after implementation: `docs/architecture.md` - current runtime architecture and state roots.
- Optional final doc update: `ae-cli/README.md` - command examples once behavior is implemented.

## Task 1: Backend Read-Only Repo Eligibility

**Files:**
- Create: `backend/internal/repo/eligibility.go`
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/router.go`
- Test: `backend/internal/handler/repo_eligibility_test.go`

- [x] **Step 1: Write failing HTTP tests for resolve eligibility**

Add `backend/internal/handler/repo_eligibility_test.go`:

```go
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

func createEligibilityRepo(t *testing.T, client *ent.Client, status repoconfig.Status, cloneURL string) int {
	t.Helper()
	return client.RepoConfig.Create().
		SetName("repo").
		SetFullName("org/repo").
		SetCloneURL(cloneURL).
		SetDefaultBranch("main").
		SetStatus(status).
		SaveX(context.Background()).ID
}

func TestResolveRemoteEligibleForActiveRepo(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusActive, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url":           "git@repo-host.example.com:org/repo.git",
		"branch":               "main",
		"client_cache_version": "repo-eligibility-v1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != true {
		t.Fatalf("eligible = %v, want true", data["eligible"])
	}
	if data["repo_key"] != "repo-host.example.com/org/repo" {
		t.Fatalf("repo_key = %v", data["repo_key"])
	}
	if int(data["repo_config_id"].(float64)) == 0 {
		t.Fatalf("repo_config_id missing: %v", data)
	}
}

func TestResolveRemoteDoesNotCreateUnknownRepo(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/missing.git",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != false || data["reason"] != "not_found" {
		t.Fatalf("unexpected data: %v", data)
	}
	if count := env.client.RepoConfig.Query().CountX(context.Background()); count != 0 {
		t.Fatalf("repo count = %d, want 0", count)
	}
}

func TestResolveRemoteInactiveIsIneligible(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusInactive, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/repo.git",
	})

	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != false || data["reason"] != "inactive" {
		t.Fatalf("unexpected data: %v", data)
	}
}

func TestResolveRemoteWebhookFailedIsEligible(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusWebhookFailed, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/repo.git",
	})

	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != true {
		t.Fatalf("unexpected data: %v", data)
	}
}
```

- [x] **Step 2: Run resolve tests and verify failure**

Run: `cd backend && go test ./internal/handler -run 'TestResolveRemote' -count=1`

Expected: FAIL with `404` or missing `/api/v1/repos/resolve-remote`.

- [x] **Step 3: Add read-only eligibility service**

Create `backend/internal/repo/eligibility.go`:

```go
package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

const EligibilityVersion = "repo-eligibility-v1"

type ResolveRemoteRequest struct {
	RemoteURL          string `json:"remote_url" binding:"required"`
	Branch             string `json:"branch"`
	ClientCacheVersion string `json:"client_cache_version"`
}

type HookEligibleRepoRequest struct {
	RepoKey   string `json:"repo_key"`
	RemoteURL string `json:"remote_url"`
}

type EligibilityResult struct {
	Eligible      bool   `json:"eligible"`
	RepoConfigID  int    `json:"repo_config_id,omitempty"`
	RepoKey       string `json:"repo_key,omitempty"`
	FullName      string `json:"full_name,omitempty"`
	CloneURL      string `json:"clone_url,omitempty"`
	Status        string `json:"status,omitempty"`
	BindingState  string `json:"binding_state,omitempty"`
	SCMProviderID *int   `json:"scm_provider_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (s *Service) ResolveRemoteEligibility(ctx context.Context, req ResolveRemoteRequest) (*EligibilityResult, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("resolve repo eligibility: service is not initialized")
	}
	remoteURL := strings.TrimSpace(req.RemoteURL)
	if remoteURL == "" {
		return &EligibilityResult{Eligible: false, Reason: "invalid_remote"}, nil
	}

	identity, err := DeriveRepoIdentity(remoteURL)
	if err != nil {
		identity = FallbackRepoIdentity(remoteURL, remoteURL)
	}
	result := &EligibilityResult{
		Eligible: false,
		RepoKey:  identity.RepoKey,
	}

	rc, err := s.findExistingRepoByIdentity(ctx, identity, remoteURL)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		result.Reason = "not_found"
		return result, nil
	}
	return s.eligibilityForRepo(rc), nil
}

func (s *Service) BatchHookEligibility(ctx context.Context, repos []HookEligibleRepoRequest) ([]EligibilityResult, []EligibilityResult, error) {
	eligible := make([]EligibilityResult, 0, len(repos))
	ineligible := make([]EligibilityResult, 0, len(repos))
	for _, item := range repos {
		result, err := s.ResolveRemoteEligibility(ctx, ResolveRemoteRequest{RemoteURL: item.RemoteURL})
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(result.RepoKey) == "" {
			result.RepoKey = strings.TrimSpace(item.RepoKey)
		}
		if result.Eligible {
			eligible = append(eligible, *result)
		} else {
			ineligible = append(ineligible, *result)
		}
	}
	return eligible, ineligible, nil
}

func (s *Service) eligibilityForRepo(rc *ent.RepoConfig) *EligibilityResult {
	result := &EligibilityResult{
		RepoConfigID: rc.ID,
		RepoKey:      rc.RepoKey,
		FullName:     rc.FullName,
		CloneURL:     rc.CloneURL,
		Status:       string(rc.Status),
		BindingState: "unbound",
	}
	if rc.Edges.ScmProvider != nil {
		result.BindingState = "bound"
		id := rc.Edges.ScmProvider.ID
		result.SCMProviderID = &id
	}
	switch rc.Status {
	case repoconfig.StatusActive, repoconfig.StatusWebhookFailed:
		result.Eligible = true
	default:
		result.Eligible = false
		result.Reason = "inactive"
	}
	return result
}
```

- [x] **Step 4: Wire HTTP handlers and routes**

In `backend/internal/handler/repo.go`, add:

```go
type hookEligibleRequest struct {
	Repos []repo.HookEligibleRepoRequest `json:"repos" binding:"required"`
}

func (h *RepoHandler) ResolveRemote(c *gin.Context) {
	var req repo.ResolveRemoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.repoService.ResolveRemoteEligibility(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *RepoHandler) HookEligible(c *gin.Context) {
	var req hookEligibleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	eligible, ineligible, err := h.repoService.BatchHookEligibility(c.Request.Context(), req.Repos)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{
		"repos":      eligible,
		"ineligible": ineligible,
		"version":    repo.EligibilityVersion,
	})
}
```

In `backend/internal/handler/router.go`, under `repoGroup` and before `/:id` routes:

```go
repoGroup.POST("/resolve-remote", repoHandler.ResolveRemote)
repoGroup.POST("/hook-eligible", repoHandler.HookEligible)
```

- [x] **Step 5: Run resolve tests and verify pass**

Run: `cd backend && go test ./internal/handler -run 'TestResolveRemote' -count=1`

Expected: PASS.

- [x] **Step 6: Write failing batch eligibility tests**

Append to `backend/internal/handler/repo_eligibility_test.go`:

```go
func TestHookEligibleReturnsOnlyRequestedRepos(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusActive, "https://repo-host.example.com/org/repo.git")
	env.client.RepoConfig.Create().
		SetName("other").
		SetFullName("org/other").
		SetCloneURL("https://repo-host.example.com/org/other.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/hook-eligible", map[string]any{
		"repos": []map[string]any{
			{"repo_key": "repo-host.example.com/org/repo", "remote_url": "https://repo-host.example.com/org/repo.git"},
			{"repo_key": "repo-host.example.com/org/missing", "remote_url": "https://repo-host.example.com/org/missing.git"},
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	repos := data["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("eligible repos len = %d, want 1; data=%v", len(repos), data)
	}
	if repos[0].(map[string]any)["repo_key"] != "repo-host.example.com/org/repo" {
		t.Fatalf("unexpected repo: %v", repos[0])
	}
	ineligible := data["ineligible"].([]any)
	if len(ineligible) != 1 || ineligible[0].(map[string]any)["reason"] != "not_found" {
		t.Fatalf("unexpected ineligible: %v", ineligible)
	}
	if data["version"] != "repo-eligibility-v1" {
		t.Fatalf("version = %v", data["version"])
	}
}
```

- [x] **Step 7: Run batch eligibility tests**

Run: `cd backend && go test ./internal/handler -run 'TestHookEligible|TestResolveRemote' -count=1`

Expected: PASS.

- [x] **Step 8: Commit backend eligibility slice**

```bash
git add backend/internal/repo/eligibility.go backend/internal/handler/repo.go backend/internal/handler/router.go backend/internal/handler/repo_eligibility_test.go
git -c core.hooksPath=/dev/null commit -m "feat(backend): add hook repo eligibility endpoints"
```

## Task 2: Backend Ingest by `repo_config_id`

**Files:**
- Modify: `backend/internal/checkpoint/service.go`
- Modify: `backend/internal/handler/checkpoint.go`
- Modify: `backend/internal/toolusage/service.go`
- Modify: `backend/internal/handler/tool_usage.go`
- Test: `backend/internal/checkpoint/service_test.go`
- Test: `backend/internal/handler/checkpoint_test.go`
- Test: `backend/internal/toolusage/service_test.go`
- Test: `backend/internal/handler/tool_usage_test.go`

- [x] **Step 1: Write failing checkpoint service tests**

Append to `backend/internal/checkpoint/service_test.go`:

```go
func TestRecordCheckpointWithRepoConfigIDDoesNotAutoCreateOnRemoteMiss(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)

	err := NewService(client).RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:        "cp-by-id",
		RepoConfigID:   repo.ID,
		RepoFullName:   "https://repo-host.example.com/unknown/repo.git",
		WorkspaceID:    "ws-by-id",
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		BindingSource:  "unbound",
	})
	if err != nil {
		t.Fatalf("RecordCheckpointForUser: %v", err)
	}
	if count := client.RepoConfig.Query().CountX(ctx); count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
	row := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ("cp-by-id")).OnlyX(ctx)
	if row.RepoConfigID != repo.ID {
		t.Fatalf("repo_config_id = %d, want %d", row.RepoConfigID, repo.ID)
	}
}

func TestRecordCheckpointWithInactiveRepoConfigIDRejected(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)
	client.RepoConfig.UpdateOneID(repo.ID).SetStatus("inactive").ExecX(ctx)

	err := NewService(client).RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:       "cp-inactive",
		RepoConfigID:  repo.ID,
		WorkspaceID:   "ws-inactive",
		CommitSHA:     "abc123",
		BindingSource: "unbound",
	})
	if err == nil {
		t.Fatalf("expected inactive repo rejection")
	}
	if count := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ("cp-inactive")).CountX(ctx); count != 0 {
		t.Fatalf("checkpoint count = %d, want 0", count)
	}
}

func TestRecordRewriteWithRepoConfigIDDoesNotAutoCreate(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)

	err := NewService(client).RecordRewriteForUser(ctx, userID, CommitRewriteRequest{
		EventID:       "rw-by-id",
		RepoConfigID:  repo.ID,
		RepoFullName:  "https://repo-host.example.com/unknown/repo.git",
		WorkspaceID:   "ws-rw",
		RewriteType:   "amend",
		OldCommitSHA:  "old",
		NewCommitSHA:  "new",
		BindingSource: "unbound",
	})
	if err != nil {
		t.Fatalf("RecordRewriteForUser: %v", err)
	}
	if count := client.RepoConfig.Query().CountX(ctx); count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
}
```

- [x] **Step 2: Run checkpoint tests and verify failure**

Run: `cd backend && go test ./internal/checkpoint -run 'RepoConfigID|AutoCreatesRepo' -count=1`

Expected: FAIL because `RepoConfigID` is not in request structs.

- [x] **Step 3: Implement checkpoint/rewrite `repo_config_id` path**

In `backend/internal/checkpoint/service.go`, add fields:

```go
RepoConfigID int `json:"repo_config_id,omitempty"`
```

to both `CommitCheckpointRequest` and `CommitRewriteRequest`.

Add helper:

```go
func (s *Service) resolveRepoConfigForIngest(ctx context.Context, repoConfigID int, repoFullName, cloneURL, branch string) (*ent.RepoConfig, error) {
	if repoConfigID > 0 {
		rc, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.IDEQ(repoConfigID)).
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("repo_config_id %d not found: %w", repoConfigID, err)
		}
		switch rc.Status {
		case repoconfig.StatusActive, repoconfig.StatusWebhookFailed:
			return rc, nil
		default:
			return nil, fmt.Errorf("repo_config_id %d is not reporting-enabled", repoConfigID)
		}
	}
	return s.resolveOrEnsureRepoConfig(ctx, repoFullName, cloneURL, branch)
}
```

Replace calls:

```go
rc, err := txSvc.resolveOrEnsureRepoConfig(ctx, req.RepoFullName, req.CloneURL, req.BranchSnapshot)
```

with:

```go
rc, err := txSvc.resolveRepoConfigForIngest(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL, req.BranchSnapshot)
```

Replace rewrite's resolve call the same way:

```go
rc, err := s.resolveRepoConfigForIngest(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL, "")
```

- [x] **Step 4: Run checkpoint tests**

Run: `cd backend && go test ./internal/checkpoint -run 'RepoConfigID|AutoCreatesRepo|RecordCheckpointDedupes|RecordRewrite' -count=1`

Expected: PASS. The existing `TestRecordCheckpointForUser_AutoCreatesRepoOnRemoteMiss` must still pass when `repo_config_id` is omitted.

- [x] **Step 5: Write failing tool usage tests**

Append to `backend/internal/toolusage/service_test.go`:

```go
func TestCreateUsageEventWithRepoConfigIDDoesNotNeedCheckpointScope(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	ctx := context.Background()
	defer client.Close()

	repo := client.RepoConfig.Create().
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://repo-host.example.com/org/demo.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SaveX(ctx)
	userID := client.User.Create().
		SetUsername("tool-user").
		SetEmail("tool-user@example.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	err := NewService(client).CreateUsageEvent(ctx, userID, CreateUsageEventRequest{
		RepoConfigID:     repo.ID,
		Tool:             "codex",
		WorkspaceID:      "ws-direct",
		ToolSessionID:    "codex-sess",
		DedupeKey:        "codex:direct",
		UsageUnit:        "token",
		ObservedStartAt:  time.Unix(100, 0).UTC(),
		ObservedEndAt:    time.Unix(101, 0).UTC(),
		InputTokens:      10,
		OutputTokens:     5,
		RawSourcePath:    "/tmp/should-stay-supported-for-legacy",
		RawSourceLocator: "line:1",
		RawPayload:       map[string]any{"legacy": true},
	})
	if err != nil {
		t.Fatalf("CreateUsageEvent: %v", err)
	}
	row := client.ToolUsageEvent.Query().Where(toolusageevent.DedupeKeyEQ("codex:direct")).OnlyX(ctx)
	if row.RepoConfigID != repo.ID || row.UserID != userID {
		t.Fatalf("scope = repo %d user %d, want repo %d user %d", row.RepoConfigID, row.UserID, repo.ID, userID)
	}
}

func TestCreateUsageEventWithRepoConfigIDIgnoresConflictingWorkspaceScope(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	ctx := context.Background()
	defer client.Close()

	scopeA := seedToolUsageScope(t, client)
	scopeB := seedToolUsageScope(t, client)
	userID := scopeB.UserID

	err := NewService(client).CreateUsageEvent(ctx, userID, CreateUsageEventRequest{
		RepoConfigID:    scopeB.RepoConfigID,
		Tool:            "claude",
		WorkspaceID:     scopeA.WorkspaceID,
		ToolSessionID:   "claude-sess",
		DedupeKey:       "claude:override",
		UsageUnit:       "token",
		ObservedStartAt: time.Unix(200, 0).UTC(),
		ObservedEndAt:   time.Unix(201, 0).UTC(),
		InputTokens:     8,
		OutputTokens:    3,
	})
	if err != nil {
		t.Fatalf("CreateUsageEvent: %v", err)
	}
	row := client.ToolUsageEvent.Query().Where(toolusageevent.DedupeKeyEQ("claude:override")).OnlyX(ctx)
	if row.RepoConfigID != scopeB.RepoConfigID || row.UserID != scopeB.UserID {
		t.Fatalf("scope = repo %d user %d, want repo %d user %d", row.RepoConfigID, row.UserID, scopeB.RepoConfigID, scopeB.UserID)
	}
}
```

- [x] **Step 6: Implement tool usage `repo_config_id` binding**

In `backend/internal/toolusage/service.go`, add to `CreateUsageEventRequest`:

```go
RepoConfigID int
```

In `CreateUsageEvent`, replace unconditional `resolveScopeByWorkspace` with:

```go
scope, err := s.resolveScope(ctx, userID, req.RepoConfigID, workspaceID)
if err != nil {
	return err
}
```

Add:

```go
func (s *Service) resolveScope(ctx context.Context, userID, repoConfigID int, workspaceID string) (*scopeResolution, error) {
	if repoConfigID > 0 {
		exists, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.IDEQ(repoConfigID)).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("create tool usage event: query repo_config_id: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("create tool usage event: repo_config_id %d not found", repoConfigID)
		}
		return &scopeResolution{RepoConfigID: repoConfigID, UserID: userID}, nil
	}
	return s.resolveScopeByWorkspace(ctx, workspaceID)
}
```

Add import:

```go
"github.com/ai-efficiency/backend/ent/repoconfig"
```

- [x] **Step 7: Wire handler request fields**

In `backend/internal/handler/checkpoint.go`, make sure the handler-bound request structs include `repo_config_id` through the service request structs. If the handler imports service request structs directly, no extra code is needed after Task 2 Step 3.

In `backend/internal/handler/tool_usage.go`, add:

```go
RepoConfigID int `json:"repo_config_id"`
```

and pass:

```go
RepoConfigID: req.RepoConfigID,
```

- [x] **Step 8: Run backend ingest tests**

Run:

```bash
cd backend
go test ./internal/checkpoint ./internal/toolusage ./internal/handler -run 'RepoConfigID|CreateUsageEvent|Checkpoint|Rewrite|ToolUsage' -count=1
```

Expected: PASS.

- [x] **Step 9: Commit backend ingest slice**

```bash
git add backend/internal/checkpoint/service.go backend/internal/handler/checkpoint.go backend/internal/toolusage/service.go backend/internal/handler/tool_usage.go backend/internal/checkpoint/service_test.go backend/internal/handler/checkpoint_test.go backend/internal/toolusage/service_test.go backend/internal/handler/tool_usage_test.go
git -c core.hooksPath=/dev/null commit -m "feat(backend): bind hook ingest by repo config"
```

## Task 3: CLI Client, Identity, and Auth Context

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Test: `ae-cli/internal/client/client_test.go`
- Create: `ae-cli/internal/repoidentity/identity.go`
- Test: `ae-cli/internal/repoidentity/identity_test.go`
- Modify: `ae-cli/internal/auth/token.go`
- Test: `ae-cli/internal/auth/token_test.go`
- Modify: `ae-cli/cmd/login.go`
- Modify: `ae-cli/cmd/root.go`
- Test: `ae-cli/cmd/login_test.go`
- Test: `ae-cli/cmd/root_test.go`

- [x] **Step 1: Write failing client tests for new API and `repo_config_id` payloads**

Append to `ae-cli/internal/client/client_test.go`:

```go
func TestResolveRepoFromRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req["remote_url"] != "git@repo-host.example.com:org/repo.git" || req["client_cache_version"] != "repo-eligibility-v1" {
			t.Fatalf("request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"eligible":       true,
				"repo_config_id": 123,
				"repo_key":       "repo-host.example.com/org/repo",
				"full_name":      "org/repo",
				"clone_url":      "git@repo-host.example.com:org/repo.git",
				"status":         "active",
				"binding_state":  "unbound",
			},
		})
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ResolveRepoFromRemote(context.Background(), ResolveRepoRequest{
		RemoteURL:          "git@repo-host.example.com:org/repo.git",
		ClientCacheVersion: "repo-eligibility-v1",
	})
	if err != nil {
		t.Fatalf("ResolveRepoFromRemote: %v", err)
	}
	if !resp.Eligible || resp.RepoConfigID != 123 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestBatchHookEligible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/hook-eligible" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"version": "repo-eligibility-v1",
				"repos": []map[string]any{{
					"eligible":       true,
					"repo_config_id": 123,
					"repo_key":       "repo-host.example.com/org/repo",
				}},
				"ineligible": []map[string]any{{
					"eligible": false,
					"repo_key": "repo-host.example.com/org/missing",
					"reason":   "not_found",
				}},
			},
		})
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").BatchHookEligible(context.Background(), []HookEligibleRepoRequest{
		{RepoKey: "repo-host.example.com/org/repo", RemoteURL: "https://repo-host.example.com/org/repo.git"},
	})
	if err != nil {
		t.Fatalf("BatchHookEligible: %v", err)
	}
	if resp.Version != "repo-eligibility-v1" || len(resp.Repos) != 1 || len(resp.Ineligible) != 1 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestManagedPayloadsIncludeRepoConfigID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload["repo_config_id"].(float64) != 123 {
			t.Fatalf("payload missing repo_config_id: %#v", payload)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.SendCommitCheckpoint(context.Background(), CommitCheckpointRequest{
		EventID:      "cp",
		RepoConfigID: 123,
		WorkspaceID:  "ws",
		CommitSHA:    "abc",
		BindingSource: "unbound",
	}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}
```

- [x] **Step 2: Implement client structs and methods**

In `ae-cli/internal/client/client.go`, add fields:

```go
RepoConfigID int `json:"repo_config_id,omitempty"`
```

to `CommitCheckpointRequest`, `CommitRewriteRequest`, and `ToolUsageEventRequest`.

Add:

```go
const RepoEligibilityVersion = "repo-eligibility-v1"

type ResolveRepoRequest struct {
	RemoteURL          string `json:"remote_url"`
	Branch             string `json:"branch,omitempty"`
	ClientCacheVersion string `json:"client_cache_version,omitempty"`
}

type RepoEligibilityResponse struct {
	Eligible      bool   `json:"eligible"`
	RepoConfigID  int    `json:"repo_config_id,omitempty"`
	RepoKey       string `json:"repo_key,omitempty"`
	FullName      string `json:"full_name,omitempty"`
	CloneURL      string `json:"clone_url,omitempty"`
	Status        string `json:"status,omitempty"`
	BindingState  string `json:"binding_state,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type HookEligibleRepoRequest struct {
	RepoKey   string `json:"repo_key"`
	RemoteURL string `json:"remote_url"`
}

type BatchHookEligibleResponse struct {
	Repos      []RepoEligibilityResponse `json:"repos"`
	Ineligible []RepoEligibilityResponse `json:"ineligible"`
	Version    string                    `json:"version"`
}
```

Add methods:

```go
func (c *Client) ResolveRepoFromRemote(ctx context.Context, req ResolveRepoRequest) (*RepoEligibilityResponse, error) {
	if req.ClientCacheVersion == "" {
		req.ClientCacheVersion = RepoEligibilityVersion
	}
	var envelope struct {
		Data RepoEligibilityResponse `json:"data"`
	}
	if err := c.postJSON(ctx, "/api/v1/repos/resolve-remote", req, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *Client) BatchHookEligible(ctx context.Context, repos []HookEligibleRepoRequest) (*BatchHookEligibleResponse, error) {
	var envelope struct {
		Data BatchHookEligibleResponse `json:"data"`
	}
	if err := c.postJSON(ctx, "/api/v1/repos/hook-eligible", map[string]any{"repos": repos}, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}
```

Extract the repeated POST code into:

```go
func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
```

- [x] **Step 3: Add CLI repo identity helper and tests**

Create `ae-cli/internal/repoidentity/identity_test.go`:

```go
package repoidentity

import "testing"

func TestDeriveRepoIdentityCommonRemoteForms(t *testing.T) {
	for _, remote := range []string{
		"git@repo-host.example.com:org/repo.git",
		"ssh://git@repo-host.example.com/org/repo.git",
		"https://repo-host.example.com/org/repo.git",
	} {
		id, err := Derive(remote)
		if err != nil {
			t.Fatalf("Derive(%q): %v", remote, err)
		}
		if id.RepoKey != "repo-host.example.com/org/repo" {
			t.Fatalf("RepoKey for %q = %q", remote, id.RepoKey)
		}
		if id.FullName != "org/repo" {
			t.Fatalf("FullName for %q = %q", remote, id.FullName)
		}
	}
}

func TestDeriveRepoIdentityBitbucketServerForms(t *testing.T) {
	for _, remote := range []string{
		"https://repo-host.example.com/scm/PROJ/repo.git",
		"https://repo-host.example.com/projects/PROJ/repos/repo",
	} {
		id, err := Derive(remote)
		if err != nil {
			t.Fatalf("Derive(%q): %v", remote, err)
		}
		if id.RepoKey != "repo-host.example.com/proj/repo" {
			t.Fatalf("RepoKey for %q = %q", remote, id.RepoKey)
		}
		if id.FullName != "PROJ/repo" {
			t.Fatalf("FullName for %q = %q", remote, id.FullName)
		}
	}
}
```

Create `ae-cli/internal/repoidentity/identity.go` by mirroring `backend/internal/repoidentity/identity.go` with package name `repoidentity` and exported function:

```go
func Derive(remoteURL string) (Identity, error)
```

Use the same normalization rules and test cases as backend. Do not import backend code into `ae-cli`.

- [x] **Step 4: Add stable auth subject support**

In `ae-cli/internal/auth/token.go`, extend `TokenFile`:

```go
AuthSubject string `json:"auth_subject,omitempty"`
```

Add:

```go
func SubjectFromAccessToken(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	switch v := claims["user_id"].(type) {
	case float64:
		if v > 0 {
			return fmt.Sprintf("user:%d", int(v))
		}
	case string:
		v = strings.TrimSpace(v)
		if v != "" {
			return "user:" + v
		}
	}
	return ""
}

func (t *TokenFile) StableAuthSubject() string {
	if t == nil {
		return ""
	}
	if s := strings.TrimSpace(t.AuthSubject); s != "" {
		return s
	}
	return SubjectFromAccessToken(t.AccessToken)
}
```

Update imports with `encoding/base64` and `strings`.

- [x] **Step 5: Write and pass auth subject tests**

Add to `ae-cli/internal/auth/token_test.go`:

```go
func TestSubjectFromAccessTokenUserID(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":123,"type":"access"}`))
	token := "header." + payload + ".signature"
	if got := SubjectFromAccessToken(token); got != "user:123" {
		t.Fatalf("subject = %q", got)
	}
}

func TestStableAuthSubjectPrefersPersistedSubject(t *testing.T) {
	tf := &TokenFile{AccessToken: "bad", AuthSubject: "user:456"}
	if got := tf.StableAuthSubject(); got != "user:456" {
		t.Fatalf("subject = %q", got)
	}
}
```

Run: `cd ae-cli && go test ./internal/auth -run 'Subject|Token' -count=1`

Expected: PASS.

- [x] **Step 6: Persist subject during login and refresh**

In `ae-cli/cmd/login.go`, when constructing `auth.TokenFile`, set:

```go
AuthSubject: auth.SubjectFromAccessToken(result.AccessToken),
```

In `ae-cli/cmd/root.go`, when writing refreshed token, preserve stable subject:

```go
AuthSubject: firstNonEmpty(auth.SubjectFromAccessToken(refreshed.AccessToken), tf.StableAuthSubject()),
```

Add helper in `root.go`:

```go
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
```

- [x] **Step 7: Run CLI client and auth tests**

Run:

```bash
cd ae-cli
go test ./internal/client ./internal/repoidentity ./internal/auth ./cmd -run 'ResolveRepo|BatchHook|RepoConfigID|DeriveRepoIdentity|Subject|Login|Refresh' -count=1
```

Expected: PASS.

- [x] **Step 8: Commit client and context slice**

```bash
git add ae-cli/internal/client/client.go ae-cli/internal/client/client_test.go ae-cli/internal/repoidentity ae-cli/internal/auth/token.go ae-cli/internal/auth/token_test.go ae-cli/cmd/login.go ae-cli/cmd/root.go ae-cli/cmd/login_test.go ae-cli/cmd/root_test.go
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): add hook repo identity and auth context"
```

## Task 4: Local State Root, Cache, Observed Repos, Registry, and Ledger

Status: Completed in this branch. Implementation intentionally did not add a test that creates `~/.ai-efficiency`; that older step conflicted with this task's active contract that tests must not read or write the old state root. Coverage now asserts the active `~/.ae-cli/state/attribution` root and verifies the Task 4 implementation slice has no old-root references.

**Files:**
- Create: `ae-cli/internal/clistate/state.go`
- Create: `ae-cli/internal/hookstate/context.go`
- Create: `ae-cli/internal/hookstate/eligibility.go`
- Create: `ae-cli/internal/hookstate/observed.go`
- Create: `ae-cli/internal/hookstate/installations.go`
- Test: `ae-cli/internal/hookstate/context_test.go`
- Test: `ae-cli/internal/hookstate/eligibility_test.go`
- Test: `ae-cli/internal/hookstate/observed_test.go`
- Test: `ae-cli/internal/hookstate/installations_test.go`
- Modify: `ae-cli/internal/attributionlocal/state.go`
- Modify: `ae-cli/internal/hooks/queue.go`
- Test: `ae-cli/internal/attributionlocal/sync_test.go`
- Test: `ae-cli/internal/hooks/queue_test.go`

- [x] **Step 1: Add CLI state root helper**

Create `ae-cli/internal/clistate/state.go`:

```go
package clistate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func RootDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ae-cli")
}

func StateDir() string {
	return filepath.Join(RootDir(), "state")
}

func HooksStateDir() string {
	return filepath.Join(StateDir(), "hooks")
}

func AttributionStateDir() string {
	return filepath.Join(StateDir(), "attribution")
}

func SaveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename json: %w", err)
	}
	return nil
}

func LoadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}
```

- [x] **Step 2: Move attribution root without fallback**

In `ae-cli/internal/attributionlocal/state.go`, replace implementation with:

```go
package attributionlocal

import "github.com/ai-efficiency/ae-cli/internal/clistate"

func AttributionRootDir() string {
	return clistate.AttributionStateDir()
}

var SaveJSON = clistate.SaveJSON
var LoadJSON = clistate.LoadJSON
```

Do not read or migrate `~/.ai-efficiency`. Remove `migrateLegacyWorkspaceSpool` calls in `sync.go` and delete `legacyGlobalSpoolPath` / `migrateLegacyWorkspaceSpool` functions.

- [x] **Step 3: Update tests that asserted old state root**

In `ae-cli/internal/attributionlocal/sync_test.go`, update paths to expect:

```go
filepath.Join(tmpHome, ".ae-cli", "state", "attribution", ...)
```

Remove expectations that legacy `~/.ai-efficiency/attribution/spool.json` is migrated. Replace the legacy migration test with:

```go
func TestRunForWorkspaceDoesNotReadOldAiEfficiencyRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldRoot := filepath.Join(home, ".ai-efficiency", "attribution", "spool.json")
	if err := os.MkdirAll(filepath.Dir(oldRoot), 0o700); err != nil {
		t.Fatalf("mkdir old root: %v", err)
	}
	if err := os.WriteFile(oldRoot, []byte(`[{"dedupe_key":"old"}]`), 0o600); err != nil {
		t.Fatalf("write old root: %v", err)
	}
	engine := NewSyncEngine(nil)
	if err := engine.RunForWorkspace(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("expected non-git workspace error before any old-root migration")
	}
	if _, err := os.Stat(oldRoot); err != nil {
		t.Fatalf("old root should be untouched: %v", err)
	}
}
```

- [x] **Step 4: Add context binding types and tests**

Create `ae-cli/internal/hookstate/context.go`:

```go
package hookstate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

type Context struct {
	ServerURL    string `json:"server_url,omitempty"`
	AuthSubject string `json:"auth_subject,omitempty"`
	RepoKey     string `json:"repo_key,omitempty"`
}

func NormalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func (c Context) Normalized() Context {
	return Context{
		ServerURL:    NormalizeServerURL(c.ServerURL),
		AuthSubject: strings.TrimSpace(c.AuthSubject),
		RepoKey:     strings.TrimSpace(c.RepoKey),
	}
}

func (c Context) Stable() bool {
	n := c.Normalized()
	return n.ServerURL != "" && n.AuthSubject != "" && n.RepoKey != ""
}

func (c Context) CacheKey() string {
	n := c.Normalized()
	sum := sha256.Sum256([]byte(n.ServerURL + "\x1f" + n.AuthSubject + "\x1f" + n.RepoKey))
	return hex.EncodeToString(sum[:])
}
```

Create `ae-cli/internal/hookstate/context_test.go`:

```go
package hookstate

import "testing"

func TestContextCacheKeyIncludesServerSubjectAndRepo(t *testing.T) {
	a := Context{ServerURL: "https://AE.example.com/", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/repo"}
	b := Context{ServerURL: "https://ae.example.com", AuthSubject: "user:2", RepoKey: "repo-host.example.com/org/repo"}
	if !a.Stable() {
		t.Fatalf("context should be stable")
	}
	if a.CacheKey() == b.CacheKey() {
		t.Fatalf("cache key must change by auth subject")
	}
}
```

- [x] **Step 5: Add eligibility cache**

Create `ae-cli/internal/hookstate/eligibility.go` with records:

```go
type EligibilityRecord struct {
	Eligible       bool      `json:"eligible"`
	ServerURL      string    `json:"server_url"`
	AuthSubject   string    `json:"auth_subject"`
	RepoConfigID  int       `json:"repo_config_id,omitempty"`
	RepoKey       string    `json:"repo_key"`
	FullName      string    `json:"full_name,omitempty"`
	CloneURL      string    `json:"clone_url,omitempty"`
	Status        string    `json:"status,omitempty"`
	BindingState  string    `json:"binding_state,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	LastResolvedAt time.Time `json:"last_resolved_at,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type EligibilityCache struct {
	Version            int                          `json:"version"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	ETag               string                       `json:"etag,omitempty"`
	EligibilityVersion string                       `json:"eligibility_version"`
	Repos              map[string]EligibilityRecord `json:"repos"`
	Negative           map[string]EligibilityRecord `json:"negative"`
}
```

Implement:

```go
func EligibilityPath() string
func LoadEligibilityCache() (*EligibilityCache, error)
func (c *EligibilityCache) PutPositive(ctx Context, resp client.RepoEligibilityResponse, now time.Time)
func (c *EligibilityCache) PutNegative(ctx Context, remoteURL, reason string, now time.Time)
func (c *EligibilityCache) Lookup(ctx Context, now time.Time, hasUsableCredential bool) (EligibilityRecord, bool)
func (c *EligibilityCache) Save() error
```

Rules in code:

- positive TTL: `24 * time.Hour`
- negative TTL: `5 * time.Minute`
- `Lookup` rejects entries when `hasUsableCredential == false`
- `Lookup` rejects positive entries with `RepoConfigID == 0`
- expired entries are misses
- mismatched `server_url`, `auth_subject`, or `repo_key` are misses because the key differs and record fields are rechecked

- [x] **Step 6: Add observed repo and installation registry**

Create `ae-cli/internal/hookstate/observed.go` with:

```go
type ObservedRepoRecord struct {
	RepoKey         string    `json:"repo_key"`
	ServerURL       string    `json:"server_url,omitempty"`
	AuthSubject    string    `json:"auth_subject,omitempty"`
	RemoteURL      string    `json:"remote_url"`
	FirstObservedAt time.Time `json:"first_observed_at"`
	LastObservedAt  time.Time `json:"last_observed_at"`
}
```

Implement:

```go
func ObservedPath() string
func LoadObservedRepos() (*ObservedRepos, error)
func (o *ObservedRepos) Observe(ctx Context, remoteURL string, now time.Time)
func (o *ObservedRepos) Matching(ctx Context) []ObservedRepoRecord
```

Use `ctx.CacheKey()` when stable, else key `unbound:<repo_key>`.

Create `ae-cli/internal/hookstate/installations.go` with:

```go
const CurrentHookTemplateVersion = 2

type InstallationRecord struct {
	Mode           string    `json:"mode"`
	RepoKey        string    `json:"repo_key,omitempty"`
	GitDir         string    `json:"git_dir,omitempty"`
	GitCommonDir   string    `json:"git_common_dir,omitempty"`
	ConfigScope    string    `json:"config_scope,omitempty"`
	HooksPath      string    `json:"hooks_path"`
	Enabled        bool      `json:"enabled"`
	TemplateVersion int      `json:"template_version"`
	UpdatedAt      time.Time `json:"updated_at"`
}
```

Implement upsert/disable matching rules exactly:

- global identity: `mode`
- worktree repo identity: `mode + git_dir + config_scope + hooks_path`
- local repo identity: `mode + git_common_dir + config_scope + hooks_path`

- [x] **Step 7: Add upload ledger skeleton**

In `ae-cli/internal/hooks/queue.go`, add:

```go
type Binding struct {
	ServerURL    string `json:"server_url,omitempty"`
	AuthSubject string `json:"auth_subject,omitempty"`
	RepoConfigID int   `json:"repo_config_id,omitempty"`
	RepoKey      string `json:"repo_key,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
}

type LedgerRecord struct {
	Version      int       `json:"version"`
	Kind         string    `json:"kind"`
	DedupeKey    string    `json:"dedupe_key"`
	ServerURL    string    `json:"server_url"`
	AuthSubject string    `json:"auth_subject"`
	RepoConfigID int      `json:"repo_config_id"`
	RepoKey      string   `json:"repo_key"`
	WorkspaceID  string   `json:"workspace_id"`
	Status       string    `json:"status"`
	AttemptCount int      `json:"attempt_count"`
	AttemptedAt  time.Time `json:"attempted_at"`
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}
```

Add:

```go
func LedgerPath(workspaceID string) (string, error)
func AppendLedger(workspaceID string, rec LedgerRecord) error
func ReadLedger(workspaceID string) ([]LedgerRecord, error)
```

Do not include raw payloads or local file paths in ledger structs.

- [x] **Step 8: Add and pass state package tests**

Tests must cover:

- eligibility positive hit, negative hit, expiry, context mismatch, missing credential, and zero `repo_config_id`
- observed stable and unbound records
- installation registry identity for global, local, and worktree
- attribution root equals `~/.ae-cli/state/attribution`
- no test reads or writes `~/.ai-efficiency`

Run:

```bash
cd ae-cli
go test ./internal/clistate ./internal/hookstate ./internal/attributionlocal ./internal/hooks -run 'Context|Eligibility|Observed|Installation|AttributionRoot|Ledger|Queue' -count=1
```

Expected: PASS.

- [x] **Step 9: Commit state slice**

```bash
git add ae-cli/internal/clistate ae-cli/internal/hookstate ae-cli/internal/attributionlocal ae-cli/internal/hooks/queue.go ae-cli/internal/hooks/queue_test.go
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): store hook state under ae-cli root"
```

## Task 5: Hook Installation, Status, Disable, and Refresh Commands

**Files:**
- Create: `ae-cli/internal/hooks/gitcontext.go`
- Create: `ae-cli/internal/hooks/script.go`
- Create: `ae-cli/internal/hooks/config.go`
- Modify: `ae-cli/internal/hooks/install.go`
- Create: `ae-cli/cmd/hooks.go`
- Modify: `ae-cli/cmd/doctor.go`
- Test: `ae-cli/internal/hooks/gitcontext_test.go`
- Test: `ae-cli/internal/hooks/script_test.go`
- Test: `ae-cli/internal/hooks/config_test.go`
- Test: `ae-cli/internal/hooks/install_test.go`
- Test: `ae-cli/cmd/hooks_test.go`
- Test: `ae-cli/cmd/doctor_test.go`

- [ ] **Step 1: Replace legacy install tests with ownership tests**

In `ae-cli/internal/hooks/install_test.go`, remove tests asserting legacy hook chaining and add:

```go
func TestRenderManagedHookScriptResolvesRuntimeBinary(t *testing.T) {
	script := RenderManagedHookScript("post-commit", "0.1.0")
	for _, want := range []string{"AE_CLI_BIN", "$HOME/.local/bin/ae-cli", "command -v ae-cli", "hook post-commit"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "/tmp/ae-cli") || strings.Contains(script, "ae-cli-local") || strings.Contains(script, "AE_CLI_HOOK_BIN") {
		t.Fatalf("script contains forbidden binary reference:\n%s", script)
	}
}

func TestEnableRepoForceDoesNotPreserveExistingHook(t *testing.T) {
	repo := initRepoWithCommit(t)
	defaultHook := filepath.Join(git(t, repo, "rev-parse", "--git-path", "hooks"), "post-commit")
	if !filepath.IsAbs(defaultHook) {
		defaultHook = filepath.Join(repo, defaultHook)
	}
	if err := os.MkdirAll(filepath.Dir(defaultHook), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defaultHook, []byte("#!/bin/sh\necho legacy\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnableRepo(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	hooksPath := git(t, repo, "config", "--get", "core.hooksPath")
	managed := filepath.Join(hooksPath, "post-commit")
	data, err := os.ReadFile(managed)
	if err != nil {
		t.Fatalf("read managed hook: %v", err)
	}
	if strings.Contains(string(data), "legacy") {
		t.Fatalf("managed hook should not chain legacy hook:\n%s", string(data))
	}
}

func TestEnableRepoRefusesExecutableDefaultHookWithoutForce(t *testing.T) {
	repo := initRepoWithCommit(t)
	defaultHook := filepath.Join(git(t, repo, "rev-parse", "--git-path", "hooks"), "post-commit")
	if !filepath.IsAbs(defaultHook) {
		defaultHook = filepath.Join(repo, defaultHook)
	}
	if err := os.MkdirAll(filepath.Dir(defaultHook), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defaultHook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnableRepo(InstallOptions{CWD: repo, Force: false, NonInteractive: true}); err == nil {
		t.Fatalf("expected refusal without force")
	}
}
```

- [ ] **Step 2: Implement Git context helper**

Create `ae-cli/internal/hooks/gitcontext.go`:

```go
type GitContext struct {
	RepoRoot        string
	RemoteURL       string
	Branch          string
	GitDir          string
	GitCommonDir    string
	DefaultHooksDir string
	WorkspaceID     string
	RepoKey         string
}

func DetectGitContext(cwd string) (*GitContext, error)
```

Implementation details:

- use `git rev-parse --show-toplevel`
- use `git rev-parse --absolute-git-dir`
- use `git rev-parse --git-common-dir`, then convert to canonical absolute under repo root when relative
- use `git rev-parse --git-path hooks` for default hook dir
- use `git config --get remote.origin.url`
- use `git symbolic-ref --short -q HEAD`
- derive `WorkspaceID` with `session.DeriveWorkspaceID(repoRoot, repoRoot, gitDir, gitCommonDir)`
- derive `RepoKey` with `ae-cli/internal/repoidentity.Derive`

- [ ] **Step 3: Implement managed script rendering**

Create `ae-cli/internal/hooks/script.go`:

```go
const ManagedHeaderPrefix = "# ae-cli-managed-hook:"

func RenderManagedHookScript(hookName, generatorVersion string) string
func WriteManagedScripts(dir, generatorVersion string) error
func ParseTemplateVersion(data []byte) (int, bool)
```

`RenderManagedHookScript` must output POSIX `sh` with this behavior:

```sh
#!/bin/sh
# ae-cli-managed-hook: template_version=2 generator_version=<version>
ae_cli="${AE_CLI_BIN:-}"
if [ -z "$ae_cli" ] && [ -x "$HOME/.local/bin/ae-cli" ]; then
  ae_cli="$HOME/.local/bin/ae-cli"
fi
if [ -z "$ae_cli" ]; then
  ae_cli="$(command -v ae-cli 2>/dev/null || true)"
fi
if [ -z "$ae_cli" ] || [ ! -x "$ae_cli" ]; then
  exit 0
fi
```

For `post-commit`, append:

```sh
"$ae_cli" hook post-commit "$@" || true
```

For `post-rewrite`, preserve stdin:

```sh
tmp="${TMPDIR:-/tmp}/ae-cli-post-rewrite.$$"
cat >"$tmp" || true
"$ae_cli" hook post-rewrite "$@" <"$tmp" || true
rm -f "$tmp"
```

Do not invoke any previous hook script.

- [ ] **Step 4: Implement Git config inspection and mutation**

Create `ae-cli/internal/hooks/config.go`:

```go
type ConfigScope string
const (
	ConfigScopeGlobal ConfigScope = "global"
	ConfigScopeLocal ConfigScope = "local"
	ConfigScopeWorktree ConfigScope = "worktree"
)

type HookMode string
const (
	HookModeNone HookMode = "none"
	HookModeGitDefault HookMode = "git_default"
	HookModeAEGlobal HookMode = "ae_global"
	HookModeAERepo HookMode = "ae_repo"
	HookModeNonAEGlobal HookMode = "non_ae_global"
	HookModeNonAERepo HookMode = "non_ae_repo"
)

type EffectiveHookConfig struct {
	Mode HookMode
	Scope ConfigScope
	HooksPath string
	DefaultHooksDir string
	DefaultExecutableHooks []string
	LocalHooksPath string
	WorktreeHooksPath string
	GlobalHooksPath string
}
```

Implement:

- `GlobalManagedHooksPath() (string, error)` returns expanded absolute `~/.ae-cli/git-hooks`
- `RepoManagedHooksPath(gitCommonDir string) (string, error)` returns canonical absolute `<gitCommonDir>/ae-hooks`
- `InspectEffectiveHookConfig(cwd string, gitCtx *GitContext) (*EffectiveHookConfig, error)`
- `SetGlobalHooksPath(path string) error`
- `UnsetGlobalHooksPath() error`
- `SetRepoHooksPath(cwd string, scope ConfigScope, path string) error`
- `UnsetRepoHooksPath(cwd string, scope ConfigScope) error`
- `IsAEManagedPath(path string, gitCtx *GitContext) bool`
- `HasExecutableDefaultHook(dir string) []string`

Use `git config --show-origin --show-scope --get-all core.hooksPath` where helpful, but verify effective behavior with `git config --get core.hooksPath`. Local/worktree commands must use `git config --local` and `git config --worktree` explicitly.

- [ ] **Step 5: Implement enable/disable/status/refresh helpers**

Replace `InstallSharedHooks` in `ae-cli/internal/hooks/install.go` with:

```go
type InstallOptions struct {
	CWD              string
	Force            bool
	NonInteractive   bool
	GeneratorVersion string
}

type StatusOptions struct {
	CWD     string
	Uploads bool
}

type Status struct {
	GlobalEnabled bool
	RepoEnabled bool
	EffectiveMode HookMode
	EffectiveScope ConfigScope
	HooksPath string
	TemplateVersion int
	TemplateStale bool
	BinaryPath string
	BinaryOverride bool
	DefaultExecutableHooks []string
	EligibilityCache string
	ObservedRepo string
}

func EnableGlobal(opts InstallOptions) error
func EnableRepo(opts InstallOptions) error
func DisableGlobal() error
func DisableRepo(cwd string) error
func StatusForRepo(opts StatusOptions) (*Status, error)
func RefreshCurrent(ctx context.Context, c repoResolver, cwd string, binding hookstate.Context) error
func RefreshObserved(ctx context.Context, c batchResolver, binding hookstate.Context) error
func RefreshManagedInstallations(generatorVersion string, out io.Writer) error
```

Overwrite behavior:

- global `--force` overwrites only global `core.hooksPath`
- repo `--force` writes only local/worktree override and never modifies global config
- without `--force`, non-interactive fails with the existing hook path/default hook detail
- interactive prompt asks once and proceeds only on `y` or `yes`
- no code copies, restores, or invokes previous hooks

Registry behavior:

- `EnableGlobal` writes scripts and upserts enabled global installation
- `EnableRepo` writes scripts and upserts enabled repo installation with `git_dir`, `git_common_dir`, `config_scope`, `hooks_path`, and known `repo_key`
- disable marks records disabled only after Git config is unset or already absent
- `DisableRepo` repeats unsetting only effective AE repo-local local/worktree layers until no AE repo-local layer remains effective

- [ ] **Step 6: Add public `ae-cli hooks` command**

Create `ae-cli/cmd/hooks.go`:

```go
var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage AE Git hooks",
}
```

Subcommands:

- `enable --global [--force]`
- `enable --repo [--force]`
- `disable --global`
- `disable --repo`
- `status [--uploads]`
- `refresh [--current]`

Validation:

- exactly one of `--global` / `--repo` for enable and disable
- enable and refresh require a usable token
- `hooks refresh` without `--current` requires stable `auth_subject`

Status output should include at least:

```text
Hook status
  Global:        enabled|disabled|non-ae
  Repo-local:    enabled|disabled|overridden|non-ae
  Effective:     ae_global|ae_repo|git_default|non_ae_global|non_ae_repo|none
  Binary:        /Users/.../.local/bin/ae-cli
  AE_CLI_BIN:    set|unset
  Template:      current|stale|missing
  Eligibility:   positive|negative|missing|expired|unbound
```

- [ ] **Step 7: Extend doctor**

In `ae-cli/cmd/doctor.go`, call `hooks.StatusForRepo` and print hook status after existing sessionless attribution fields. Do not call `EnsureRepoFromRemote` from doctor; use resolve-only diagnostics.

- [ ] **Step 8: Run hook command tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks ./cmd -run 'Hook|Hooks|Enable|Disable|Status|Refresh|Doctor|Managed|Template|Worktree|Default' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit command slice**

```bash
git add ae-cli/internal/hooks ae-cli/cmd/hooks.go ae-cli/cmd/hook_test.go ae-cli/cmd/hooks_test.go ae-cli/cmd/doctor.go ae-cli/cmd/doctor_test.go
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): manage global and repo git hooks"
```

## Task 6: Hidden Hook Dispatcher and Managed Upload Path

**Files:**
- Modify: `ae-cli/cmd/hook.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/uploader_backend.go`
- Modify: `ae-cli/internal/hooks/queue.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/types.go`
- Test: `ae-cli/cmd/hook_test.go`
- Test: `ae-cli/internal/hooks/handler_test.go`
- Test: `ae-cli/internal/hooks/uploader_backend_test.go`
- Test: `ae-cli/internal/attributionlocal/sync_test.go`

- [ ] **Step 1: Write failing dispatcher tests**

In `ae-cli/cmd/hook_test.go`, replace marker-based tests with:

```go
func TestHookPostCommitSkipsUnknownRepoWithoutQueue(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return &recordingHookUploader{} }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(repo)

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ae-cli", "state", "attribution")); !os.IsNotExist(err) {
		t.Fatalf("unexpected durable attribution state for unknown repo: %v", err)
	}
}

func TestHookPostCommitUsesResolvedRepoConfigID(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "repo-host-or-github-key", 123)

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(repo)

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(u.events) != 1 || u.events[0].RepoConfigID != 123 {
		t.Fatalf("events = %+v", u.events)
	}
}
```

Use real helper implementations instead of pseudocode:

- `writeTestToken` writes `~/.ae-cli/token.json` with valid future expiry and `auth_subject`
- `writePositiveEligibility` uses `hookstate.LoadEligibilityCache` and `PutPositive`
- `recordingHookUploader` implements `hooks.Uploader`

- [ ] **Step 2: Extend `HookEvent` and uploader**

In `ae-cli/internal/hooks/queue.go`, add to `HookEvent`:

```go
ServerURL    string `json:"server_url,omitempty"`
AuthSubject string `json:"auth_subject,omitempty"`
RepoConfigID int   `json:"repo_config_id,omitempty"`
RepoKey      string `json:"repo_key,omitempty"`
```

In `ae-cli/internal/hooks/uploader_backend.go`, pass `RepoConfigID` into checkpoint/rewrite requests.

- [ ] **Step 3: Remove `.ae` marker dependency from active handler**

In `ae-cli/internal/hooks/handler.go`:

- delete `repoEnsureClient`
- remove `session.ReadMarker` from `PostCommit`, `PostRewrite`, and `Flush`
- remove `bootstrapMarkerFromEnv` from active path
- do not call `EnsureRepoFromRemote`
- add input type:

```go
type ExecutionContext struct {
	ServerURL     string
	AuthSubject  string
	RepoConfigID int
	RepoKey       string
	RepoFullName  string
	WorkspaceID   string
	RepoRoot      string
	Branch        string
	DurableReplay bool
}
```

Add:

```go
func (h *Handler) PostCommitResolved(ctx context.Context, exec ExecutionContext) error
func (h *Handler) PostRewriteResolved(ctx context.Context, exec ExecutionContext, rewriteType string, stdin io.Reader) error
func (h *Handler) FlushResolved(ctx context.Context, exec ExecutionContext) error
```

Keep legacy `PostCommit`, `PostRewrite`, and `Flush` only as thin fail-open wrappers for tests or remove them after updating callers. The hidden commands should use only `Resolved` methods.

Queue writes:

- only when `exec.DurableReplay == true`
- queued item must include `server_url`, `auth_subject`, `repo_config_id`, `repo_key`, and `workspace_id`

Collector snapshot cache and attribution sync:

- use `exec.WorkspaceID`
- no `.ae` marker read/write

- [ ] **Step 4: Add eligibility resolver in hidden command**

In `ae-cli/cmd/hook.go`, before creating handler work:

```go
gitCtx, err := hooks.DetectGitContext(cwd)
if err != nil { return nil }
execCtx, ok := resolveHookExecutionContext(ctx, gitCtx)
if !ok { return nil }
return hooks.NewHandler(newHookUploader()).PostCommitResolved(ctx, execCtx)
```

Implement `resolveHookExecutionContext` in `cmd/hook.go` or a focused helper file:

1. load token and server URL
2. require usable credential for cache authorization
3. derive stable `auth_subject` from token file
4. write observed repo best-effort:
   - context-bound when stable
   - unbound when not stable
5. if stable, check eligibility cache
6. on miss/expiry, call `apiClient.ResolveRepoFromRemote` with 500ms context timeout
7. write positive/negative cache only when stable
8. return `DurableReplay: stable`
9. skip AE work on negative, unknown, timeout, missing token, or failed resolve

For no stable `auth_subject` but usable authenticated credential, allow immediate resolve/upload with `DurableReplay: false`.

- [ ] **Step 5: Bind attribution sync to repo context and omit raw fields**

In `ae-cli/internal/attributionlocal/types.go`, add:

```go
RepoConfigID int
ServerURL    string
AuthSubject  string
RepoKey      string
ManagedUpload bool
```

In `ae-cli/internal/attributionlocal/sync.go`, add:

```go
type RunOptions struct {
	WorkspaceRoot string
	WorkspaceID   string
	ServerURL     string
	AuthSubject   string
	RepoConfigID  int
	RepoKey       string
	DurableReplay bool
	ManagedUpload bool
}

func (e *SyncEngine) Run(ctx context.Context, opts RunOptions) error
```

`RunForWorkspace` can call `Run` with legacy options only for non-managed clients. Hidden hook commands and `ae-cli sync` must call `Run` with `ManagedUpload: true`.

In `toClientUsageRequest`, when `ManagedUpload` is true:

- set `RepoConfigID`
- omit `RawSourcePath`
- omit `RawSourceLocator`
- omit `RawPayload`

Replay skips spooled events when binding fields do not match current stable context. If no stable `auth_subject`, do not write spools or ledger.

- [ ] **Step 6: Run dispatcher tests**

Run:

```bash
cd ae-cli
go test ./cmd ./internal/hooks ./internal/attributionlocal -run 'HookPost|PostCommit|PostRewrite|RepoConfigID|ManagedUpload|NoMarker|Raw|Replay|Queue' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit dispatcher slice**

```bash
git add ae-cli/cmd/hook.go ae-cli/cmd/hook_test.go ae-cli/internal/hooks ae-cli/internal/attributionlocal
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): resolve hook eligibility before uploads"
```

## Task 7: `init`, `sync`, and `doctor` Contract Updates

**Files:**
- Modify: `ae-cli/cmd/init.go`
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/internal/repolink/ensure.go`
- Test: `ae-cli/cmd/init_test.go`
- Test: `ae-cli/cmd/sync_test.go`
- Test: `ae-cli/cmd/doctor_test.go`
- Test: `ae-cli/internal/repolink/ensure_test.go`

- [ ] **Step 1: Add init tests for default no hook install**

Create `ae-cli/cmd/init_test.go`:

```go
package cmd

import (
	"bytes"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
)

func TestInitDefaultsToNoHookInstall(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	var installed bool
	origInstall := enableRepoHooks
	enableRepoHooks = func(opts hooks.InstallOptions) error {
		installed = true
		return nil
	}
	t.Cleanup(func() { enableRepoHooks = origInstall })

	cmd := initCmd
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	withWorkingDir(t, repo, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if installed {
		t.Fatalf("init should not install hooks by default")
	}
}
```

Implement `withWorkingDir` helper in `ae-cli/cmd/test_helpers_test.go`.

- [ ] **Step 2: Update init command**

In `ae-cli/cmd/init.go`:

- add flags:

```go
var initHooksMode string
var initForce bool
```

- register:

```go
initCmd.Flags().StringVar(&initHooksMode, "hooks", "none", "hook mode: none, repo, or global")
initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing hook path when enabling hooks")
```

- remove unconditional `installSharedHooks`
- keep `repolink.Ensure` because `init` is explicit user registration
- create `~/.ae-cli/state/attribution`
- write observed repo and positive eligibility only when stable `auth_subject`
- delegate:

```go
switch initHooksMode {
case "none":
case "repo":
	err = enableRepoHooks(hooks.InstallOptions{CWD: ctx.repoRoot, Force: initForce, GeneratorVersion: buildinfo.Version})
case "global":
	err = enableGlobalHooks(hooks.InstallOptions{CWD: ctx.repoRoot, Force: initForce, GeneratorVersion: buildinfo.Version})
default:
	return fmt.Errorf("invalid --hooks %q: expected none, repo, or global", initHooksMode)
}
```

Expose package variables for tests:

```go
var enableRepoHooks = hooks.EnableRepo
var enableGlobalHooks = hooks.EnableGlobal
```

- [ ] **Step 3: Update sync command**

In `ae-cli/cmd/sync.go`:

- remove `repolink.Ensure`
- detect Git context
- call read-only resolve endpoint
- fail with:

```text
repository is not registered or reporting-enabled; run 'ae-cli init' or ask an admin to configure it
```

- run `hooks.FlushResolved` and `attributionlocal.SyncEngine.Run` with `repo_config_id`
- do not create repositories from sync
- if no stable `auth_subject`, allow immediate upload after resolve but no durable cache/spool/ledger writes

- [ ] **Step 4: Update doctor command**

In `ae-cli/cmd/doctor.go`:

- remove `repolink.Ensure`
- use resolve-only status
- print cache and hook diagnostics
- make failure to contact backend a diagnostic line, not a repo creation side effect

- [ ] **Step 5: Run command tests**

Run:

```bash
cd ae-cli
go test ./cmd ./internal/repolink -run 'Init|Sync|Doctor|Ensure|Hooks' -count=1
```

Expected: PASS. Existing `repolink.Ensure` tests may remain because `init` still uses explicit ensure.

- [ ] **Step 6: Commit command contract slice**

```bash
git add ae-cli/cmd/init.go ae-cli/cmd/init_test.go ae-cli/cmd/sync.go ae-cli/cmd/sync_test.go ae-cli/cmd/doctor.go ae-cli/cmd/doctor_test.go ae-cli/internal/repolink
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): separate repo registration from hooks"
```

## Task 8: Installer and Upgrade Hook Refresh

**Files:**
- Modify: `ae-cli/install.sh`
- Modify: `ae-cli/install.ps1`
- Modify: `ae-cli/test/install-test.sh`
- Modify: `ae-cli/internal/hooks/install.go`
- Test: `ae-cli/internal/hooks/install_test.go`
- Test: `ae-cli/test/install-test.sh`

- [ ] **Step 1: Add hook refresh command path**

Add hidden command in `ae-cli/cmd/hooks.go`:

```go
var hooksRefreshInstallationsCmd = &cobra.Command{
	Use:    "refresh-installations",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return hooks.RefreshManagedInstallations(buildinfo.Version, cmd.ErrOrStderr())
	},
}
```

Attach it under `hooksCmd`.

- [ ] **Step 2: Implement refresh installation behavior tests**

In `ae-cli/internal/hooks/install_test.go`, add:

```go
func TestRefreshManagedInstallationsRewritesActiveGlobalFromGitConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".ae-cli", "git-hooks")
	git(t, home, "config", "--global", "core.hooksPath", globalDir)
	if err := RefreshManagedInstallations("test-version", io.Discard); err != nil {
		t.Fatalf("RefreshManagedInstallations: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(globalDir, "post-commit"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(data), "template_version=2") {
		t.Fatalf("stale script: %s", data)
	}
}

func TestRefreshManagedInstallationsSkipsDisabledRepoRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	disabledPath := filepath.Join(home, "repo", ".git", "ae-hooks")
	registry, _ := hookstate.LoadInstallations()
	registry.Upsert(hookstate.InstallationRecord{
		Mode: "repo", GitCommonDir: filepath.Join(home, "repo", ".git"),
		ConfigScope: "local", HooksPath: disabledPath, Enabled: false,
		TemplateVersion: 1, UpdatedAt: time.Now(),
	})
	registry.Save()
	if err := RefreshManagedInstallations("test-version", io.Discard); err != nil {
		t.Fatalf("RefreshManagedInstallations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disabledPath, "post-commit")); !os.IsNotExist(err) {
		t.Fatalf("disabled repo hook should not be rewritten, stat err=%v", err)
	}
}
```

Use a test-specific global Git config by setting `GIT_CONFIG_GLOBAL` in the test before running `git config --global`.

- [ ] **Step 3: Update Unix installer**

In `ae-cli/install.sh`, after `install_binary` and before final success output:

```bash
refresh_managed_hooks() {
  if [[ -x "$TARGET_PATH" ]]; then
    "$TARGET_PATH" hooks refresh-installations >/dev/null 2>&1 || {
      echo "Warning: installed ae-cli but failed to refresh managed hook scripts." >&2
    }
  fi
}
```

Call `refresh_managed_hooks` after `write_cli_config`.

- [ ] **Step 4: Update Windows installer**

In `ae-cli/install.ps1`, after binary copy and config writes:

```powershell
try {
  & $TargetPath hooks refresh-installations *> $null
} catch {
  Write-Host "Warning: installed ae-cli but failed to refresh managed hook scripts."
}
```

- [ ] **Step 5: Run install tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks ./cmd -run 'RefreshManagedInstallations|refresh-installations' -count=1
bash test/install-test.sh
```

Expected: PASS.

- [ ] **Step 6: Commit installer slice**

```bash
git add ae-cli/install.sh ae-cli/install.ps1 ae-cli/test/install-test.sh ae-cli/internal/hooks/install.go ae-cli/internal/hooks/install_test.go ae-cli/cmd/hooks.go ae-cli/cmd/hooks_test.go
git -c core.hooksPath=/dev/null commit -m "feat(ae-cli): refresh managed hooks after install"
```

## Task 9: Architecture and User Documentation

**Files:**
- Modify: `docs/architecture.md`
- Modify: `ae-cli/README.md`
- Optional modify: `docs/ae-cli-hook-attribution-flow.md`

- [ ] **Step 1: Update architecture after code is in place**

In `docs/architecture.md`, update only current-state sections:

- all user-level CLI state under `~/.ae-cli/`
- `~/.ae-cli/git-hooks` for global hook scripts
- `<canonical git common dir>/ae-hooks` for repo-local hook scripts
- hook path uses read-only `resolve-remote`, never `ensure-remote`
- `init` is explicit repo registration and cache bootstrap
- `sync` is resolve-first and does not create repos
- checkpoint, rewrite, and managed tool usage payloads carry `repo_config_id`
- no active `.ae` workspace metadata in hook flow

- [ ] **Step 2: Update CLI README examples**

In `ae-cli/README.md`, add examples:

```markdown
ae-cli hooks enable --global
ae-cli hooks enable --repo
ae-cli hooks status
ae-cli hooks disable --repo
ae-cli hooks disable --global
ae-cli init --hooks none
ae-cli init --hooks repo --force
```

Document ownership clearly:

- `--force` overwrites the relevant hook path
- AE-managed hooks do not chain previous hooks
- official binary path is `~/.local/bin/ae-cli`
- `AE_CLI_BIN` is an advanced override

- [ ] **Step 3: Run documentation checks**

Run:

```bash
git diff --check
rg -n '\.ai-efficiency|/\.ae/|AE_CLI_HOOK_BIN|ae-cli-local' docs/architecture.md ae-cli/README.md docs/superpowers/specs/2026-05-23-global-git-hooks-design.md
```

Expected: `git diff --check` passes. The `rg` command may show historical mentions in the spec only where it explicitly forbids old roots; it must not show new architecture or README instructions using old roots.

- [ ] **Step 4: Commit documentation slice**

```bash
git add docs/architecture.md ae-cli/README.md docs/ae-cli-hook-attribution-flow.md
git -c core.hooksPath=/dev/null commit -m "docs(ae-cli): document global git hook runtime"
```

## Task 10: Full Verification and Manual Integration

**Files:**
- No new files expected.

- [ ] **Step 1: Run backend tests**

Run:

```bash
cd backend
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run ae-cli tests**

Run:

```bash
cd ae-cli
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Build CLI**

Run:

```bash
cd ae-cli
go build -o /tmp/ae-cli-hook-contract ./main.go
/tmp/ae-cli-hook-contract version
```

Expected: binary builds and prints version information.

- [ ] **Step 4: Manual global hook smoke test**

Use a temp HOME and temp global Git config:

```bash
TMP_HOME="$(mktemp -d)"
TMP_GLOBAL_CONFIG="$TMP_HOME/gitconfig"
HOME="$TMP_HOME" GIT_CONFIG_GLOBAL="$TMP_GLOBAL_CONFIG" /tmp/ae-cli-hook-contract hooks enable --global --force
HOME="$TMP_HOME" GIT_CONFIG_GLOBAL="$TMP_GLOBAL_CONFIG" git config --global --get core.hooksPath
```

Expected: output is an absolute path ending in `.ae-cli/git-hooks`, and `post-commit` contains `AE_CLI_BIN`, `$HOME/.local/bin/ae-cli`, and `command -v ae-cli`.

- [ ] **Step 5: Manual repo-local hook smoke test**

```bash
REPO="$(mktemp -d)"
git -C "$REPO" init
git -C "$REPO" config user.email alice@example.com
git -C "$REPO" config user.name alice
git -C "$REPO" remote add origin https://repo-host.example.com/org/repo.git
touch "$REPO/a.txt"
git -C "$REPO" add a.txt
git -C "$REPO" commit -m init
(cd "$REPO" && HOME="$TMP_HOME" GIT_CONFIG_GLOBAL="$TMP_GLOBAL_CONFIG" /tmp/ae-cli-hook-contract hooks enable --repo --force --server http://127.0.0.1:1)
git -C "$REPO" config --get core.hooksPath
```

Expected: repo-local `core.hooksPath` is an absolute path ending in `.git/ae-hooks`; global Git config is unchanged.

- [ ] **Step 6: Unknown repo fail-open smoke test**

In a temp repo without cache or reachable backend:

```bash
TIMEFORMAT='%R'
time git -C "$REPO" commit --allow-empty -m unknown-repo-smoke
find "$TMP_HOME/.ae-cli/state/attribution" -type f -print 2>/dev/null || true
```

Expected: commit completes quickly, no durable queue/spool/ledger is created for the unknown repository.

- [ ] **Step 7: Context mismatch replay smoke test**

Create a fake queue or spool item with `server_url` / `auth_subject` that differs from the current token context, then run:

```bash
HOME="$TMP_HOME" /tmp/ae-cli-hook-contract hooks status --uploads
```

Expected: status groups by backend/account/repo/workspace and does not upload mismatched items. If a skipped ledger record is written, it must contain only operational metadata and no raw payloads or local file paths.

- [ ] **Step 8: Final diff hygiene**

Run:

```bash
git status --short
git diff --check
perl -ne 'print if /TB[D]/ || /TO[D]O/ || /implement\s+later/ || /fill\s+in\s+details/ || /Similar\s+to/ || /appropriate\s+error\s+handling/ || /handle\s+edge\s+cases/ || /Write\s+tests\s+for\s+the\s+above/' docs/superpowers/plans/2026-05-24-global-git-hooks-implementation.md
rg -n '\.ai-efficiency|/\.ae/|AE_CLI_HOOK_BIN|ae-cli-local' ae-cli backend docs/architecture.md ae-cli/README.md
```

Expected:

- no whitespace errors
- placeholder scan has no hits in the plan
- old root scan has hits only in deleted lines, historical docs, tests that assert old roots are ignored, or code comments explaining ignored historical state

- [ ] **Step 9: Commit final fixes if verification required changes**

If Step 1-8 required any changes:

```bash
git add <changed-files>
git -c core.hooksPath=/dev/null commit -m "fix(ae-cli): harden hook contract verification"
```

Expected: clean working tree after commit.

## Implementation Notes and Conflict Checks

- Managed hook paths must never call `EnsureRepoFromRemote`; only `ae-cli init` may use the existing create-or-ensure endpoint.
- `active` and `webhook_failed` are reporting-enabled. `inactive` is not. There is no per-user repo reporting permission check in v1.
- `--force` overwrites only the relevant hook path: global mode writes global Git config, repo mode writes local/worktree Git config and leaves global Git config unchanged.
- AE-managed hooks do not preserve, copy, restore, or chain previous non-AE hooks.
- Hook scripts resolve runtime binary in this order: `AE_CLI_BIN`, `~/.local/bin/ae-cli`, `command -v ae-cli`.
- Do not introduce `AE_CLI_HOOK_BIN`.
- Do not use `~/.ai-efficiency`, `~/.ai-efficiency/bin/ae-cli-local`, or `.ae` as active state or metadata.
- Local durable cache, queue, spool, registry, and ledger writes require stable `auth_subject`. Immediate online upload without stable subject is allowed only for the current invocation and must not leave replayable durable state.
- Positive cache entries must contain non-zero `repo_config_id` and match `server_url + auth_subject + repo_key`.
- Managed hook and `ae-cli sync` tool usage uploads must omit `raw_source_path`, `raw_source_locator`, and `raw_payload`.
- `docs/architecture.md` is updated only after implementation so it reflects current code, not proposed behavior.
