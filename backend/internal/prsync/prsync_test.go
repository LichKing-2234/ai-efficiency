package prsync

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/scm"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// mockSCMProvider implements scm.SCMProvider for testing.
type mockSCMProvider struct {
	listPRsFunc func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error)
}

func (m *mockSCMProvider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	if m.listPRsFunc != nil {
		return m.listPRsFunc(ctx, repoFullName, opts)
	}
	return nil, nil
}
func (m *mockSCMProvider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	return 0, nil
}
func (m *mockSCMProvider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	return nil
}
func (m *mockSCMProvider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	return nil
}
func (m *mockSCMProvider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	return nil
}
func (m *mockSCMProvider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	return nil
}
func (m *mockSCMProvider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	return nil
}
func (m *mockSCMProvider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (string, error) {
	return "", nil
}

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	return enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
}

func createTestRepo(t *testing.T, ctx context.Context, client *ent.Client, name string) *ent.RepoConfig {
	t.Helper()
	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	return client.RepoConfig.Create().
		SetName(name).
		SetFullName("org/" + name).
		SetCloneURL("https://github.com/org/" + name + ".git").
		SetDefaultBranch("main").
		SetScmProviderID(sp.ID).
		SaveX(ctx)
}

// --- NewService ---

func TestNewService(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	logger := zap.NewNop()

	svc := NewService(client, nil, nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil Service")
	}
	if svc.entClient != client {
		t.Error("entClient not set")
	}
}

// --- mapPRStatus ---

func TestMapPRStatusMerged(t *testing.T) {
	if got := mapPRStatus("merged"); got != prrecord.StatusMerged {
		t.Errorf("got %v, want StatusMerged", got)
	}
}

func TestMapPRStatusClosed(t *testing.T) {
	if got := mapPRStatus("closed"); got != prrecord.StatusClosed {
		t.Errorf("got %v, want StatusClosed", got)
	}
}

func TestMapPRStatusOpen(t *testing.T) {
	if got := mapPRStatus("open"); got != prrecord.StatusOpen {
		t.Errorf("got %v, want StatusOpen", got)
	}
}

func TestMapPRStatusDefault(t *testing.T) {
	if got := mapPRStatus("unknown"); got != prrecord.StatusOpen {
		t.Errorf("got %v, want StatusOpen for unknown state", got)
	}
}

func TestMapPRStatusEmpty(t *testing.T) {
	if got := mapPRStatus(""); got != prrecord.StatusOpen {
		t.Errorf("got %v, want StatusOpen for empty state", got)
	}
}

// --- Sync ---

func TestSyncEmptyPRList(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "empty-repo")
	svc := NewService(client, nil, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return nil, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("total = %d, want 0", result.Total)
	}
	if result.Created != 0 {
		t.Errorf("created = %d, want 0", result.Created)
	}
	if result.Updated != 0 {
		t.Errorf("updated = %d, want 0", result.Updated)
	}
}

func TestSyncCreateNewPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "new-prs-repo")
	svc := NewService(client, nil, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 1, Title: "PR 1", Author: "alice", SourceBranch: "feat-1", TargetBranch: "main", State: "open", URL: "https://example.com/pr/1"},
				{ID: 2, Title: "PR 2", Author: "bob", SourceBranch: "feat-2", TargetBranch: "main", State: "merged", URL: "https://example.com/pr/2"},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Created != 2 {
		t.Errorf("created = %d, want 2", result.Created)
	}
	if result.Updated != 0 {
		t.Errorf("updated = %d, want 0", result.Updated)
	}

	// Verify records in DB
	count, _ := client.PrRecord.Query().
		Where(prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Count(ctx)
	if count != 2 {
		t.Errorf("DB count = %d, want 2", count)
	}
}

func TestSyncUpdateExistingPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "update-prs-repo")
	svc := NewService(client, nil, nil, logger)

	// Pre-create a PR record
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SetScmPrURL("https://example.com/pr/1").
		SetTitle("Old Title").
		SetAuthor("alice").
		SetSourceBranch("feat-1").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 1, Title: "Updated Title", Author: "alice", SourceBranch: "feat-1", TargetBranch: "main", State: "merged", URL: "https://example.com/pr/1"},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 0 {
		t.Errorf("created = %d, want 0", result.Created)
	}
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}

	// Verify the record was updated
	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Only(ctx)
	if pr.Title != "Updated Title" {
		t.Errorf("title = %q, want %q", pr.Title, "Updated Title")
	}
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %v, want StatusMerged", pr.Status)
	}
}

func TestSyncWithMergedAtAndLabels(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "labels-repo")
	svc := NewService(client, nil, nil, logger)

	mergedAt := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "PR with labels", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "merged", URL: "https://example.com/pr/1",
					Labels: []string{"ai-assisted", "feature"},
					CreatedAt: createdAt, MergedAt: mergedAt,
					LinesAdded: 50, LinesDeleted: 10,
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}

	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Only(ctx)
	if pr.LinesAdded != 50 {
		t.Errorf("lines_added = %d, want 50", pr.LinesAdded)
	}
	if pr.LinesDeleted != 10 {
		t.Errorf("lines_deleted = %d, want 10", pr.LinesDeleted)
	}
}

func TestSyncFetchError(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "error-repo")
	svc := NewService(client, nil, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return nil, context.DeadlineExceeded
		},
	}

	_, err := svc.Sync(ctx, mock, rc)
	if err == nil {
		t.Fatal("expected error from Sync")
	}
}

