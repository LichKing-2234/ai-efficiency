package attribution

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/scm"
	_ "github.com/mattn/go-sqlite3"
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

type usageCall struct {
	APIKeyID int64
	From     time.Time
	To       time.Time
}

type fakeRelayProvider struct {
	logs                         []relay.UsageLog
	calls                        []usageCall
	listUsageLogsByAPIKeyExactFn func(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error)
}

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "fake" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
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
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error { return nil }
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	if f.listUsageLogsByAPIKeyExactFn != nil {
		return f.listUsageLogsByAPIKeyExactFn(ctx, apiKeyID, from, to)
	}
	f.calls = append(f.calls, usageCall{APIKeyID: apiKeyID, From: from, To: to})
	return f.logs, nil
}

func testRepoPRSession(t *testing.T, client *ent.Client, apiKeyID int) (*ent.RepoConfig, *ent.PrRecord, *ent.Session) {
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

	sessCreate := client.Session.Create().
		SetRepoConfigID(repo.ID).
		SetBranch("feature/x").
		SetProviderName("codex").
		SetRuntimeRef("rt-1").
		SetInitialWorkspaceRoot("/tmp/repo").
		SetInitialGitDir("/tmp/repo/.git").
		SetInitialGitCommonDir("/tmp/repo/.git")
	if apiKeyID > 0 {
		sessCreate.SetRelayAPIKeyID(apiKeyID)
	}
	sess := sessCreate.SetStartedAt(time.Now().Add(-2 * time.Hour)).SaveX(ctx)

	return repo, pr, sess
}

func TestSettlePR_UsesPreviousOverallCheckpointForMatchedInterval(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, sess := testRepoPRSession(t, client, 321)
	t0 := sess.StartedAt
	t1 := t0.Add(20 * time.Minute)
	t2 := t1.Add(30 * time.Minute)

	client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("base-sha").
		SetParentShas([]string{"p0"}).
		SetCapturedAt(t1).
		SaveX(ctx)

	client.CommitCheckpoint.Create().
		SetEventID("cp-2").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("pr-sha-1").
		SetParentShas([]string{"base-sha"}).
		SetCapturedAt(t2).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		logs: []relay.UsageLog{
			{TotalTokens: 30, TotalCost: 1.25, AccountID: "acct-a"},
		},
	}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-sha-1"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "alice")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "clear" {
		t.Fatalf("result classification = %q, want clear", result.ResultClassification)
	}
	if len(fakeRelay.calls) != 1 {
		t.Fatalf("usage log calls = %d, want 1", len(fakeRelay.calls))
	}
	if !fakeRelay.calls[0].From.Equal(t1) {
		t.Fatalf("from = %s, want %s", fakeRelay.calls[0].From, t1)
	}
	if !fakeRelay.calls[0].To.Equal(t2) {
		t.Fatalf("to = %s, want %s", fakeRelay.calls[0].To, t2)
	}

	updatedPR := client.PrRecord.GetX(ctx, pr.ID)
	if updatedPR.AttributionStatus != prrecord.AttributionStatusClear {
		t.Fatalf("pr attribution_status = %q, want clear", updatedPR.AttributionStatus)
	}
	if updatedPR.PrimaryTokenCount != 30 {
		t.Fatalf("pr primary_token_count = %d, want 30", updatedPR.PrimaryTokenCount)
	}
	if updatedPR.LastAttributionRunID == nil || *updatedPR.LastAttributionRunID == 0 {
		t.Fatal("expected last_attribution_run_id to be set")
	}

	run := client.PrAttributionRun.GetX(ctx, *updatedPR.LastAttributionRunID)
	if run.ResultClassification == nil || string(*run.ResultClassification) != "clear" {
		t.Fatalf("run classification = %v, want clear", run.ResultClassification)
	}
	if run.PrimaryUsageSummary["total_tokens"] != float64(30) {
		t.Fatalf("run total_tokens = %v, want 30", run.PrimaryUsageSummary["total_tokens"])
	}
}

