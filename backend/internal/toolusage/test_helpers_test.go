package toolusage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const (
	largeEventFixtureSize       = 2400
	largeEventFixtureBatchSize  = 200
	largeEventRawPayloadSize    = 16 * 1024
	largeEventRawPayloadMarker  = "task3-large-event-payload-marker:"
	largeEventCommitNeedleIndex = 40
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

type largeEventRecord struct {
	Ordinal int
	UserID  int
	RepoID  int
	Bound   bool
	Row     EventListRow
}

type largeEventFixture struct {
	AliceUserID int
	BobUserID   int
	AlphaRepoID int
	BetaRepoID  int
	BaseTime    time.Time
	Records     []largeEventRecord
	RawPayload  string
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
	seed("q-source", alpha, beta.RepoConfigID, "claude", "q-source-session", "q-source-event", "q-source-dedupe", from.Add(-2*time.Hour), "/synthetic/sources/LITERAL\\BACKSLASH-SOURCE-NEEDLE.JSONL/", 0)
	seed("q-session", beta, beta.RepoConfigID, "codex", "prefix-SESSION-NEEDLE-suffix", "prefix-LITERAL_UNDERSCORE-suffix", "q-session-dedupe", to.Add(2*time.Hour), "", 0)
	seed("q-event", beta, beta.RepoConfigID, "claude", "prefix-LITERALXUNDERSCORE-suffix", "prefix-EVENT-NEEDLE-suffix", "prefix-LITERALBACKSLASH-suffix", to.Add(3*time.Hour), "", 0)
	seed("q-dedupe", beta, alpha.RepoConfigID, "claude", "q-dedupe-session", "q-dedupe-event", "prefix-DEDUPE-NEEDLE-suffix", to.Add(4*time.Hour), "", 0)
	seed("q-commit", beta, beta.RepoConfigID, "kiro", "q-commit-session", "q-commit-event", "q-commit-dedupe", to.Add(5*time.Hour), "", commitNeedleCheckpoint.ID)
	seed("directory-decoy", beta, beta.RepoConfigID, "kiro", "directory-decoy-session", "directory-decoy-event", "directory-decoy-dedupe", to.Add(6*time.Hour), "/private/directory-only-needle/LITERALBACKSLASH-source.jsonl", 0)

	return eventFilterFixture{
		Alpha:      alpha,
		Beta:       beta,
		From:       from,
		To:         to,
		EventNames: eventNames,
	}
}

func seedLargeEventFixture(t *testing.T, client *ent.Client) largeEventFixture {
	t.Helper()

	ctx := context.Background()
	scm := client.ScmProvider.Create().
		SetName("github-scale-fixture").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("test-credentials").
		SaveX(ctx)
	users := []*ent.User{
		client.User.Create().
			SetUsername("alice").
			SetEmail("alice@example.com").
			SetAuthSource("ldap").
			SetRole(user.RoleUser).
			SaveX(ctx),
		client.User.Create().
			SetUsername("bob").
			SetEmail("bob@example.org").
			SetAuthSource("ldap").
			SetRole(user.RoleAdmin).
			SaveX(ctx),
	}
	repos := []*ent.RepoConfig{
		client.RepoConfig.Create().
			SetScmProviderID(scm.ID).
			SetName("alpha").
			SetFullName("org/alpha").
			SetCloneURL("https://github.com/org/alpha.git").
			SetDefaultBranch("main").
			SaveX(ctx),
		client.RepoConfig.Create().
			SetScmProviderID(scm.ID).
			SetName("beta").
			SetFullName("org/beta").
			SetCloneURL("https://github.com/org/beta.git").
			SetDefaultBranch("main").
			SaveX(ctx),
	}

	baseTime := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	workspaceIDs := []string{"scale-workspace-alice", "scale-workspace-bob"}
	sharedCheckpoints := make(map[[2]int]*ent.CommitCheckpoint, 4)
	for userIndex := range users {
		for repoIndex := range repos {
			key := [2]int{userIndex, repoIndex}
			sharedCheckpoints[key] = client.CommitCheckpoint.Create().
				SetEventID(fmt.Sprintf("scale-checkpoint-%d-%d", userIndex, repoIndex)).
				SetUserID(users[userIndex].ID).
				SetWorkspaceID(workspaceIDs[userIndex]).
				SetRepoConfigID(repos[repoIndex].ID).
				SetCommitSha(fmt.Sprintf("scale-shared-commit-%d-%d", userIndex, repoIndex)).
				SetParentShas([]string{"scale-parent"}).
				SetBindingSource(commitcheckpoint.BindingSourceManual).
				SetCapturedAt(baseTime.Add(-time.Minute)).
				SaveX(ctx)
		}
	}
	commitNeedleCheckpoint := client.CommitCheckpoint.Create().
		SetEventID("scale-checkpoint-commit-needle").
		SetUserID(users[0].ID).
		SetWorkspaceID(workspaceIDs[0]).
		SetRepoConfigID(repos[0].ID).
		SetCommitSha("scale-COMMIT-NEEDLE-0040").
		SetParentShas([]string{"scale-parent"}).
		SetBindingSource(commitcheckpoint.BindingSourceManual).
		SetCapturedAt(baseTime.Add(-time.Minute)).
		SaveX(ctx)

	rawPayload := largeEventRawPayloadMarker + strings.Repeat(
		"x",
		largeEventRawPayloadSize-len(largeEventRawPayloadMarker),
	)
	tools := []string{"codex", "claude", "kiro"}
	records := make([]largeEventRecord, 0, largeEventFixtureSize)
	for batchStart := 0; batchStart < largeEventFixtureSize; batchStart += largeEventFixtureBatchSize {
		creates := make([]*ent.ToolUsageEventCreate, 0, largeEventFixtureBatchSize)
		for i := batchStart; i < batchStart+largeEventFixtureBatchSize; i++ {
			userIndex := i % len(users)
			repoIndex := (i / 4) % len(repos)
			observedEndAt := baseTime.Add(time.Duration(i%64) * time.Minute)
			create := client.ToolUsageEvent.Create().
				SetTool(tools[i%len(tools)]).
				SetWorkspaceID(workspaceIDs[userIndex]).
				SetRepoConfigID(repos[repoIndex].ID).
				SetUserID(users[userIndex].ID).
				SetToolSessionID(fmt.Sprintf("scale-session-%04d", i)).
				SetToolEventID(fmt.Sprintf("scale-event-%04d", i)).
				SetDedupeKey(fmt.Sprintf("scale-dedupe-%04d", i)).
				SetUsageUnit(toolusageevent.UsageUnitToken).
				SetRequestCount(i%5 + 1).
				SetInputTokens(int64(1000 + i)).
				SetOutputTokens(int64(2000 + i)).
				SetCachedInputTokens(int64(3000 + i)).
				SetReasoningTokens(int64(4000 + i)).
				SetCreditUsage(float64(i%100) + 0.5).
				SetContextUsagePct(float64(i % 100)).
				SetObservedStartAt(observedEndAt.Add(-time.Second)).
				SetObservedEndAt(observedEndAt).
				SetRawSourcePath(fmt.Sprintf("/synthetic/directory-only-fragment/source-%04d.jsonl", i)).
				SetRawSourceLocator(fmt.Sprintf("line:%04d", i+1)).
				SetRawPayload(map[string]any{"content": rawPayload})
			if i%4 < 2 {
				checkpoint := sharedCheckpoints[[2]int{userIndex, repoIndex}]
				if i == largeEventCommitNeedleIndex {
					checkpoint = commitNeedleCheckpoint
				}
				create.SetCommitCheckpointID(checkpoint.ID)
			}
			creates = append(creates, create)
		}

		events := client.ToolUsageEvent.CreateBulk(creates...).SaveX(ctx)
		for batchIndex, event := range events {
			i := batchStart + batchIndex
			userIndex := i % len(users)
			repoIndex := (i / 4) % len(repos)
			bound := i%4 < 2
			row := EventListRow{
				ID:                event.ID,
				Tool:              tools[i%len(tools)],
				RepoID:            repos[repoIndex].ID,
				RepoName:          repos[repoIndex].FullName,
				Username:          users[userIndex].Username,
				ToolSessionID:     fmt.Sprintf("scale-session-%04d", i),
				ToolEventID:       fmt.Sprintf("scale-event-%04d", i),
				DedupeKey:         fmt.Sprintf("scale-dedupe-%04d", i),
				ObservedEndAt:     baseTime.Add(time.Duration(i%64) * time.Minute),
				RequestCount:      i%5 + 1,
				InputTokens:       int64(1000 + i),
				OutputTokens:      int64(2000 + i),
				CachedInputTokens: int64(3000 + i),
				ReasoningTokens:   int64(4000 + i),
				CreditUsage:       float64(i%100) + 0.5,
				SourceBasename:    fmt.Sprintf("source-%04d.jsonl", i),
				BindingStatus:     "unbound",
			}
			if bound {
				checkpoint := sharedCheckpoints[[2]int{userIndex, repoIndex}]
				if i == largeEventCommitNeedleIndex {
					checkpoint = commitNeedleCheckpoint
				}
				checkpointID := checkpoint.ID
				row.CommitCheckpointID = &checkpointID
				row.CommitSHA = checkpoint.CommitSha
				row.BindingStatus = "bound"
			}
			records = append(records, largeEventRecord{
				Ordinal: i,
				UserID:  users[userIndex].ID,
				RepoID:  repos[repoIndex].ID,
				Bound:   bound,
				Row:     row,
			})
		}
	}

	return largeEventFixture{
		AliceUserID: users[0].ID,
		BobUserID:   users[1].ID,
		AlphaRepoID: repos[0].ID,
		BetaRepoID:  repos[1].ID,
		BaseTime:    baseTime,
		Records:     records,
		RawPayload:  rawPayload,
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
