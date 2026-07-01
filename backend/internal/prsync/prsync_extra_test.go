package prsync

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

type recordingUsageRefresher struct {
	ids []int
}

func (r *recordingUsageRefresher) RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error) {
	r.ids = append(r.ids, pr.ID)
	return nil, nil
}

func (r *recordingUsageRefresher) PRIDs() []int {
	return append([]int(nil), r.ids...)
}

// ---------------------------------------------------------------------------
// Sync – cover the upsertPR error warn path (line 53-54)
// ---------------------------------------------------------------------------

func TestSyncUpsertError(t *testing.T) {
	// When upsertPR fails for a PR, Sync should log a warning and continue.
	// We can trigger this by providing a PR with data that causes a DB constraint
	// violation — but that's hard with the current schema. Instead, we use a
	// mock that returns PRs, then close the DB mid-sync.
	//
	// A simpler approach: create a scenario where the query in upsertPR fails.
	// We'll use a context that gets cancelled after the first page fetch.

	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "upsert-err-repo")

	// Create a second client that we'll close to simulate DB errors during upsert
	client2 := testdb.Open(t)
	// Create the same repo structure in client2
	sp2 := client2.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)
	rc2 := client2.RepoConfig.Create().
		SetName("upsert-err-repo").
		SetFullName("org/upsert-err-repo").
		SetCloneURL("https://github.com/org/upsert-err-repo.git").
		SetDefaultBranch("main").
		SetScmProviderID(sp2.ID).
		SaveX(ctx)

	// Close client2 to force upsert errors
	client2.Close()

	svc := NewService(client2, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 1, Title: "PR 1", Author: "alice", SourceBranch: "feat-1", TargetBranch: "main", State: "open", URL: "https://example.com/pr/1"},
				{ID: 2, Title: "PR 2", Author: "bob", SourceBranch: "feat-2", TargetBranch: "main", State: "open", URL: "https://example.com/pr/2"},
			}, nil
		},
	}

	// Sync should not panic — it should log warnings for failed upserts
	result, err := svc.Sync(ctx, mock, rc2)
	if err != nil {
		t.Fatalf("Sync should not return error for upsert failures: %v", err)
	}
	// Both upserts should fail, so created and updated should be 0
	if result.Created != 0 {
		t.Errorf("created = %d, want 0 (all upserts failed)", result.Created)
	}
	if result.Updated != 0 {
		t.Errorf("updated = %d, want 0 (all upserts failed)", result.Updated)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}

	_ = rc // suppress unused
}

// ---------------------------------------------------------------------------
// Sync – cover the labeler error warn path (line 67-69)
// ---------------------------------------------------------------------------

func TestSyncWithLabelerError(t *testing.T) {
	// Use a labeler that will fail because the PR record won't have proper
	// repo config edges loaded. Actually, the labeler queries by ID so it
	// should work. Let's create a scenario where labeling fails.
	//
	// We can create a PR via sync, then have the labeler fail by using a
	// closed secondary client for the labeler.

	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "labeler-err-repo")

	// Create a labeler with a separate closed client to force errors
	client2 := testdb.Open(t)
	client2.Close()
	labeler := efficiency.NewLabeler(client2, nil, logger)

	svc := NewService(client, labeler, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "PR for labeler error", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "open", URL: "https://example.com/pr/1",
					CreatedAt: time.Now(),
				},
			}, nil
		},
	}

	// Sync should succeed even though labeler fails
	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
}

// ---------------------------------------------------------------------------
// upsertPR – cover the query error path (non-NotFound error, line 127-129)
// ---------------------------------------------------------------------------

func TestUpsertPRQueryError(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "query-err-repo")

	svc := NewService(client, nil, logger)

	// Close the client to force query errors
	client.Close()

	pr := &scm.PR{
		ID: 1, Title: "PR", Author: "alice",
		SourceBranch: "feat-1", TargetBranch: "main",
		State: "open", URL: "https://example.com/pr/1",
	}

	_, _, err := svc.upsertPR(ctx, rc.ID, pr)
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

// ---------------------------------------------------------------------------
// upsertPR – cover the update Exec error path (line 154-156)
// ---------------------------------------------------------------------------

func TestUpsertPRUpdateExecError(t *testing.T) {
	// Create a PR, then try to update it with a closed client.
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "update-exec-err-repo")

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

	svc := NewService(client, nil, logger)

	// Close the client to force update errors
	client.Close()

	pr := &scm.PR{
		ID: 1, Title: "Updated Title", Author: "alice",
		SourceBranch: "feat-1", TargetBranch: "main",
		State: "merged", URL: "https://example.com/pr/1",
	}

	_, _, err := svc.upsertPR(ctx, rc.ID, pr)
	if err == nil {
		t.Fatal("expected error when DB is closed during update")
	}
}

// ---------------------------------------------------------------------------
// Sync – cover update path with zero timestamps (update branch, lines 144-153)
// ---------------------------------------------------------------------------

