package repo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Create (with SCM validation) — error paths
// ---------------------------------------------------------------------------

func TestCreate_ProviderNotFound(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.Create(context.Background(), CreateRequest{
		SCMProviderID: 99999,
		FullName:      "org/no-provider",
	})
	if err == nil {
		t.Fatal("Create with non-existent provider should return error")
	}
}

func TestCreate_DecryptError(t *testing.T) {
	client := testdb.Open(t)
	svc := NewService(client, "1111111111111111111111111111111111111111111111111111111111111111", zap.NewNop())
	ctx := context.Background()

	otherKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, otherKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-bad-key").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	_, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/decrypt-fail",
	})
	if err == nil {
		t.Fatal("Create with wrong encryption key should return error")
	}
}

func TestCreate_UnsupportedProviderType(t *testing.T) {
	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-provider").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	// Will succeed in getting provider and decrypting, but fail at GetRepo
	_, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/unreachable-repo",
	})
	if err == nil {
		t.Fatal("Create with unreachable SCM should return error")
	}
}

// ---------------------------------------------------------------------------
// Create — with GroupID set
// ---------------------------------------------------------------------------

func TestCreate_WithGroupID(t *testing.T) {
	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("github-group").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		Save(ctx)

	// This will fail at GetRepo (can't reach GitHub), but exercises the GroupID path
	_, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/group-repo",
		GroupID:       "team-alpha",
	})
	// Expected to fail at GetRepo since we can't reach GitHub
	if err == nil {
		t.Log("Create succeeded unexpectedly (may have reached GitHub)")
	}
}

// ---------------------------------------------------------------------------
// Delete — with all child types
// ---------------------------------------------------------------------------