func TestSyncPagination(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "paginated-repo")
	svc := NewService(client, nil, nil, logger)

	callCount := 0
	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			callCount++
			if callCount == 1 {
				// Return exactly pageSize (100) to trigger next page
				prs := make([]*scm.PR, 100)
				for i := 0; i < 100; i++ {
					prs[i] = &scm.PR{
						ID: i + 1, Title: "PR", Author: "user",
						SourceBranch: "feat", TargetBranch: "main",
						State: "open", URL: "https://example.com/pr",
					}
				}
				return prs, nil
			}
			// Second page: fewer than pageSize
			return []*scm.PR{
				{ID: 101, Title: "PR 101", Author: "user", SourceBranch: "feat", TargetBranch: "main", State: "open", URL: "https://example.com/pr/101"},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Total != 101 {
		t.Errorf("total = %d, want 101", result.Total)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (pagination)", callCount)
	}
}

func TestSyncNilLabelerAndAggregator(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "nil-deps-repo")
	svc := NewService(client, nil, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 1, Title: "PR", Author: "user", SourceBranch: "feat", TargetBranch: "main", State: "open", URL: "https://example.com/pr/1"},
			}, nil
		},
	}

	// Should not panic with nil labeler and aggregator
	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
}

func TestSyncMixedCreateAndUpdate(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "mixed-repo")
	svc := NewService(client, nil, nil, logger)

	// Pre-create PR #1
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SetScmPrURL("https://example.com/pr/1").
		SetTitle("Existing PR").
		SetAuthor("alice").
		SetSourceBranch("feat-1").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 1, Title: "Updated PR", Author: "alice", SourceBranch: "feat-1", TargetBranch: "main", State: "merged", URL: "https://example.com/pr/1"},
				{ID: 2, Title: "New PR", Author: "bob", SourceBranch: "feat-2", TargetBranch: "main", State: "open", URL: "https://example.com/pr/2"},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}
}

func TestSyncWithLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "labeler-repo")

	labeler := efficiency.NewLabeler(client, nil, logger)
	svc := NewService(client, labeler, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "PR with labeler", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "open", URL: "https://example.com/pr/1",
					CreatedAt: time.Now(),
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}

	// Verify the labeler ran — PR should have ai_label set
	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Only(ctx)
	if pr.AiLabel == "" {
		t.Error("expected ai_label to be set after labeler ran")
	}
}

func TestSyncWithAggregator(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "aggregator-repo")

	aggregator := efficiency.NewAggregator(client, logger)
	svc := NewService(client, nil, aggregator, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "PR for aggregation", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "merged", URL: "https://example.com/pr/1",
					CreatedAt: time.Now(),
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}

	// Verify aggregator ran — should have efficiency metrics
	count, _ := client.EfficiencyMetric.Query().Count(ctx)
	if count == 0 {
		t.Error("expected efficiency metrics to be created after aggregation")
	}
}

func TestSyncWithLabelerAndAggregator(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "full-sync-repo")

	labeler := efficiency.NewLabeler(client, nil, logger)
	aggregator := efficiency.NewAggregator(client, logger)
	svc := NewService(client, labeler, aggregator, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "Full sync PR", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "open", URL: "https://example.com/pr/1",
					CreatedAt: time.Now(),
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
}

func TestSyncUpdateWithMergedAtAndLabels(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "update-labels-repo")
	svc := NewService(client, nil, nil, logger)

	// Pre-create a PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SetScmPrURL("https://example.com/pr/1").
		SetTitle("Old Title").
		SetAuthor("alice").
		SetSourceBranch("feat-1").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	mergedAt := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "Updated with labels", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "merged", URL: "https://example.com/pr/1",
					Labels:    []string{"ai-assisted", "feature"},
					CreatedAt: createdAt, MergedAt: mergedAt,
					LinesAdded: 100, LinesDeleted: 20,
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}

	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Only(ctx)
	if pr.Title != "Updated with labels" {
		t.Errorf("title = %q, want %q", pr.Title, "Updated with labels")
	}
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %v, want merged", pr.Status)
	}
	if pr.LinesAdded != 100 {
		t.Errorf("lines_added = %d, want 100", pr.LinesAdded)
	}
	if pr.LinesDeleted != 20 {
		t.Errorf("lines_deleted = %d, want 20", pr.LinesDeleted)
	}
}

func TestSyncCreateWithZeroTimestamps(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "zero-ts-repo")
	svc := NewService(client, nil, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "No timestamps", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "open", URL: "https://example.com/pr/1",
					// CreatedAt and MergedAt are zero values
					// Labels is empty
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
}

func TestSyncCreateWithAllFields(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "all-fields-repo")
	svc := NewService(client, nil, nil, logger)

	createdAt := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "All fields PR", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "merged", URL: "https://example.com/pr/1",
					Labels:       []string{"bug", "priority"},
					CreatedAt:    createdAt,
					MergedAt:     mergedAt,
					LinesAdded:   200,
					LinesDeleted: 50,
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}

	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1)).
		Only(ctx)
	if pr.LinesAdded != 200 {
		t.Errorf("lines_added = %d, want 200", pr.LinesAdded)
	}
	if pr.MergedAt == nil {
		t.Error("expected merged_at to be set")
	}
}
