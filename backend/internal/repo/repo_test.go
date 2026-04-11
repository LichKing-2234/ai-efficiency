package repo

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

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
	p, err := newGitHubProvider("https://api.github.com", "test-token", zap.NewNop())
	if err != nil {
		t.Fatalf("newGitHubProvider error: %v", err)
	}
	if p == nil {
		t.Fatal("newGitHubProvider returned nil")
	}
}

func TestNewBitbucketProvider(t *testing.T) {
	p, err := newBitbucketProvider("https://bitbucket.example.com", "test-token", zap.NewNop())
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

func TestUpdate_ScanPromptOverride(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "prompt-repo")

	prompt := map[string]string{"system": "You are a code reviewer."}
	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{ScanPromptOverride: prompt})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.ScanPromptOverride == nil {
		t.Fatal("ScanPromptOverride should not be nil")
	}
	if updated.ScanPromptOverride["system"] != "You are a code reviewer." {
		t.Errorf("ScanPromptOverride[system] = %q", updated.ScanPromptOverride["system"])
	}
}

func TestUpdate_ClearScanPrompt(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "clear-prompt-repo")

	// Set a prompt first
	prompt := map[string]string{"system": "prompt"}
	_, err := svc.Update(context.Background(), rc.ID, UpdateRequest{ScanPromptOverride: prompt})
	if err != nil {
		t.Fatalf("set prompt: %v", err)
	}

	// Clear it
	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{ClearScanPrompt: true})
	if err != nil {
		t.Fatalf("clear prompt: %v", err)
	}
	if updated.ScanPromptOverride != nil {
		t.Errorf("ScanPromptOverride = %v, want nil", updated.ScanPromptOverride)
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

	// Create child: session
	_, err := client.Session.Create().
		SetRepoConfigID(rc.ID).
		SetBranch("feature-x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Create child: AI scan result
	_, err = client.AiScanResult.Create().
		SetRepoConfigID(rc.ID).
		SetScore(85).
		Save(ctx)
	if err != nil {
		t.Fatalf("create scan result: %v", err)
	}

	// Create child: PR record
	_, err = client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(42).
		Save(ctx)
	if err != nil {
		t.Fatalf("create pr record: %v", err)
	}

	// Create child: efficiency metric
	_, err = client.EfficiencyMetric.Create().
		SetRepoConfigID(rc.ID).
		SetPeriodType("daily").
		SetPeriodStart(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create efficiency metric: %v", err)
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
	sessions, _ := client.Session.Query().All(ctx)
	if len(sessions) != 0 {
		t.Errorf("sessions count = %d, want 0", len(sessions))
	}
	scans, _ := client.AiScanResult.Query().All(ctx)
	if len(scans) != 0 {
		t.Errorf("scan results count = %d, want 0", len(scans))
	}
	prs, _ := client.PrRecord.Query().All(ctx)
	if len(prs) != 0 {
		t.Errorf("pr records count = %d, want 0", len(prs))
	}
	metrics, _ := client.EfficiencyMetric.Query().All(ctx)
	if len(metrics) != 0 {
		t.Errorf("efficiency metrics count = %d, want 0", len(metrics))
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
// TriggerScan
// ---------------------------------------------------------------------------

func TestTriggerScan(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "scan-repo")

	// Initially nil
	if rc.LastScanAt != nil {
		t.Errorf("LastScanAt should be nil initially, got %v", rc.LastScanAt)
	}

	before := time.Now().Add(-time.Second)
	if err := svc.TriggerScan(context.Background(), rc.ID); err != nil {
		t.Fatalf("TriggerScan error: %v", err)
	}

	// Re-fetch and verify
	updated, err := svc.Get(context.Background(), rc.ID)
	if err != nil {
		t.Fatalf("Get after scan: %v", err)
	}
	if updated.LastScanAt == nil {
		t.Fatal("LastScanAt should not be nil after TriggerScan")
	}
	if updated.LastScanAt.Before(before) {
		t.Errorf("LastScanAt = %v, should be after %v", updated.LastScanAt, before)
	}
}

func TestTriggerScan_NotFound(t *testing.T) {
	_, svc := setupTest(t)

	err := svc.TriggerScan(context.Background(), 99999)
	if err == nil {
		t.Fatal("TriggerScan non-existent should return error")
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
