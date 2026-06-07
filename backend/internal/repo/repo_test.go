package repo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func setupTest(t *testing.T) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	return client, svc
}

// createSCMProvider creates a minimal SCM provider for FK satisfaction.
func createSCMProvider(t *testing.T, client *ent.Client) *ent.ScmProvider {
	t.Helper()
	p, err := client.ScmProvider.Create().
		SetName("test-github").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted-creds").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}
	return p
}

// createRepo is a shortcut that creates a repo via CreateDirect.
func createRepo(t *testing.T, svc *Service, providerID int, fullName string) *ent.RepoConfig {
	t.Helper()
	rc, err := svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: providerID,
		Name:          fullName,
		FullName:      "org/" + fullName,
		CloneURL:      "https://github.com/org/" + fullName + ".git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("createRepo(%s): %v", fullName, err)
	}
	return rc
}

// ---------------------------------------------------------------------------
// parseToken
// ---------------------------------------------------------------------------

func TestParseToken_JSONCredentials(t *testing.T) {
	got := parseToken(`{"token":"abc123"}`)
	if got != "abc123" {
		t.Errorf("parseToken JSON = %q, want %q", got, "abc123")
	}
}

func TestParseToken_PlainString(t *testing.T) {
	got := parseToken("plain-token")
	if got != "plain-token" {
		t.Errorf("parseToken plain = %q, want %q", got, "plain-token")
	}
}

func TestParseToken_InvalidJSON(t *testing.T) {
	got := parseToken("{bad json")
	if got != "{bad json" {
		t.Errorf("parseToken invalid JSON = %q, want %q", got, "{bad json")
	}
}

func TestParseToken_EmptyString(t *testing.T) {
	got := parseToken("")
	if got != "" {
		t.Errorf("parseToken empty = %q, want %q", got, "")
	}
}

// ---------------------------------------------------------------------------
// newGitHubProvider / newBitbucketProvider
// ---------------------------------------------------------------------------

func TestNewGitHubProvider(t *testing.T) {
	p, err := newGitHubProvider("https://api.github.com", "test-token", zap.NewNop(), "")
	if err != nil {
		t.Fatalf("newGitHubProvider error: %v", err)
	}
	if p == nil {
		t.Fatal("newGitHubProvider returned nil")
	}
}

func TestNewBitbucketProvider(t *testing.T) {
	p, err := newBitbucketProvider("https://bitbucket.example.com", "test-token", zap.NewNop(), "")
	if err != nil {
		t.Fatalf("newBitbucketProvider error: %v", err)
	}
	if p == nil {
		t.Fatal("newBitbucketProvider returned nil")
	}
}

// ---------------------------------------------------------------------------
// generateSecret
// ---------------------------------------------------------------------------

func TestGenerateSecret_Length(t *testing.T) {
	s, err := generateSecret(32)
	if err != nil {
		t.Fatalf("generateSecret error: %v", err)
	}
	// 32 bytes → 64 hex chars
	if len(s) != 64 {
		t.Errorf("generateSecret len = %d, want 64", len(s))
	}
	// Must be valid hex
	if _, err := hex.DecodeString(s); err != nil {
		t.Errorf("generateSecret not valid hex: %v", err)
	}
}

func TestGenerateSecret_Unique(t *testing.T) {
	a, _ := generateSecret(32)
	b, _ := generateSecret(32)
	if a == b {
		t.Error("generateSecret produced identical values")
	}
}

// ---------------------------------------------------------------------------
// NewService
// ---------------------------------------------------------------------------

