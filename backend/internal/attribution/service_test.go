package attribution

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
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeSCMProvider struct {
	listPRCommitsFn func(ctx context.Context, repoFullName string, prID int) ([]string, error)
}

func (f *fakeSCMProvider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	return nil, nil
}
func (f *fakeSCMProvider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (f *fakeSCMProvider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	return nil, nil
}
func (f *fakeSCMProvider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (f *fakeSCMProvider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	if f.listPRCommitsFn == nil {
		return nil, nil
	}
	return f.listPRCommitsFn(ctx, repoFullName, prID)
}
func (f *fakeSCMProvider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	return 0, nil
}
func (f *fakeSCMProvider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	return nil
}
func (f *fakeSCMProvider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	return nil
}
func (f *fakeSCMProvider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	return nil
}
func (f *fakeSCMProvider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (webhookID string, err error) {
	return "", nil
}
func (f *fakeSCMProvider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	return nil
}
func (f *fakeSCMProvider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (f *fakeSCMProvider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	return "", nil
}
func (f *fakeSCMProvider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	return nil
}
func (f *fakeSCMProvider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (sha string, err error) {
	return "", nil
}

type fakeRelayProvider struct{}

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "fake" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error) {
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ChatCompletion(ctx context.Context, req relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	return nil, nil
}
func (f *fakeRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return nil, nil
}
func (f *fakeRelayProvider) UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error {
	return nil
}
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error { return nil }
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}

func testRepoPR(t *testing.T, client *ent.Client) (*ent.RepoConfig, *ent.PrRecord) {
	t.Helper()
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
		SetAttributionStatus(prrecord.AttributionStatusNotRun).
		SaveX(ctx)

	return repo, pr
}

func TestSettlePRReturnsAmbiguousWhenMatchedCheckpointHasNoBoundToolUsage(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()

	repo, pr := testRepoPR(t, client)
	userID := client.User.Create().
		SetUsername("ambiguous-user").
		SetEmail("ambiguous-user@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID
	client.CommitCheckpoint.Create().
		SetEventID("cp-unbound-1").
		SetUserID(userID).
		SetWorkspaceID("ws-u").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCommitSha("pr-sha-u1").
		SetParentShas([]string{"p1"}).
		SetCapturedAt(time.Now().Add(-10 * time.Minute)).
		SaveX(ctx)

	svc := NewService(client, &fakeRelayProvider{})
	result, err := svc.Settle(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-sha-u1"}, nil
		},
	}, pr, "bob")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "ambiguous" {
		t.Fatalf("result classification = %q, want ambiguous", result.ResultClassification)
	}
	if result.MetadataSummary["reason"] != "no_bound_tool_usage" {
		t.Fatalf("reason = %v, want no_bound_tool_usage", result.MetadataSummary["reason"])
	}
}

func TestSettlePRUsesRewriteHistoryToMatchCheckpoint(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()

	repo, pr := testRepoPR(t, client)
	userID := client.User.Create().
		SetUsername("tool-user").
		SetEmail("tool-user@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID
	t1 := time.Now().Add(-30 * time.Minute).UTC()

	oldCheckpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-old").
		SetUserID(userID).
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("old-sha").
		SetParentShas([]string{"base-sha"}).
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
		SetCapturedAt(t1.Add(1 * time.Minute)).
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
		SetCommitCheckpointID(oldCheckpoint.ID).
		SaveX(ctx)

	svc := NewService(client, &fakeRelayProvider{})
	result, err := svc.Settle(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"new-sha"}, nil
		},
	}, pr, "carol")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "clear" {
		t.Fatalf("result classification = %q, want clear", result.ResultClassification)
	}
	if result.PrimaryTokenCount != 44 {
		t.Fatalf("primary_token_count = %d, want 44", result.PrimaryTokenCount)
	}
}

func TestSettlePRAggregatesToolUsageEventsByCheckpoint(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()

	repo, pr := testRepoPR(t, client)
	userID := client.User.Create().
		SetUsername("toolusage-user").
		SetEmail("toolusage-user@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID
	t1 := time.Now().Add(-20 * time.Minute).UTC()
	t2 := t1.Add(20 * time.Minute)

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-tool-usage-1").
		SetUserID(userID).
		SetWorkspaceID("ws-tool-usage").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("pr-tool-usage").
		SetParentShas([]string{"base"}).
		SetCapturedAt(t2).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-tool-usage").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("codex-1").
		SetToolEventID("resp-1").
		SetObservedStartAt(t1.Add(1 * time.Minute)).
		SetObservedEndAt(t1.Add(2 * time.Minute)).
		SetUsageUnit("token").
		SetInputTokens(100).
		SetOutputTokens(40).
		SetCachedInputTokens(10).
		SetDedupeKey("codex:1").
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID("ws-tool-usage").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("kiro-1").
		SetToolEventID("turn-1").
		SetObservedStartAt(t1.Add(3 * time.Minute)).
		SetObservedEndAt(t1.Add(4 * time.Minute)).
		SetUsageUnit("credit").
		SetRequestCount(2).
		SetCreditUsage(0.6).
		SetDedupeKey("kiro:1").
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	svc := NewService(client, &fakeRelayProvider{})
	result, err := svc.Settle(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-tool-usage"}, nil
		},
	}, pr, "tester")
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if result.PrimaryTokenCount != 150 {
		t.Fatalf("PrimaryTokenCount = %d, want 150", result.PrimaryTokenCount)
	}
	if result.MetadataSummary["kiro_credit_usage"] != 0.6 {
		t.Fatalf("MetadataSummary = %+v", result.MetadataSummary)
	}
	if got := result.MetadataSummary["matched_session_count"]; got != 2 {
		t.Fatalf("matched_session_count = %v, want 2 tool-native sessions", got)
	}
}

func TestSettlePRAllowsUnboundCheckpointWhenToolUsageIsAlreadyBound(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()

	repo, pr := testRepoPR(t, client)
	userID := client.User.Create().
		SetUsername("sessionless-user").
		SetEmail("sessionless-user@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-sessionless-1").
		SetUserID(userID).
		SetWorkspaceID("ws-sessionless").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCommitSha("pr-sessionless-1").
		SetParentShas([]string{"base"}).
		SetCapturedAt(time.Now().Add(-5 * time.Minute)).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-sessionless").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("codex-sessionless-1").
		SetToolEventID("resp-sessionless-1").
		SetObservedStartAt(checkpoint.CapturedAt.Add(-30 * time.Second)).
		SetObservedEndAt(checkpoint.CapturedAt.Add(-5 * time.Second)).
		SetUsageUnit("token").
		SetInputTokens(70).
		SetOutputTokens(11).
		SetCachedInputTokens(9).
		SetReasoningTokens(4).
		SetDedupeKey("codex:sessionless:1").
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	svc := NewService(client, &fakeRelayProvider{})
	result, err := svc.Settle(ctx, &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-sessionless-1"}, nil
		},
	}, pr, "tester")
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if result.ResultClassification != "clear" {
		t.Fatalf("ResultClassification = %q, want clear", result.ResultClassification)
	}
	if result.PrimaryTokenCount != 90 {
		t.Fatalf("PrimaryTokenCount = %d, want 90", result.PrimaryTokenCount)
	}
	if got := result.MetadataSummary["matched_session_count"]; got != 1 {
		t.Fatalf("matched_session_count = %v, want 1 tool-native session", got)
	}
}