func TestSettlePR_ReturnsAmbiguousWhenMatchedCheckpointIsUnbound(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, _ := testRepoPRSession(t, client, 0)
	capturedAt := time.Now().Add(-10 * time.Minute)
	client.CommitCheckpoint.Create().
		SetEventID("cp-unbound-1").
		SetWorkspaceID("ws-u").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCommitSha("pr-sha-u1").
		SetParentShas([]string{"p1"}).
		SetCapturedAt(capturedAt).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-sha-u1"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "bob")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "ambiguous" {
		t.Fatalf("result classification = %q, want ambiguous", result.ResultClassification)
	}
	if len(fakeRelay.calls) != 0 {
		t.Fatalf("usage logs should not be queried for unbound checkpoint")
	}

	updatedPR := client.PrRecord.GetX(ctx, pr.ID)
	if updatedPR.AttributionStatus != prrecord.AttributionStatusAmbiguous {
		t.Fatalf("pr attribution_status = %q, want ambiguous", updatedPR.AttributionStatus)
	}
	if updatedPR.LastAttributionRunID == nil || *updatedPR.LastAttributionRunID == 0 {
		t.Fatal("expected last_attribution_run_id to be set")
	}
}

func TestSettlePR_UsesRewriteHistoryToMatchCheckpoint(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, sess := testRepoPRSession(t, client, 654)
	t1 := sess.StartedAt.Add(25 * time.Minute)

	client.CommitCheckpoint.Create().
		SetEventID("cp-old").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("old-sha").
		SetParentShas([]string{"base-sha"}).
		SetCapturedAt(t1).
		SaveX(ctx)

	client.CommitRewrite.Create().
		SetEventID("rw-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-rw").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitrewrite.BindingSourceMarker).
		SetRewriteType(commitrewrite.RewriteTypeAmend).
		SetOldCommitSha("old-sha").
		SetNewCommitSha("new-sha").
		SetCapturedAt(t1.Add(1 * time.Minute)).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		logs: []relay.UsageLog{
			{TotalTokens: 44, TotalCost: 2.5, AccountID: "acct-rw"},
		},
	}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"new-sha"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "carol")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "clear" {
		t.Fatalf("result classification = %q, want clear", result.ResultClassification)
	}
	if result.AttributionConfidence != "medium" {
		t.Fatalf("attribution confidence = %q, want medium", result.AttributionConfidence)
	}
	if result.ValidationSummary["confidence"] != "medium" {
		t.Fatalf("validation confidence = %v, want medium", result.ValidationSummary["confidence"])
	}
	if result.ValidationSummary["reason"] != "session_start_fallback" {
		t.Fatalf("validation reason = %v, want session_start_fallback", result.ValidationSummary["reason"])
	}
	if result.PrimaryTokenCount != 44 {
		t.Fatalf("primary_token_count = %d, want 44", result.PrimaryTokenCount)
	}

	updatedPR := client.PrRecord.GetX(ctx, pr.ID)
	if updatedPR.AttributionConfidence == nil || *updatedPR.AttributionConfidence != prrecord.AttributionConfidenceMedium {
		t.Fatalf("pr attribution_confidence = %v, want medium", updatedPR.AttributionConfidence)
	}
	if updatedPR.LastAttributionRunID == nil || *updatedPR.LastAttributionRunID == 0 {
		t.Fatal("expected last_attribution_run_id to be set")
	}
	run := client.PrAttributionRun.GetX(ctx, *updatedPR.LastAttributionRunID)
	if run.ValidationSummary["confidence"] != "medium" {
		t.Fatalf("run validation confidence = %v, want medium", run.ValidationSummary["confidence"])
	}
}

func TestSettlePR_PrefersSessionUsageEventsOverRelayLedger(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, sess := testRepoPRSession(t, client, 111)
	t1 := sess.StartedAt.Add(20 * time.Minute)
	t2 := t1.Add(20 * time.Minute)

	client.CommitCheckpoint.Create().
		SetEventID("cp-local-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("base-local").
		SetParentShas([]string{"p0"}).
		SetCapturedAt(t1).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-local-2").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("pr-local").
		SetParentShas([]string{"base-local"}).
		SetCapturedAt(t2).
		SaveX(ctx)

	client.SessionUsageEvent.Create().
		SetEventID("usage-local-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local").
		SetRequestID("req-local-1").
		SetProviderName("codex").
		SetModel("gpt-5").
		SetStartedAt(t1.Add(1 * time.Minute)).
		SetFinishedAt(t1.Add(2 * time.Minute)).
		SetInputTokens(100).
		SetOutputTokens(40).
		SetTotalTokens(140).
		SetStatus("completed").
		SetRawMetadata(map[string]interface{}{"source": "test"}).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		listUsageLogsByAPIKeyExactFn: func(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
			panic("relay usage logs fallback should not be called when local usage exists")
		},
	}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-local"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "alice")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.PrimaryTokenCount != 140 {
		t.Fatalf("primary_token_count = %d, want 140", result.PrimaryTokenCount)
	}
}