func TestNewService(t *testing.T) {
	client := testdb.Open(t)
	svc := NewService(client, "key", zap.NewNop())
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// ---------------------------------------------------------------------------
// CreateDirect
// ---------------------------------------------------------------------------

func TestCreateDirect(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)

	rc, err := svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: p.ID,
		Name:          "my-repo",
		FullName:      "org/my-repo",
		CloneURL:      "https://github.com/org/my-repo.git",
		DefaultBranch: "develop",
		GroupID:       "team-alpha",
	})
	if err != nil {
		t.Fatalf("CreateDirect error: %v", err)
	}

	if rc.Name != "my-repo" {
		t.Errorf("Name = %q, want %q", rc.Name, "my-repo")
	}
	if rc.FullName != "org/my-repo" {
		t.Errorf("FullName = %q, want %q", rc.FullName, "org/my-repo")
	}
	if rc.CloneURL != "https://github.com/org/my-repo.git" {
		t.Errorf("CloneURL = %q", rc.CloneURL)
	}
	if rc.DefaultBranch != "develop" {
		t.Errorf("DefaultBranch = %q, want %q", rc.DefaultBranch, "develop")
	}
	if rc.Status != repoconfig.StatusActive {
		t.Errorf("Status = %q, want %q", rc.Status, repoconfig.StatusActive)
	}
	if rc.GroupID == nil || *rc.GroupID != "team-alpha" {
		t.Errorf("GroupID = %v, want team-alpha", rc.GroupID)
	}
}

func TestCreateDirect_NoGroupID(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)

	rc, err := svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: p.ID,
		Name:          "repo-no-group",
		FullName:      "org/repo-no-group",
		CloneURL:      "https://github.com/org/repo-no-group.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateDirect error: %v", err)
	}
	if rc.GroupID != nil {
		t.Errorf("GroupID = %v, want nil", rc.GroupID)
	}
}

func TestCreateDirect_AllowsUnboundRepo(t *testing.T) {
	_, svc := setupTest(t)

	rc, err := svc.CreateDirect(context.Background(), CreateDirectRequest{
		RepoKey:       "github.com/org/repo-unbound",
		Name:          "repo-unbound",
		FullName:      "org/repo-unbound",
		CloneURL:      "https://github.com/org/repo-unbound.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateDirect error: %v", err)
	}
	if rc.RepoKey != "github.com/org/repo-unbound" {
		t.Fatalf("RepoKey = %q, want %q", rc.RepoKey, "github.com/org/repo-unbound")
	}
	if rc.Edges.ScmProvider != nil {
		t.Fatalf("expected nil scm provider edge, got %#v", rc.Edges.ScmProvider)
	}
}

func TestRepoConfigHookDerivesRepoKeyOnRawCreate(t *testing.T) {
	client := testdb.Open(t)

	rc, err := client.RepoConfig.Create().
		SetName("raw-repo").
		SetFullName("org/raw-repo").
		SetCloneURL("https://github.com/org/raw-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("raw create repo config: %v", err)
	}
	if rc.RepoKey != "github.com/org/raw-repo" {
		t.Fatalf("repo_key = %q, want %q", rc.RepoKey, "github.com/org/raw-repo")
	}
}

func TestFindOrCreateFromRemote_RequeriesAfterConstraintConflict(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	identity, err := DeriveRepoIdentity("https://github.com/acme/platform.git")
	if err != nil {
		t.Fatalf("DeriveRepoIdentity: %v", err)
	}

	injected := false
	client.RepoConfig.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if !m.Op().Is(ent.OpCreate) || injected {
				return next.Mutate(ctx, m)
			}
			injected = true
			if _, err := client.RepoConfig.Create().
				SetRepoKey(identity.RepoKey).
				SetName(identity.Name).
				SetFullName(identity.FullName).
				SetCloneURL(identity.CloneURL).
				SetDefaultBranch("main").
				SetStatus(repoconfig.StatusActive).
				Save(ctx); err != nil {
				return nil, err
			}
			return next.Mutate(ctx, m)
		})
	})

	rc, err := svc.FindOrCreateFromRemote(ctx, identity.CloneURL, "main")
	if err != nil {
		t.Fatalf("FindOrCreateFromRemote: %v", err)
	}
	if rc.RepoKey != identity.RepoKey {
		t.Fatalf("repo_key = %q, want %q", rc.RepoKey, identity.RepoKey)
	}
	count, err := client.RepoConfig.Query().Where(repoconfig.RepoKeyEQ(identity.RepoKey)).Count(ctx)
	if err != nil {
		t.Fatalf("count repo configs: %v", err)
	}
	if count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
}

