package prusage

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeSCMProvider struct {
	listPRCommitsFn func(ctx context.Context, repoFullName string, prID int) ([]string, error)
}

func (f *fakeSCMProvider) GetRepo(context.Context, string) (*scm.Repo, error) { return nil, nil }
func (f *fakeSCMProvider) ListRepos(context.Context, scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (f *fakeSCMProvider) CreatePR(context.Context, scm.CreatePRRequest) (*scm.PR, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetPR(context.Context, string, int) (*scm.PR, error) { return nil, nil }
func (f *fakeSCMProvider) ListPRs(context.Context, string, scm.PRListOpts) ([]*scm.PR, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetPRChangedFiles(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (f *fakeSCMProvider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	if f.listPRCommitsFn == nil {
		return nil, nil
	}
	return f.listPRCommitsFn(ctx, repoFullName, prID)
}
func (f *fakeSCMProvider) GetPRApprovals(context.Context, string, int) (int, error)  { return 0, nil }
func (f *fakeSCMProvider) AddLabels(context.Context, string, int, []string) error    { return nil }
func (f *fakeSCMProvider) SetPRStatus(context.Context, scm.SetStatusRequest) error   { return nil }
func (f *fakeSCMProvider) MergePR(context.Context, string, int, scm.MergeOpts) error { return nil }
func (f *fakeSCMProvider) RegisterWebhook(context.Context, string, []string, string) (string, error) {
	return "", nil
}
func (f *fakeSCMProvider) DeleteWebhook(context.Context, string, string) error { return nil }
func (f *fakeSCMProvider) ParseWebhookPayload(*http.Request, string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetFileContent(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetTree(context.Context, string, string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetBranchSHA(context.Context, string, string) (string, error) {
	return "", nil
}
func (f *fakeSCMProvider) CreateBranch(context.Context, string, string, string) error { return nil }
func (f *fakeSCMProvider) CommitFiles(context.Context, scm.CommitFilesRequest) (string, error) {
	return "", nil
}

func newTestRepoPR(t *testing.T) (*ent.Client, *ent.RepoConfig, *ent.PrRecord, int) {
	t.Helper()
	client := testdb.Open(t)
	ctx := context.Background()

	scmRecord := client.ScmProvider.Create().
		SetName("gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	repo := client.RepoConfig.Create().
		SetScmProviderID(scmRecord.ID).
		SetName("repo").
		SetFullName("org/repo").
		SetCloneURL("https://github.com/org/repo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	pr := client.PrRecord.Create().
		SetRepoConfigID(repo.ID).
		SetScmPrID(88).
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	userID := client.User.Create().
		SetUsername("usage-user").
		SetEmail("usage-user@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	return client, repo, pr, userID
}

func TestRefreshPRUsage_WritesCommitSnapshotsAndSummary(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()

	t1 := time.Now().Add(-30 * time.Minute).UTC()
	t2 := time.Now().Add(-20 * time.Minute).UTC()

	cp1 := client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetUserID(userID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("abc123").
		SetParentShas([]string{"base"}).
		SetCapturedAt(t1).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("codex-1").
		SetToolEventID("resp-1").
		SetObservedStartAt(t1).
		SetObservedEndAt(t1.Add(time.Minute)).
		SetUsageUnit("token").
		SetInputTokens(11).
		SetOutputTokens(7).
		SetCachedInputTokens(3).
		SetReasoningTokens(2).
		SetRequestCount(2).
		SetDedupeKey("codex:usage:1").
		SetCommitCheckpointID(cp1.ID).
		SaveX(ctx)

	cp2 := client.CommitCheckpoint.Create().
		SetEventID("cp-2").
		SetUserID(userID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("def456").
		SetParentShas([]string{"abc123"}).
		SetCapturedAt(t2).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("kiro-1").
		SetToolEventID("turn-1").
		SetObservedStartAt(t2).
		SetObservedEndAt(t2.Add(time.Minute)).
		SetUsageUnit("credit").
		SetCreditUsage(1.5).
		SetRequestCount(1).
		SetDedupeKey("kiro:usage:1").
		SetCommitCheckpointID(cp2.ID).
		SaveX(ctx)

	svc := NewService(client)
	result, err := svc.RefreshPR(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"abc123", "def456"}, nil
		},
	}, pr)
	if err != nil {
		t.Fatalf("RefreshPR error: %v", err)
	}
	if result.Summary.InputTokens != 11 || result.Summary.OutputTokens != 7 {
		t.Fatalf("summary = %+v, want input=11 output=7", result.Summary)
	}
	if result.Summary.CreditUsage != 1.5 || result.Summary.RequestCount != 3 {
		t.Fatalf("summary = %+v, want credit=1.5 request_count=3", result.Summary)
	}

	rows := client.PRCommitUsageSnapshot.Query().AllX(ctx)
	if len(rows) != 2 {
		t.Fatalf("commit snapshots = %d, want 2", len(rows))
	}
}

func TestRefreshPRUsage_UsesCommitRewrites(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()

	t1 := time.Now().Add(-30 * time.Minute).UTC()
	cp := client.CommitCheckpoint.Create().
		SetEventID("cp-old").
		SetUserID(userID).
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("old-sha").
		SetParentShas([]string{"base"}).
		SetCapturedAt(t1).
		SaveX(ctx)
	client.CommitRewrite.Create().
		SetEventID("rw-1").
		SetUserID(userID).
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitrewrite.BindingSourceMarker).
		SetRewriteType(commitrewrite.RewriteTypeAmend).
		SetOldCommitSha("old-sha").
		SetNewCommitSha("new-sha").
		SetCapturedAt(t1.Add(time.Minute)).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("codex-rw-1").
		SetToolEventID("resp-rw-1").
		SetObservedStartAt(t1.Add(2 * time.Minute)).
		SetObservedEndAt(t1.Add(3 * time.Minute)).
		SetUsageUnit("token").
		SetInputTokens(30).
		SetOutputTokens(14).
		SetDedupeKey("codex:rw:1").
		SetCommitCheckpointID(cp.ID).
		SaveX(ctx)

	svc := NewService(client)
	result, err := svc.RefreshPR(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"new-sha"}, nil
		},
	}, pr)
	if err != nil {
		t.Fatalf("RefreshPR error: %v", err)
	}
	if result.Summary.InputTokens != 30 || result.Summary.OutputTokens != 14 {
		t.Fatalf("summary = %+v, want input=30 output=14", result.Summary)
	}

	row := client.PRCommitUsageSnapshot.Query().OnlyX(ctx)
	if row.CommitSha != "new-sha" {
		t.Fatalf("commit_sha = %q, want %q", row.CommitSha, "new-sha")
	}
}