func TestSettlePR_UsesLocalUsageEventsWithoutRelayAPIKey(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, sess := testRepoPRSession(t, client, 999)
	t1 := sess.StartedAt.Add(10 * time.Minute)
	t2 := t1.Add(20 * time.Minute)

	client.CommitCheckpoint.Create().
		SetEventID("cp-local-nokey-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-nokey").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("base-local-nokey").
		SetParentShas([]string{"p0"}).
		SetCapturedAt(t1).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-local-nokey-2").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-nokey").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("pr-local-nokey").
		SetParentShas([]string{"base-local-nokey"}).
		SetCapturedAt(t2).
		SaveX(ctx)

	client.SessionUsageEvent.Create().
		SetEventID("usage-local-nokey-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-nokey").
		SetRequestID("req-local-nokey-1").
		SetProviderName("codex").
		SetModel("gpt-5").
		SetStartedAt(t1.Add(1 * time.Minute)).
		SetFinishedAt(t1.Add(2 * time.Minute)).
		SetInputTokens(90).
		SetOutputTokens(30).
		SetTotalTokens(120).
		SetStatus("completed").
		SetRawMetadata(map[string]interface{}{"source": "test"}).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"pr-local-nokey"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "alice")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.ResultClassification != "clear" {
		t.Fatalf("result classification = %q, want clear", result.ResultClassification)
	}
	if result.PrimaryTokenCount != 120 {
		t.Fatalf("primary_token_count = %d, want 120", result.PrimaryTokenCount)
	}
}

func TestSettlePR_AssignsBoundarySpanningUsageByFinishedAtOnce(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	repo, pr, sess := testRepoPRSession(t, client, 999)
	t1 := sess.StartedAt.Add(10 * time.Minute)
	t2 := t1.Add(20 * time.Minute)
	t3 := t2.Add(20 * time.Minute)

	client.CommitCheckpoint.Create().
		SetEventID("cp-local-overlap-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-overlap").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("base-local-overlap").
		SetParentShas([]string{"p0"}).
		SetCapturedAt(t1).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-local-overlap-2").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-overlap").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("mid-local-overlap").
		SetParentShas([]string{"base-local-overlap"}).
		SetCapturedAt(t2).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-local-overlap-3").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-overlap").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("pr-local-overlap").
		SetParentShas([]string{"mid-local-overlap"}).
		SetCapturedAt(t3).
		SaveX(ctx)

	client.SessionUsageEvent.Create().
		SetEventID("usage-local-overlap-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-local-overlap").
		SetRequestID("req-local-overlap-1").
		SetProviderName("codex").
		SetModel("gpt-5").
		SetStartedAt(t2.Add(-30 * time.Second)).
		SetFinishedAt(t2.Add(30 * time.Second)).
		SetInputTokens(50).
		SetOutputTokens(20).
		SetTotalTokens(70).
		SetStatus("completed").
		SetRawMetadata(map[string]interface{}{"source": "test"}).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{}
	fakeProvider := &fakeSCMProvider{
		listPRCommitsFn: func(ctx context.Context, repoFullName string, prID int) ([]string, error) {
			return []string{"mid-local-overlap", "pr-local-overlap"}, nil
		},
	}

	svc := NewService(client, fakeRelay)
	result, err := svc.Settle(ctx, fakeProvider, pr, "alice")
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if result.PrimaryTokenCount != 70 {
		t.Fatalf("primary_token_count = %d, want 70", result.PrimaryTokenCount)
	}
}