func TestEnsureFromRemote_CreatesUnboundRepo(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	rc, err := svc.EnsureFromRemote(ctx, "https://github.com/acme/platform.git", "main")
	if err != nil {
		t.Fatalf("EnsureFromRemote error: %v", err)
	}
	if rc.RepoKey != "github.com/acme/platform" {
		t.Fatalf("RepoKey = %q, want %q", rc.RepoKey, "github.com/acme/platform")
	}
	if rc.Edges.ScmProvider != nil {
		t.Fatalf("ScmProvider edge = %#v, want nil", rc.Edges.ScmProvider)
	}

	count := client.RepoConfig.Query().CountX(ctx)
	if count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
}

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

func TestFindOrCreateFromRemote_FallsBackWhenIdentityParsingFails(t *testing.T) {
	_, svc := setupTest(t)
	ctx := context.Background()

	rc, err := svc.FindOrCreateFromRemote(ctx, "ssh://git@repo-host.example.com/platform.git", "main")
	if err != nil {
		t.Fatalf("FindOrCreateFromRemote: %v", err)
	}
	if rc.CloneURL != "ssh://git@repo-host.example.com/platform.git" {
		t.Fatalf("clone_url = %q, want original remote", rc.CloneURL)
	}
	if rc.RepoKey == "" {
		t.Fatal("expected fallback repo_key to be populated")
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestGet_Existing(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	created := createRepo(t, svc, p.ID, "get-repo")

	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	// Should eagerly load SCM provider edge
	if got.Edges.ScmProvider == nil {
		t.Error("ScmProvider edge not loaded")
	}
}

func TestGet_NotFound(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.Get(context.Background(), 99999)
	if err == nil {
		t.Fatal("Get non-existent should return error")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestList_Empty(t *testing.T) {
	_, svc := setupTest(t)

	repos, total, err := svc.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(repos) != 0 {
		t.Errorf("repos len = %d, want 0", len(repos))
	}
}

func TestList_WithItems(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	createRepo(t, svc, p.ID, "repo-a")
	createRepo(t, svc, p.ID, "repo-b")
	createRepo(t, svc, p.ID, "repo-c")

	repos, total, err := svc.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(repos) != 3 {
		t.Errorf("repos len = %d, want 3", len(repos))
	}
}

func TestList_Pagination(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	for i := 0; i < 5; i++ {
		createRepo(t, svc, p.ID, fmt.Sprintf("page-repo-%d", i))
	}

	// Page 1, size 2
	repos, total, err := svc.List(context.Background(), ListOpts{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 1 error: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(repos) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(repos))
	}

	// Page 3, size 2 → 1 item
	repos, _, err = svc.List(context.Background(), ListOpts{Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 3 error: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("page 3 len = %d, want 1", len(repos))
	}
}

func TestList_FilterByStatus(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)

	rc := createRepo(t, svc, p.ID, "active-repo")
	_ = rc

	// Create an inactive repo by updating status
	inactive := createRepo(t, svc, p.ID, "inactive-repo")
	_, err := svc.Update(context.Background(), inactive.ID, UpdateRequest{Status: "inactive"})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}

	repos, total, err := svc.List(context.Background(), ListOpts{Status: "active"})
	if err != nil {
		t.Fatalf("List filter status error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

func TestList_FilterByGroupID(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)

	// Create repos with different group IDs
	svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: p.ID,
		Name:          "group-a-repo",
		FullName:      "org/group-a-repo",
		CloneURL:      "https://github.com/org/group-a-repo.git",
		DefaultBranch: "main",
		GroupID:       "group-a",
	})
	svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: p.ID,
		Name:          "group-b-repo",
		FullName:      "org/group-b-repo",
		CloneURL:      "https://github.com/org/group-b-repo.git",
		DefaultBranch: "main",
		GroupID:       "group-b",
	})

	repos, total, err := svc.List(context.Background(), ListOpts{GroupID: "group-a"})
	if err != nil {
		t.Fatalf("List filter group error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUpdate_Name(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "old-name")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{Name: "new-name"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.Name != "new-name" {
		t.Errorf("Name = %q, want %q", updated.Name, "new-name")
	}
}

func TestUpdate_Status(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "status-repo")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{Status: "inactive"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.Status != repoconfig.StatusInactive {
		t.Errorf("Status = %q, want %q", updated.Status, repoconfig.StatusInactive)
	}
}

func TestUpdate_GroupID(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "group-repo")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{GroupID: "new-group"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.GroupID == nil || *updated.GroupID != "new-group" {
		t.Errorf("GroupID = %v, want new-group", updated.GroupID)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.Update(context.Background(), 99999, UpdateRequest{Name: "x"})
	if err == nil {
		t.Fatal("Update non-existent should return error")
	}
	if err.Error() != "repo not found" {
		t.Errorf("error = %q, want %q", err.Error(), "repo not found")
	}
}

// ---------------------------------------------------------------------------
// Delete (cascading)
// ---------------------------------------------------------------------------

func TestDelete_CascadingRelations(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "delete-me")

	// Create child: PR record
	_, err := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(42).
		Save(ctx)
	if err != nil {
		t.Fatalf("create pr record: %v", err)
	}

	// Delete the repo
	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Verify repo is gone
	_, err = client.RepoConfig.Get(ctx, rc.ID)
	if !ent.IsNotFound(err) {
		t.Errorf("repo should be deleted, got err: %v", err)
	}

	// Verify all children are gone
	prs, _ := client.PrRecord.Query().All(ctx)
	if len(prs) != 0 {
		t.Errorf("pr records count = %d, want 0", len(prs))
	}
}

func TestDelete_NotFound(t *testing.T) {
	_, svc := setupTest(t)

	err := svc.Delete(context.Background(), 99999)
	if err == nil {
		t.Fatal("Delete non-existent should return error")
	}
}

// ---------------------------------------------------------------------------
// newSCMProvider (method on Service)
// ---------------------------------------------------------------------------

func TestNewSCMProvider_GitHub(t *testing.T) {
	_, svc := setupTest(t)

	p, err := svc.newSCMProvider("github", "https://api.github.com", "token")
	if err != nil {
		t.Fatalf("newSCMProvider github error: %v", err)
	}
	if p == nil {
		t.Fatal("newSCMProvider github returned nil")
	}
}

func TestNewSCMProvider_Bitbucket(t *testing.T) {
	_, svc := setupTest(t)

	p, err := svc.newSCMProvider("bitbucket_server", "https://bitbucket.example.com", "token")
	if err != nil {
		t.Fatalf("newSCMProvider bitbucket error: %v", err)
	}
	if p == nil {
		t.Fatal("newSCMProvider bitbucket returned nil")
	}
}

func TestNewSCMProvider_Unsupported(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.newSCMProvider("gitlab", "https://gitlab.com", "token")
	if err == nil {
		t.Fatal("newSCMProvider unsupported should return error")
	}
	expected := "unsupported provider type: gitlab"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

// ---------------------------------------------------------------------------
// GetSCMProvider
// ---------------------------------------------------------------------------

func TestGetSCMProvider_Success(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	// Create an SCM provider with properly encrypted credentials
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, err := pkg.Encrypt(`{"token":"ghp_test123"}`, encKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	sp, err := client.ScmProvider.Create().
		SetName("github-encrypted").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}

	rc, err := svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: sp.ID,
		Name:          "scm-repo",
		FullName:      "org/scm-repo",
		CloneURL:      "https://github.com/org/scm-repo.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	provider, gotRC, err := svc.GetSCMProvider(ctx, rc.ID)
	if err != nil {
		t.Fatalf("GetSCMProvider: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if gotRC.ID != rc.ID {
		t.Errorf("repo config ID = %d, want %d", gotRC.ID, rc.ID)
	}
}

func TestGetSCMProvider_UnboundRepo(t *testing.T) {
	_, svc := setupTest(t)
	ctx := context.Background()

	rc, err := svc.CreateDirect(ctx, CreateDirectRequest{
		RepoKey:       "github.com/org/unbound-repo",
		Name:          "unbound-repo",
		FullName:      "org/unbound-repo",
		CloneURL:      "https://github.com/org/unbound-repo.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	_, gotRC, err := svc.GetSCMProvider(ctx, rc.ID)
	if !errors.Is(err, ErrRepoUnbound) {
		t.Fatalf("GetSCMProvider error = %v, want ErrRepoUnbound", err)
	}
	if gotRC == nil || gotRC.ID != rc.ID {
		t.Fatalf("repo config = %#v, want ID %d", gotRC, rc.ID)
	}
}

func TestGetSCMProvider_NotFound(t *testing.T) {
	_, svc := setupTest(t)

	_, _, err := svc.GetSCMProvider(context.Background(), 99999)
	if err == nil {
		t.Fatal("GetSCMProvider non-existent should return error")
	}
}

func TestGetSCMProvider_DecryptError(t *testing.T) {
	client := testdb.Open(t)
	// Use a different key than what was used to encrypt
	svc := NewService(client, "1111111111111111111111111111111111111111111111111111111111111111", zap.NewNop())
	ctx := context.Background()

	// Create provider with credentials encrypted with a different key
	otherKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, _ := pkg.Encrypt("token", otherKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-bad-key").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	rc, _ := svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: sp.ID,
		Name:          "bad-key-repo",
		FullName:      "org/bad-key-repo",
		CloneURL:      "https://github.com/org/bad-key-repo.git",
		DefaultBranch: "main",
	})

	_, _, err := svc.GetSCMProvider(ctx, rc.ID)
	if err == nil {
		t.Fatal("GetSCMProvider with wrong key should return error")
	}
}

func TestGetSCMProvider_NonExistentRepo(t *testing.T) {
	_, svc := setupTest(t)
	ctx := context.Background()

	_, _, err := svc.GetSCMProvider(ctx, 99999)
	if err == nil {
		t.Fatal("GetSCMProvider with non-existent repo should return error")
	}
}

// ---------------------------------------------------------------------------
// Delete — additional coverage for webhook cleanup paths
// ---------------------------------------------------------------------------

func TestDelete_NoChildren(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "delete-no-children")

	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := client.RepoConfig.Get(ctx, rc.ID)
	if !ent.IsNotFound(err) {
		t.Error("repo should be deleted")
	}
}

func TestDelete_WithWebhookID(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, _ := pkg.Encrypt("token", encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-wh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	webhookID := "wh-12345"
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("webhook-repo").
		SetFullName("org/webhook-repo").
		SetCloneURL("https://github.com/org/webhook-repo.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SetWebhookID(webhookID).
		SetWebhookSecret("secret123").
		Save(ctx)

	// Delete should attempt webhook cleanup (may fail silently since GitHub API isn't real)
	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete with webhook: %v", err)
	}

	_, err := client.RepoConfig.Get(ctx, rc.ID)
	if !ent.IsNotFound(err) {
		t.Error("repo should be deleted")
	}
}

func TestDelete_WithWebhookDecryptError(t *testing.T) {
	client := testdb.Open(t)
	// Wrong encryption key
	svc := NewService(client, "2222222222222222222222222222222222222222222222222222222222222222", zap.NewNop())
	ctx := context.Background()

	otherKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, _ := pkg.Encrypt("token", otherKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-bad").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	webhookID := "wh-bad"
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("bad-decrypt-repo").
		SetFullName("org/bad-decrypt-repo").
		SetCloneURL("https://github.com/org/bad-decrypt-repo.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SetWebhookID(webhookID).
		SetWebhookSecret("secret").
		Save(ctx)

	// Should still succeed — webhook cleanup failure is non-fatal
	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete with decrypt error: %v", err)
	}

	_, err := client.RepoConfig.Get(ctx, rc.ID)
	if !ent.IsNotFound(err) {
		t.Error("repo should be deleted")
	}
}

func TestDelete_WithEmptyWebhookID(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	p := createSCMProvider(t, client)

	emptyWH := ""
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(p.ID).
		SetName("empty-wh-repo").
		SetFullName("org/empty-wh-repo").
		SetCloneURL("https://github.com/org/empty-wh-repo.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SetWebhookID(emptyWH).
		Save(ctx)

	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete with empty webhook: %v", err)
	}
}

// ---------------------------------------------------------------------------
// List — additional filter coverage
// ---------------------------------------------------------------------------

func TestList_FilterBySCMProviderID(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	p1 := createSCMProvider(t, client)
	p2, _ := client.ScmProvider.Create().
		SetName("another-provider").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials("creds").
		Save(ctx)

	createRepo(t, svc, p1.ID, "p1-repo")
	svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: p2.ID,
		Name:          "p2-repo",
		FullName:      "org/p2-repo",
		CloneURL:      "https://bitbucket.example.com/org/p2-repo.git",
		DefaultBranch: "main",
	})

	repos, total, err := svc.List(ctx, ListOpts{SCMProviderID: p1.ID})
	if err != nil {
		t.Fatalf("List filter by provider: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

func TestList_FilterByProviderScopeAndBindingState(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	github := createSCMProvider(t, client)
	bitbucket, _ := client.ScmProvider.Create().
		SetName("bitbucket-main").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials("encrypted-creds").
		Save(ctx)

	_, err := svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: github.ID,
		Name:          "repo-a",
		FullName:      "org/repo-a",
		CloneURL:      "https://github.com/org/repo-a.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create github org repo: %v", err)
	}
	_, err = svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: github.ID,
		Name:          "repo-b",
		FullName:      "sdk/repo-b",
		CloneURL:      "https://github.com/sdk/repo-b.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create github sdk repo: %v", err)
	}
	_, err = svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: bitbucket.ID,
		Name:          "repo-c",
		FullName:      "org/repo-c",
		CloneURL:      "https://bitbucket.example.com/scm/org/repo-c.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create bitbucket repo: %v", err)
	}
	_, err = svc.CreateDirect(ctx, CreateDirectRequest{
		Name:          "repo-unbound",
		FullName:      "org/repo-unbound",
		CloneURL:      "https://unknown.example.com/org/repo-unbound.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create unbound repo: %v", err)
	}

	repos, total, err := svc.List(ctx, ListOpts{
		Page:          1,
		PageSize:      20,
		SCMProviderID: github.ID,
		Scope:         "org",
		BindingState:  "bound",
	})
	if err != nil {
		t.Fatalf("List provider scope binding filter: %v", err)
	}
	if total != 1 || len(repos) != 1 || repos[0].FullName != "org/repo-a" {
		t.Fatalf("filtered repos = len %d total %d first %q, want only org/repo-a", len(repos), total, repos[0].FullName)
	}

	repos, total, err = svc.List(ctx, ListOpts{
		Page:         1,
		PageSize:     20,
		Scope:        "org",
		BindingState: "unbound",
	})
	if err != nil {
		t.Fatalf("List unbound scope filter: %v", err)
	}
	if total != 1 || len(repos) != 1 || repos[0].FullName != "org/repo-unbound" {
		t.Fatalf("unbound filtered repos = len %d total %d first %q, want only org/repo-unbound", len(repos), total, repos[0].FullName)
	}
}

func TestInventorySummarizesProvidersAndScopes(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	github := createSCMProvider(t, client)
	bitbucket, _ := client.ScmProvider.Create().
		SetName("bitbucket-main").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials("encrypted-creds").
		Save(ctx)

	mustCreate := func(req CreateDirectRequest) *ent.RepoConfig {
		rc, err := svc.CreateDirect(ctx, req)
		if err != nil {
			t.Fatalf("CreateDirect(%s): %v", req.FullName, err)
		}
		return rc
	}
	mustCreate(CreateDirectRequest{SCMProviderID: github.ID, Name: "repo-a", FullName: "org/repo-a", CloneURL: "https://github.com/org/repo-a.git", DefaultBranch: "main"})
	mustCreate(CreateDirectRequest{SCMProviderID: github.ID, Name: "repo-b", FullName: "org/repo-b", CloneURL: "https://github.com/org/repo-b.git", DefaultBranch: "main"})
	mustCreate(CreateDirectRequest{SCMProviderID: github.ID, Name: "mobile-sdk", FullName: "sdk/mobile-sdk", CloneURL: "https://github.com/sdk/mobile-sdk.git", DefaultBranch: "main"})
	failed := mustCreate(CreateDirectRequest{SCMProviderID: bitbucket.ID, Name: "repo-c", FullName: "PROJ/repo-c", CloneURL: "https://bitbucket.example.com/scm/proj/repo-c.git", DefaultBranch: "main"})
	if _, err := svc.Update(ctx, failed.ID, UpdateRequest{Status: "webhook_failed"}); err != nil {
		t.Fatalf("mark webhook failed: %v", err)
	}
	mustCreate(CreateDirectRequest{Name: "repo-unbound", FullName: "org/repo-unbound", CloneURL: "https://unknown.example.com/org/repo-unbound.git", DefaultBranch: "main"})

	inventory, err := svc.Inventory(ctx)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}

	githubSummary := findInventoryProvider(inventory, inventoryProviderKey(github.ID))
	if githubSummary == nil {
		t.Fatalf("github provider summary missing: %#v", inventory)
	}
	if githubSummary.TotalRepos != 3 || githubSummary.BoundRepos != 3 || githubSummary.UnboundRepos != 0 {
		t.Fatalf("github totals = total %d bound %d unbound %d, want 3/3/0", githubSummary.TotalRepos, githubSummary.BoundRepos, githubSummary.UnboundRepos)
	}
	orgScope := findInventoryScope(githubSummary.Scopes, "org")
	if orgScope == nil || orgScope.TotalRepos != 2 || orgScope.BoundRepos != 2 {
		t.Fatalf("github org scope = %#v, want total 2 bound 2", orgScope)
	}

	bitbucketSummary := findInventoryProvider(inventory, inventoryProviderKey(bitbucket.ID))
	if bitbucketSummary == nil {
		t.Fatalf("bitbucket provider summary missing: %#v", inventory)
	}
	projScope := findInventoryScope(bitbucketSummary.Scopes, "PROJ")
	if projScope == nil || projScope.WebhookFailedRepos != 1 {
		t.Fatalf("bitbucket PROJ scope = %#v, want one webhook failure", projScope)
	}

	unboundSummary := findInventoryProvider(inventory, "unbound")
	if unboundSummary == nil {
		t.Fatalf("unbound provider summary missing: %#v", inventory)
	}
	if unboundSummary.TotalRepos != 1 || unboundSummary.UnboundRepos != 1 {
		t.Fatalf("unbound totals = total %d unbound %d, want 1/1", unboundSummary.TotalRepos, unboundSummary.UnboundRepos)
	}
}