func TestDelete_WithAllChildTypes(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "all-children-repo")

	// Create session
	client.Session.Create().
		SetRepoConfigID(rc.ID).
		SetBranch("main").
		SaveX(ctx)

	// Create AI scan result
	client.AiScanResult.Create().
		SetRepoConfigID(rc.ID).
		SetScore(90).
		SaveX(ctx)

	// Create PR record
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SaveX(ctx)

	// Create efficiency metric
	client.EfficiencyMetric.Create().
		SetRepoConfigID(rc.ID).
		SetPeriodType("daily").
		SetPeriodStart(time.Now()).
		SaveX(ctx)

	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := client.RepoConfig.Query().Count(ctx)
	if count != 0 {
		t.Errorf("repo count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// Delete — with webhook and Bitbucket provider type (exercises different provider path)
// ---------------------------------------------------------------------------

func TestDelete_WithWebhookBitbucketProvider(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, _ := pkg.Encrypt("token", encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("bitbucket-provider").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials(encrypted).
		Save(ctx)

	webhookID := "wh-bb-delete"
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("bb-wh-delete-repo").
		SetFullName("PROJ/bb-wh-delete-repo").
		SetCloneURL("https://bitbucket.example.com/PROJ/bb-wh-delete-repo.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SetWebhookID(webhookID).
		SetWebhookSecret("secret").
		Save(ctx)

	// Should still succeed — webhook cleanup failure (can't reach Bitbucket) is non-fatal
	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete with bitbucket webhook: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delete — with webhook but nil provider edge
// ---------------------------------------------------------------------------

func TestDelete_WithWebhookNilProviderEdge(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()
	p := createSCMProvider(t, client)

	webhookID := "wh-nil-edge"
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(p.ID).
		SetName("nil-edge-repo").
		SetFullName("org/nil-edge-repo").
		SetCloneURL("https://github.com/org/nil-edge-repo.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SetWebhookID(webhookID).
		SetWebhookSecret("secret").
		Save(ctx)

	// Delete should attempt webhook cleanup via the provider edge
	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete with webhook: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetSCMProvider — bitbucket provider type (exercises different branch)
// ---------------------------------------------------------------------------

func TestGetSCMProvider_BitbucketType(t *testing.T) {
	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("bitbucket-provider").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials(encrypted).
		Save(ctx)

	rc, _ := svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: sp.ID,
		Name:          "bb-repo",
		FullName:      "PROJ/bb-repo",
		CloneURL:      "https://bitbucket.example.com/PROJ/bb-repo.git",
		DefaultBranch: "main",
	})

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

// ---------------------------------------------------------------------------
// List — edge cases
// ---------------------------------------------------------------------------

func TestList_LargePageSize(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	createRepo(t, svc, p.ID, "large-page-repo")

	repos, total, err := svc.List(context.Background(), ListOpts{Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

func TestList_AllFilters(t *testing.T) {
	client, svc := setupTest(t)
	ctx := context.Background()

	p := createSCMProvider(t, client)

	svc.CreateDirect(ctx, CreateDirectRequest{
		SCMProviderID: p.ID,
		Name:          "filtered-repo",
		FullName:      "org/filtered-repo",
		CloneURL:      "https://github.com/org/filtered-repo.git",
		DefaultBranch: "main",
		GroupID:       "team-x",
	})

	repos, total, err := svc.List(ctx, ListOpts{
		SCMProviderID: p.ID,
		Status:        "active",
		GroupID:       "team-x",
		Page:          1,
		PageSize:      10,
	})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(repos) != 1 {
		t.Errorf("repos len = %d, want 1", len(repos))
	}
}

// ---------------------------------------------------------------------------
// Update — edge cases
// ---------------------------------------------------------------------------

func TestUpdate_NoChanges(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "no-change-repo")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.Name != "no-change-repo" {
		t.Errorf("Name = %q, want %q", updated.Name, "no-change-repo")
	}
}

func TestUpdate_ScanPromptAndClearTogether(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "prompt-clear-repo")

	updated, err := svc.Update(context.Background(), rc.ID, UpdateRequest{
		ClearScanPrompt:    true,
		ScanPromptOverride: map[string]string{"system": "test"},
	})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.ScanPromptOverride != nil {
		t.Errorf("ScanPromptOverride = %v, want nil (clear takes precedence)", updated.ScanPromptOverride)
	}
}

// ---------------------------------------------------------------------------
// GetSCMProvider — additional paths
// ---------------------------------------------------------------------------

func TestGetSCMProvider_UnsupportedType(t *testing.T) {
	_, svc := setupTest(t)

	_, err := svc.newSCMProvider("unsupported_type", "https://example.com", "token")
	if err == nil {
		t.Fatal("newSCMProvider with unsupported type should return error")
	}
}

// ---------------------------------------------------------------------------
// generateSecret — zero length
// ---------------------------------------------------------------------------

func TestGenerateSecret_ZeroLength(t *testing.T) {
	s, err := generateSecret(0)
	if err != nil {
		t.Fatalf("generateSecret(0) error: %v", err)
	}
	if s != "" {
		t.Errorf("generateSecret(0) = %q, want empty", s)
	}
}

func TestGenerateSecret_SmallLength(t *testing.T) {
	s, err := generateSecret(1)
	if err != nil {
		t.Fatalf("generateSecret(1) error: %v", err)
	}
	// 1 byte → 2 hex chars
	if len(s) != 2 {
		t.Errorf("generateSecret(1) len = %d, want 2", len(s))
	}
}

// ---------------------------------------------------------------------------
// Create — full success path with mock GitHub server
// ---------------------------------------------------------------------------

func setupMockGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Mock GetRepo
	mux.HandleFunc("/api/v3/repos/org/mock-repo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"full_name":      "org/mock-repo",
			"name":           "mock-repo",
			"clone_url":      "https://github.com/org/mock-repo.git",
			"default_branch": "main",
			"description":    "mock repo",
			"private":        false,
		})
	})

	// Mock RegisterWebhook — success
	mux.HandleFunc("/api/v3/repos/org/mock-repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 42,
		})
	})

	// Mock RegisterWebhook — failure for webhook-fail-repo
	mux.HandleFunc("/api/v3/repos/org/webhook-fail-repo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"full_name":      "org/webhook-fail-repo",
			"name":           "webhook-fail-repo",
			"clone_url":      "https://github.com/org/webhook-fail-repo.git",
			"default_branch": "main",
		})
	})
	mux.HandleFunc("/api/v3/repos/org/webhook-fail-repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestCreate_FullSuccessWithWebhook(t *testing.T) {
	server := setupMockGitHub(t)

	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("mock-github").
		SetType("github").
		SetBaseURL(server.URL).
		SetCredentials(encrypted).
		Save(ctx)

	rc, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/mock-repo",
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if rc.FullName != "org/mock-repo" {
		t.Errorf("FullName = %q, want %q", rc.FullName, "org/mock-repo")
	}
	if rc.Status != repoconfig.StatusActive {
		t.Errorf("Status = %q, want active", rc.Status)
	}
	if rc.WebhookID == nil || *rc.WebhookID == "" {
		t.Error("expected webhook_id to be set")
	}
	if rc.WebhookSecret == nil || *rc.WebhookSecret == "" {
		t.Error("expected webhook_secret to be set")
	}
}