func TestSyncUpdateWithZeroTimestamps(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "update-zero-ts-repo")
	svc := NewService(client, nil, logger)

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

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "Updated no timestamps", Author: "alice",
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
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}

	pr, _ := client.PrRecord.Query().
		Where(prrecord.ScmPrIDEQ(1), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
		Only(ctx)
	if pr.Title != "Updated no timestamps" {
		t.Errorf("title = %q, want %q", pr.Title, "Updated no timestamps")
	}
}

func TestSyncPRs_RefreshesOnlyActivePRs(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "active-refresh-repo")
	refresher := &recordingUsageRefresher{}
	svc := NewService(client, nil, logger, refresher)

	oldClosedAt := time.Now().AddDate(0, -6, 0)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(77).
		SetScmPrURL("https://example.com/pr/77").
		SetTitle("old closed").
		SetAuthor("legacy").
		SetSourceBranch("legacy").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusClosed).
		SetCreatedAt(oldClosedAt).
		SaveX(ctx)

	recentCreatedAt := time.Now().AddDate(0, -1, 0)
	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{ID: 11, Title: "open pr", Author: "alice", SourceBranch: "feat-1", TargetBranch: "main", State: "open", URL: "https://example.com/pr/11", CreatedAt: recentCreatedAt},
				{ID: 14, Title: "recent merged", Author: "bob", SourceBranch: "feat-2", TargetBranch: "main", State: "merged", URL: "https://example.com/pr/14", CreatedAt: recentCreatedAt},
				{ID: 77, Title: "old closed", Author: "legacy", SourceBranch: "legacy", TargetBranch: "main", State: "closed", URL: "https://example.com/pr/77", CreatedAt: oldClosedAt},
			}, nil
		},
	}

	_, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}

	got := refresher.PRIDs()
	want := []int{}
	for _, scmPRID := range []int{11, 14} {
		id, err := client.PrRecord.Query().
			Where(prrecord.ScmPrIDEQ(scmPRID), prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
			OnlyID(ctx)
		if err != nil {
			t.Fatalf("load PR id for scm_pr_id=%d: %v", scmPRID, err)
		}
		want = append(want, id)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refreshed PR IDs = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// fetchAllPRs – cover the error path (line 107-109)
// ---------------------------------------------------------------------------

func TestFetchAllPRsError(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	logger := zap.NewNop()

	svc := NewService(client, nil, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return nil, fmt.Errorf("SCM API error")
		},
	}

	_, err := svc.fetchAllPRs(context.Background(), mock, "org/repo")
	if err == nil {
		t.Fatal("expected error from fetchAllPRs")
	}
}

// ---------------------------------------------------------------------------
// fetchAllPRs – cover pagination error on second page
// ---------------------------------------------------------------------------

func TestFetchAllPRsPaginationError(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	logger := zap.NewNop()

	svc := NewService(client, nil, logger)

	callCount := 0
	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			callCount++
			if callCount == 1 {
				// Return exactly pageSize to trigger next page
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
			// Second page fails
			return nil, fmt.Errorf("page 2 error")
		},
	}

	_, err := svc.fetchAllPRs(context.Background(), mock, "org/repo")
	if err == nil {
		t.Fatal("expected error from fetchAllPRs on second page")
	}
}

// ---------------------------------------------------------------------------
// upsertPR – cover create error path (line 183-185)
// ---------------------------------------------------------------------------

func TestUpsertPRCreateError(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()

	svc := NewService(client, nil, logger)

	// Use a non-existent repoConfigID to trigger FK constraint error
	pr := &scm.PR{
		ID: 1, Title: "PR", Author: "alice",
		SourceBranch: "feat-1", TargetBranch: "main",
		State: "open", URL: "https://example.com/pr/1",
	}

	_, _, err := svc.upsertPR(ctx, 99999, pr)
	if err == nil {
		t.Fatal("expected FK constraint error for non-existent repo config ID")
	}
}

// ---------------------------------------------------------------------------
// NewService – verify all fields are set
// ---------------------------------------------------------------------------

func TestNewServiceAllFields(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	logger := zap.NewNop()

	labeler := efficiency.NewLabeler(client, nil, logger)

	svc := NewService(client, labeler, logger)
	if svc.entClient != client {
		t.Error("entClient not set")
	}
	if svc.labeler != labeler {
		t.Error("labeler not set")
	}
	if svc.logger != logger {
		t.Error("logger not set")
	}
}

func TestSyncWithLabelerErrors(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "both-err-repo")

	closedClient := testdb.Open(t)
	closedClient.Close()

	labeler := efficiency.NewLabeler(closedClient, nil, logger)

	svc := NewService(client, labeler, logger)

	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			return []*scm.PR{
				{
					ID: 1, Title: "PR", Author: "alice",
					SourceBranch: "feat-1", TargetBranch: "main",
					State: "open", URL: "https://example.com/pr/1",
					CreatedAt: time.Now(),
				},
			}, nil
		},
	}

	result, err := svc.Sync(ctx, mock, rc)
	if err != nil {
		t.Fatalf("Sync should succeed despite labeler errors: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
}