func TestInventorySeparatesProvidersWithDuplicateNames(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	firstProvider, _ := client.ScmProvider.Create().
		SetName("Shared Platform").
		SetType("github").
		SetBaseURL("https://github-one.example.com").
		SetCredentials("encrypted-creds").
		Save(ctx)
	secondProvider, _ := client.ScmProvider.Create().
		SetName("Shared Platform").
		SetType("github").
		SetBaseURL("https://github-two.example.com").
		SetCredentials("encrypted-creds").
		Save(ctx)

	if _, err := svc.CreateDirect(ctx, CreateDirectRequest{SCMProviderID: firstProvider.ID, Name: "repo-a", FullName: "alpha/repo-a", CloneURL: "https://github-one.example.com/alpha/repo-a.git", DefaultBranch: "main"}); err != nil {
		t.Fatalf("CreateDirect first provider repo: %v", err)
	}
	if _, err := svc.CreateDirect(ctx, CreateDirectRequest{SCMProviderID: secondProvider.ID, Name: "repo-b", FullName: "beta/repo-b", CloneURL: "https://github-two.example.com/beta/repo-b.git", DefaultBranch: "main"}); err != nil {
		t.Fatalf("CreateDirect second provider repo: %v", err)
	}

	inventory, err := svc.Inventory(ctx)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}

	firstSummary := findInventoryProvider(inventory, inventoryProviderKey(firstProvider.ID))
	if firstSummary == nil {
		t.Fatalf("first provider summary missing: %#v", inventory)
	}
	secondSummary := findInventoryProvider(inventory, inventoryProviderKey(secondProvider.ID))
	if secondSummary == nil {
		t.Fatalf("second provider summary missing: %#v", inventory)
	}
	if firstSummary.Name != "Shared Platform" || firstSummary.ProviderID == nil || *firstSummary.ProviderID != firstProvider.ID || firstSummary.TotalRepos != 1 {
		t.Fatalf("first summary = %#v, want name Shared Platform, provider id %d, one repo", firstSummary, firstProvider.ID)
	}
	if secondSummary.Name != "Shared Platform" || secondSummary.ProviderID == nil || *secondSummary.ProviderID != secondProvider.ID || secondSummary.TotalRepos != 1 {
		t.Fatalf("second summary = %#v, want name Shared Platform, provider id %d, one repo", secondSummary, secondProvider.ID)
	}
}