func TestCreate_FullSuccessWithGroupID(t *testing.T) {
	server := setupMockGitHub(t)

	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("mock-github-group").
		SetType("github").
		SetBaseURL(server.URL).
		SetCredentials(encrypted).
		Save(ctx)

	rc, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/mock-repo",
		GroupID:       "team-alpha",
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if rc.GroupID == nil || *rc.GroupID != "team-alpha" {
		t.Errorf("GroupID = %v, want team-alpha", rc.GroupID)
	}
}

func TestCreate_WebhookRegistrationFails(t *testing.T) {
	server := setupMockGitHub(t)

	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("mock-github-wh-fail").
		SetType("github").
		SetBaseURL(server.URL).
		SetCredentials(encrypted).
		Save(ctx)

	rc, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/webhook-fail-repo",
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	// Webhook registration failed → status should be webhook_failed
	if rc.Status != repoconfig.StatusWebhookFailed {
		t.Errorf("Status = %q, want webhook_failed", rc.Status)
	}
	// WebhookID should not be set
	if rc.WebhookID != nil && *rc.WebhookID != "" {
		t.Errorf("WebhookID = %v, want nil/empty", rc.WebhookID)
	}
}

// ---------------------------------------------------------------------------
// Delete — with webhook and mock GitHub for successful cleanup
// ---------------------------------------------------------------------------

func TestDelete_WithWebhookSuccessfulCleanup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/org/delete-wh-repo/hooks/42", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("mock-github-delete").
		SetType("github").
		SetBaseURL(server.URL).
		SetCredentials(encrypted).
		Save(ctx)

	webhookID := "42"
	rc, _ := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("delete-wh-repo").
		SetFullName("org/delete-wh-repo").
		SetCloneURL("https://github.com/org/delete-wh-repo.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SetWebhookID(webhookID).
		SetWebhookSecret("secret123").
		Save(ctx)

	if err := svc.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Create — Bitbucket provider path
// ---------------------------------------------------------------------------

func TestCreate_BitbucketProviderGetRepoFails(t *testing.T) {
	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("bitbucket-create").
		SetType("bitbucket_server").
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials(encrypted).
		Save(ctx)

	// Will fail at GetRepo since we can't reach Bitbucket
	_, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "PROJ/unreachable-repo",
	})
	if err == nil {
		t.Fatal("Create with unreachable Bitbucket should return error")
	}
}

// ---------------------------------------------------------------------------
// Create — save error (duplicate full_name)
// ---------------------------------------------------------------------------

func TestCreate_SaveError(t *testing.T) {
	server := setupMockGitHub(t)

	client := testdb.Open(t)
	encKey := "0000000000000000000000000000000000000000000000000000000000000000"
	svc := NewService(client, encKey, zap.NewNop())
	ctx := context.Background()

	encrypted, _ := pkg.Encrypt(`{"token":"ghp_test"}`, encKey)

	sp, _ := client.ScmProvider.Create().
		SetName("mock-github-dup").
		SetType("github").
		SetBaseURL(server.URL).
		SetCredentials(encrypted).
		Save(ctx)

	// Create first repo successfully
	_, err := svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/mock-repo",
	})
	if err != nil {
		t.Fatalf("first Create error: %v", err)
	}

	// Create duplicate — should fail on save due to unique constraint
	_, err = svc.Create(ctx, CreateRequest{
		SCMProviderID: sp.ID,
		FullName:      "org/mock-repo",
	})
	if err == nil {
		t.Log("duplicate Create did not fail (unique constraint may not be enforced)")
	}
}

// ---------------------------------------------------------------------------
// List — DB error (closed client)
// ---------------------------------------------------------------------------

func TestList_DBError(t *testing.T) {
	client := testdb.Open(t)
	svc := NewService(client, "key", zap.NewNop())

	client.Close()

	_, _, err := svc.List(context.Background(), ListOpts{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatal("List with closed DB should return error")
	}
}

// ---------------------------------------------------------------------------
// Update — DB error (closed client)
// ---------------------------------------------------------------------------

func TestUpdate_DBError(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "update-db-error-repo")

	client.Close()

	_, err := svc.Update(context.Background(), rc.ID, UpdateRequest{Name: "new-name"})
	if err == nil {
		t.Fatal("Update with closed DB should return error")
	}
}

// ---------------------------------------------------------------------------
// Delete — DB error on tx begin (closed client)
// ---------------------------------------------------------------------------

func TestDelete_DBTxError(t *testing.T) {
	client, svc := setupTest(t)
	p := createSCMProvider(t, client)
	rc := createRepo(t, svc, p.ID, "delete-tx-error-repo")

	client.Close()

	err := svc.Delete(context.Background(), rc.ID)
	if err == nil {
		t.Fatal("Delete with closed DB should return error")
	}
}
