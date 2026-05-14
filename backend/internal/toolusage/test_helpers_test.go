package toolusage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type testToolUsageScope struct {
	UserID       int
	RepoConfigID int
	WorkspaceID  string
}

type bindingFixture struct {
	WorkspaceID        string
	CheckpointID       int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

type TestToolUsageScope = testToolUsageScope
type BindingFixture = bindingFixture

func seedToolUsageScope(t *testing.T, client *ent.Client) testToolUsageScope {
	t.Helper()

	ctx := context.Background()
	suffix := time.Now().UnixNano()

	scm := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	user := client.User.Create().
		SetUsername(fmt.Sprintf("alice-%d", suffix)).
		SetEmail(fmt.Sprintf("alice-%d@test.com", suffix)).
		SetAuthSource("ldap").
		SaveX(ctx)

	repoCfg := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName(fmt.Sprintf("repo-%d", suffix)).
		SetFullName(fmt.Sprintf("org/repo-%d", suffix)).
		SetCloneURL(fmt.Sprintf("https://github.com/org/repo-%d.git", suffix)).
		SetDefaultBranch("main").
		SaveX(ctx)

	return testToolUsageScope{
		UserID:       user.ID,
		RepoConfigID: repoCfg.ID,
		WorkspaceID:  "ws-1",
	}
}

func createToolUsageBindingFixture(t *testing.T, client *ent.Client) bindingFixture {
	t.Helper()

	ctx := context.Background()
	scope := seedToolUsageScope(t, client)

	prev := time.Unix(150, 0).UTC()
	commitAt := time.Unix(200, 0).UTC()

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("abc123").
		SetParentShas([]string{"parent-1"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(commitAt).
		SaveX(ctx)

	insert := func(dedupeKey string, endAt time.Time, bound bool) {
		create := client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID("codex-sess-1").
			SetToolEventID(dedupeKey).
			SetDedupeKey(dedupeKey).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetInputTokens(10).
			SetOutputTokens(5).
			SetObservedStartAt(endAt.Add(-1 * time.Second)).
			SetObservedEndAt(endAt)
		if bound {
			create.SetCommitCheckpointID(checkpoint.ID)
		}
		create.SaveX(ctx)
	}

	insert("evt-window-1", prev.Add(2*time.Second), false)
	insert("evt-window-2", commitAt.Add(-10*time.Second), false)
	insert("evt-before-window", prev.Add(-10*time.Second), false)
	insert("evt-already-bound", commitAt.Add(-4*time.Second), true)

	return bindingFixture{
		WorkspaceID:        scope.WorkspaceID,
		CheckpointID:       checkpoint.ID,
		CommitCapturedAt:   commitAt,
		PreviousCapturedAt: prev,
	}
}

func SeedToolUsageScopeForTests(t *testing.T, client *ent.Client) TestToolUsageScope {
	t.Helper()
	return seedToolUsageScope(t, client)
}

func CreateToolUsageBindingFixtureForTests(t *testing.T, client *ent.Client) BindingFixture {
	t.Helper()
	return createToolUsageBindingFixture(t, client)
}

func TestSeedToolUsageScope_CreatesUserAndRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	scope := seedToolUsageScope(t, client)

	user := client.User.GetX(ctx, scope.UserID)
	if user.Username == "" {
		t.Fatalf("user = %+v, want populated username", user)
	}

	repoCfg := client.RepoConfig.GetX(ctx, scope.RepoConfigID)
	if repoCfg.FullName == "" || repoCfg.CloneURL == "" {
		t.Fatalf("repo = %+v, want full_name and clone_url", repoCfg)
	}
}

func TestCreateToolUsageBindingFixture_SeedsBoundAndUnboundRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	fixture := createToolUsageBindingFixture(t, client)

	rows := client.ToolUsageEvent.Query().
		Where(toolusageevent.WorkspaceIDEQ(fixture.WorkspaceID)).
		AllX(ctx)
	if len(rows) != 4 {
		t.Fatalf("tool usage row count = %d, want 4", len(rows))
	}
}
