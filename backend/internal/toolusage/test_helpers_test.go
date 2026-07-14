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

type eventFilterFixture struct {
	Alpha      testToolUsageScope
	Beta       testToolUsageScope
	From       time.Time
	To         time.Time
	EventNames map[int]string
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
		SetUserID(scope.UserID).
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

func seedEventFilterFixture(t *testing.T, client *ent.Client) eventFilterFixture {
	t.Helper()

	ctx := context.Background()
	alpha := seedToolUsageScope(t, client)
	beta := seedToolUsageScope(t, client)
	client.User.UpdateOneID(alpha.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		ExecX(ctx)
	client.User.UpdateOneID(beta.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		ExecX(ctx)
	client.RepoConfig.UpdateOneID(alpha.RepoConfigID).
		SetName("alpha").
		SetFullName("org/alpha").
		ExecX(ctx)
	client.RepoConfig.UpdateOneID(beta.RepoConfigID).
		SetName("beta").
		SetFullName("org/beta").
		ExecX(ctx)

	from := time.Date(2026, time.July, 15, 2, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	boundaryCheckpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-boundary").
		SetUserID(alpha.UserID).
		SetWorkspaceID(alpha.WorkspaceID).
		SetRepoConfigID(alpha.RepoConfigID).
		SetCommitSha("boundary-sha").
		SetParentShas([]string{"boundary-parent"}).
		SetBindingSource(commitcheckpoint.BindingSourceManual).
		SetCapturedAt(from.Add(-time.Minute)).
		SaveX(ctx)
	commitNeedleCheckpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-commit-needle").
		SetUserID(beta.UserID).
		SetWorkspaceID(beta.WorkspaceID).
		SetRepoConfigID(beta.RepoConfigID).
		SetCommitSha("prefix-COMMIT-NEEDLE-suffix").
		SetParentShas([]string{"commit-parent"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(to.Add(time.Minute)).
		SaveX(ctx)

	eventNames := make(map[int]string)
	seed := func(
		name string,
		scope testToolUsageScope,
		repoID int,
		tool string,
		toolSessionID string,
		toolEventID string,
		dedupeKey string,
		observedEndAt time.Time,
		rawSourcePath string,
		checkpointID int,
	) {
		create := client.ToolUsageEvent.Create().
			SetTool(tool).
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(repoID).
			SetUserID(scope.UserID).
			SetToolSessionID(toolSessionID).
			SetToolEventID(toolEventID).
			SetDedupeKey(dedupeKey).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetRequestCount(1).
			SetObservedStartAt(observedEndAt.Add(-time.Second)).
			SetObservedEndAt(observedEndAt).
			SetRawPayload(map[string]any{"fixture": name})
		if rawSourcePath != "" {
			create.SetRawSourcePath(rawSourcePath)
		}
		if checkpointID > 0 {
			create.SetCommitCheckpointID(checkpointID)
		}
		event := create.SaveX(ctx)
		eventNames[event.ID] = name
	}

	seed("time-from", alpha, alpha.RepoConfigID, "kiro", "time-from-session", "time-from-event", "time-from-dedupe", from, "", boundaryCheckpoint.ID)
	seed("time-to", alpha, alpha.RepoConfigID, "claude", "time-to-session", "time-to-event", "time-to-dedupe", to, "", boundaryCheckpoint.ID)
	seed("q-source", alpha, beta.RepoConfigID, "claude", "q-source-session", "q-source-event", "q-source-dedupe", from.Add(-2*time.Hour), "/synthetic/sources/SOURCE-NEEDLE.JSONL/", 0)
	seed("q-session", beta, beta.RepoConfigID, "codex", "prefix-SESSION-NEEDLE-suffix", "q-session-event", "q-session-dedupe", to.Add(2*time.Hour), "", 0)
	seed("q-event", beta, beta.RepoConfigID, "claude", "q-event-session", "prefix-EVENT-NEEDLE-suffix", "q-event-dedupe", to.Add(3*time.Hour), "", 0)
	seed("q-dedupe", beta, alpha.RepoConfigID, "claude", "q-dedupe-session", "q-dedupe-event", "prefix-DEDUPE-NEEDLE-suffix", to.Add(4*time.Hour), "", 0)
	seed("q-commit", beta, beta.RepoConfigID, "kiro", "q-commit-session", "q-commit-event", "q-commit-dedupe", to.Add(5*time.Hour), "", commitNeedleCheckpoint.ID)
	seed("directory-decoy", beta, beta.RepoConfigID, "kiro", "directory-decoy-session", "directory-decoy-event", "directory-decoy-dedupe", to.Add(6*time.Hour), "/private/directory-only-needle/source.jsonl", 0)

	return eventFilterFixture{
		Alpha:      alpha,
		Beta:       beta,
		From:       from,
		To:         to,
		EventNames: eventNames,
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