func findInventoryProvider(items []InventoryProviderSummary, key string) *InventoryProviderSummary {
	for i := range items {
		if items[i].ProviderKey == key {
			return &items[i]
		}
	}
	return nil
}

func findInventoryScope(items []InventoryScopeSummary, scope string) *InventoryScopeSummary {
	for i := range items {
		if items[i].Scope == scope {
			return &items[i]
		}
	}
	return nil
}

func TestList_DefaultPagination(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	createRepo(t, svc, p.ID, "default-page-repo")

	// Page=0 and PageSize=0 should use defaults (1 and 20)
	repos, total, err := svc.List(context.Background(), ListOpts{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("List default pagination: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

func TestList_NegativePagination(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	createRepo(t, svc, p.ID, "neg-page-repo")

	// Negative values should be corrected to defaults
	repos, total, err := svc.List(context.Background(), ListOpts{Page: -1, PageSize: -5})
	if err != nil {
		t.Fatalf("List negative pagination: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

// ---------------------------------------------------------------------------
// CreateDirect — error path
// ---------------------------------------------------------------------------

func TestCreateDirect_InvalidProviderID(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.CreateDirect(context.Background(), CreateDirectRequest{
		SCMProviderID: 99999,
		Name:          "bad-provider-repo",
		FullName:      "org/bad-provider-repo",
		CloneURL:      "https://github.com/org/bad-provider-repo.git",
		DefaultBranch: "main",
	})
	if err == nil {
		t.Fatal("CreateDirect with invalid provider ID should return error")
	}
}

// ---------------------------------------------------------------------------
// Update — multiple fields at once
// ---------------------------------------------------------------------------

func TestUpdate_MultipleFields(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "multi-update-repo")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{
		Name:    "renamed-repo",
		GroupID: "new-group",
		Status:  "inactive",
	})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.Name != "renamed-repo" {
		t.Errorf("Name = %q, want %q", updated.Name, "renamed-repo")
	}
	if updated.GroupID == nil || *updated.GroupID != "new-group" {
		t.Errorf("GroupID = %v, want new-group", updated.GroupID)
	}
	if updated.Status != "inactive" {
		t.Errorf("Status = %q, want inactive", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// parseToken — additional edge cases
// ---------------------------------------------------------------------------

func TestParseToken_JSONWithExtraFields(t *testing.T) {
	got := parseToken(`{"token":"abc123","type":"pat","extra":"ignored"}`)
	if got != "abc123" {
		t.Errorf("parseToken JSON with extra = %q, want %q", got, "abc123")
	}
}

func TestParseToken_JSONEmptyToken(t *testing.T) {
	got := parseToken(`{"token":""}`)
	if got != "" {
		t.Errorf("parseToken JSON empty token = %q, want %q", got, "")
	}
}
