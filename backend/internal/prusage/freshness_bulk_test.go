package prusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const freshnessFixtureBatchSize = 100

var freshnessFixtureTime = time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)

type recordedFreshnessSQL struct {
	Query string
	Args  []any
}

type freshnessFactQueryRole string

const (
	freshnessQueryUnknown         freshnessFactQueryRole = "unknown"
	freshnessQuerySnapshots       freshnessFactQueryRole = "snapshots"
	freshnessQueryPendingEvents   freshnessFactQueryRole = "pending_events"
	freshnessQueryCheckpointFacts freshnessFactQueryRole = "checkpoint_facts"
)

type recordingPostgresDriver struct {
	dialect.Driver

	mu         sync.Mutex
	statements []recordedFreshnessSQL

	blockSnapshots   bool
	snapshotStarted  chan struct{}
	snapshotReturned chan struct{}
	startedOnce      sync.Once
	returnedOnce     sync.Once
	inFlight         int
}

func (d *recordingPostgresDriver) Exec(ctx context.Context, query string, args, v any) error {
	d.record(query, args)
	return d.Driver.Exec(ctx, query, args, v)
}

func (d *recordingPostgresDriver) Query(ctx context.Context, query string, args, v any) error {
	role := classifyFreshnessFactQuery(query)

	d.mu.Lock()
	d.statements = append(d.statements, recordedFreshnessSQL{Query: query, Args: copyFreshnessSQLArgs(args)})
	block := d.blockSnapshots && role == freshnessQuerySnapshots
	if block {
		d.inFlight++
	}
	d.mu.Unlock()

	if !block {
		return d.Driver.Query(ctx, query, args, v)
	}

	d.startedOnce.Do(func() { close(d.snapshotStarted) })
	defer func() {
		d.mu.Lock()
		d.inFlight--
		d.mu.Unlock()
		d.returnedOnce.Do(func() { close(d.snapshotReturned) })
	}()
	<-ctx.Done()
	return ctx.Err()
}

func (d *recordingPostgresDriver) record(query string, args any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statements = append(d.statements, recordedFreshnessSQL{Query: query, Args: copyFreshnessSQLArgs(args)})
}

func (d *recordingPostgresDriver) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statements = nil
	d.blockSnapshots = false
}

func (d *recordingPostgresDriver) blockSnapshotQuery() (<-chan struct{}, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.blockSnapshots = true
	d.snapshotStarted = make(chan struct{})
	d.snapshotReturned = make(chan struct{})
	d.startedOnce = sync.Once{}
	d.returnedOnce = sync.Once{}
	return d.snapshotStarted, d.snapshotReturned
}

func (d *recordingPostgresDriver) recordedStatements() []recordedFreshnessSQL {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]recordedFreshnessSQL, len(d.statements))
	for i, statement := range d.statements {
		out[i] = recordedFreshnessSQL{
			Query: statement.Query,
			Args:  append([]any(nil), statement.Args...),
		}
	}
	return out
}

func (d *recordingPostgresDriver) inFlightQueries() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inFlight
}

func copyFreshnessSQLArgs(args any) []any {
	values, ok := args.([]any)
	if !ok {
		return []any{args}
	}
	return append([]any(nil), values...)
}

func classifyFreshnessFactQuery(query string) freshnessFactQueryRole {
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	switch {
	case strings.Contains(normalized, `from "pr_commit_usage_snapshots"`) &&
		strings.Contains(normalized, `"pr_record_id" in (`):
		return freshnessQuerySnapshots
	case strings.Contains(normalized, `from "tool_usage_events"`) &&
		strings.Contains(normalized, "count(") &&
		strings.Contains(normalized, `"repo_config_id"`) &&
		strings.Contains(normalized, `"commit_checkpoint_id" is null`) &&
		!strings.Contains(normalized, "group by"):
		return freshnessQueryPendingEvents
	case strings.Contains(normalized, `from "tool_usage_events"`) &&
		strings.Contains(normalized, "count(") &&
		strings.Contains(normalized, "max(") &&
		strings.Contains(normalized, `"observed_end_at"`) &&
		strings.Contains(normalized, `"commit_checkpoint_id" in (`) &&
		strings.Contains(normalized, "group by"):
		return freshnessQueryCheckpointFacts
	default:
		return freshnessQueryUnknown
	}
}

func sortedFreshnessFactRoles(statements []recordedFreshnessSQL) []freshnessFactQueryRole {
	roles := make([]freshnessFactQueryRole, len(statements))
	for i, statement := range statements {
		roles[i] = classifyFreshnessFactQuery(statement.Query)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}

type freshnessPageFixture struct {
	client   *ent.Client
	recorder *recordingPostgresDriver
	repo     *ent.RepoConfig
	prs      []*ent.PrRecord

	snapshotCount   int
	checkpointCount int
	eventCount      int
}

func newRecordingFreshnessFixture(t *testing.T) *freshnessPageFixture {
	t.Helper()
	_, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open recording postgres db: %v", err)
	}
	recorder := &recordingPostgresDriver{Driver: entsql.OpenDB(dialect.Postgres, db)}
	client := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close recording ent client: %v", err)
		}
	})

	ctx := context.Background()
	provider := client.ScmProvider.Create().
		SetName("synthetic-github").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://example.com/api").
		SetCredentials("test-credentials").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("alpha").
		SetFullName("org/alpha").
		SetCloneURL("https://example.com/org/alpha.git").
		SetDefaultBranch("main").
		SetCreatedAt(freshnessFixtureTime).
		SetUpdatedAt(freshnessFixtureTime).
		SaveX(ctx)
	client.User.CreateBulk(
		client.User.Create().
			SetUsername("alice").
			SetEmail("alice@example.com").
			SetAuthSource("ldap"),
		client.User.Create().
			SetUsername("bob").
			SetEmail("bob@example.org").
			SetAuthSource("ldap"),
	).SaveX(ctx)

	return &freshnessPageFixture{client: client, recorder: recorder, repo: repo}
}

func seedParityFreshnessFixture(t *testing.T) *freshnessPageFixture {
	t.Helper()
	fixture := newRecordingFreshnessFixture(t)
	ctx := context.Background()
	refreshedAt := freshnessFixtureTime.Add(30 * time.Minute)

	fixture.prs = createFreshnessFixturePRs(ctx, fixture.client, fixture.repo.ID, 5, refreshedAt)
	users := fixture.client.User.Query().AllX(ctx)
	checkpoints := fixture.client.CommitCheckpoint.CreateBulk(
		newFreshnessFixtureCheckpoint(fixture.client, fixture.repo.ID, users[0].ID, "parity-fresh", "0000000000000000000000000000000000000001", 0),
		newFreshnessFixtureCheckpoint(fixture.client, fixture.repo.ID, users[1].ID, "parity-no-events", "0000000000000000000000000000000000000003", 1),
		newFreshnessFixtureCheckpoint(fixture.client, fixture.repo.ID, users[0].ID, "parity-stale", "0000000000000000000000000000000000000004", 2),
	).SaveX(ctx)

	// Creation order, PR input order, snapshot IDs, and sort_order intentionally disagree.
	fixture.client.PRCommitUsageSnapshot.CreateBulk(
		fixture.client.PRCommitUsageSnapshot.Create().
			SetPrRecordID(fixture.prs[3].ID).
			SetCommitSha(checkpoints[2].CommitSha).
			SetCommitCheckpointID(checkpoints[2].ID).
			SetSortOrder(30).
			SetCreatedAt(freshnessFixtureTime).
			SetUpdatedAt(freshnessFixtureTime),
		fixture.client.PRCommitUsageSnapshot.Create().
			SetPrRecordID(fixture.prs[0].ID).
			SetCommitSha(checkpoints[0].CommitSha).
			SetCommitCheckpointID(checkpoints[0].ID).
			SetSortOrder(10).
			SetCreatedAt(freshnessFixtureTime).
			SetUpdatedAt(freshnessFixtureTime),
		fixture.client.PRCommitUsageSnapshot.Create().
			SetPrRecordID(fixture.prs[1].ID).
			SetCommitSha("0000000000000000000000000000000000000002").
			SetSortOrder(20).
			SetCreatedAt(freshnessFixtureTime).
			SetUpdatedAt(freshnessFixtureTime),
		fixture.client.PRCommitUsageSnapshot.Create().
			SetPrRecordID(fixture.prs[2].ID).
			SetCommitSha(checkpoints[1].CommitSha).
			SetCommitCheckpointID(checkpoints[1].ID).
			SetSortOrder(0).
			SetCreatedAt(freshnessFixtureTime).
			SetUpdatedAt(freshnessFixtureTime),
	).SaveX(ctx)

	fixture.client.ToolUsageEvent.CreateBulk(
		newFreshnessFixtureEvent(fixture.client, fixture.repo.ID, users[0].ID, "parity-fresh", checkpoints[0].ID, refreshedAt.Add(-time.Minute)),
		newFreshnessFixtureEvent(fixture.client, fixture.repo.ID, users[1].ID, "parity-stale", checkpoints[2].ID, refreshedAt.Add(time.Minute)),
		newFreshnessFixtureEvent(fixture.client, fixture.repo.ID, users[0].ID, "parity-pending", 0, refreshedAt.Add(-2*time.Minute)),
	).SaveX(ctx)

	fixture.prs = []*ent.PrRecord{
		fixture.prs[4],
		fixture.prs[2],
		fixture.prs[0],
		fixture.prs[3],
		fixture.prs[1],
	}
	fixture.snapshotCount = 4
	fixture.checkpointCount = 3
	fixture.eventCount = 3
	assertAndResetFreshnessFixture(t, fixture)
	return fixture
}

func seedScaleFreshnessFixture(t *testing.T, prCount, snapshotsPerPR int) *freshnessPageFixture {
	t.Helper()
	fixture := newRecordingFreshnessFixture(t)
	ctx := context.Background()
	refreshedAt := freshnessFixtureTime.Add(30 * time.Minute)
	fixture.prs = createFreshnessFixturePRs(ctx, fixture.client, fixture.repo.ID, prCount, refreshedAt)
	users := fixture.client.User.Query().AllX(ctx)

	checkpointBuilders := make([]*ent.CommitCheckpointCreate, 0, prCount*snapshotsPerPR)
	for prIndex := 0; prIndex < prCount; prIndex++ {
		for commitIndex := 0; commitIndex < snapshotsPerPR; commitIndex++ {
			ordinal := prIndex*snapshotsPerPR + commitIndex
			checkpointBuilders = append(checkpointBuilders, newFreshnessFixtureCheckpoint(
				fixture.client,
				fixture.repo.ID,
				users[ordinal%len(users)].ID,
				fmt.Sprintf("scale-%04d", ordinal),
				fmt.Sprintf("%040x", ordinal+1),
				ordinal,
			))
		}
	}
	checkpoints := saveFreshnessFixtureCheckpoints(ctx, fixture.client, checkpointBuilders)

	snapshotBuilders := make([]*ent.PRCommitUsageSnapshotCreate, 0, len(checkpoints))
	eventBuilders := make([]*ent.ToolUsageEventCreate, 0, len(checkpoints))
	for prIndex, pr := range fixture.prs {
		for commitIndex := 0; commitIndex < snapshotsPerPR; commitIndex++ {
			ordinal := prIndex*snapshotsPerPR + commitIndex
			checkpoint := checkpoints[ordinal]
			snapshotBuilders = append(snapshotBuilders, fixture.client.PRCommitUsageSnapshot.Create().
				SetPrRecordID(pr.ID).
				SetCommitSha(checkpoint.CommitSha).
				SetCommitCheckpointID(checkpoint.ID).
				SetSortOrder(commitIndex).
				SetCreatedAt(freshnessFixtureTime).
				SetUpdatedAt(freshnessFixtureTime))
			eventBuilders = append(eventBuilders, newFreshnessFixtureEvent(
				fixture.client,
				fixture.repo.ID,
				users[ordinal%len(users)].ID,
				fmt.Sprintf("scale-%04d", ordinal),
				checkpoint.ID,
				refreshedAt.Add(-time.Minute),
			))
		}
	}
	saveFreshnessFixtureSnapshots(ctx, fixture.client, snapshotBuilders)
	saveFreshnessFixtureEvents(ctx, fixture.client, eventBuilders)

	fixture.snapshotCount = len(checkpoints)
	fixture.checkpointCount = len(checkpoints)
	fixture.eventCount = len(checkpoints)
	assertAndResetFreshnessFixture(t, fixture)
	return fixture
}

func createFreshnessFixturePRs(
	ctx context.Context,
	client *ent.Client,
	repoConfigID int,
	count int,
	refreshedAt time.Time,
) []*ent.PrRecord {
	builders := make([]*ent.PrRecordCreate, 0, count)
	for i := 0; i < count; i++ {
		builders = append(builders, client.PrRecord.Create().
			SetRepoConfigID(repoConfigID).
			SetScmPrID(i+1).
			SetTitle(fmt.Sprintf("Synthetic PR %d", i+1)).
			SetStatus(prrecord.StatusOpen).
			SetUsageRefreshedAt(refreshedAt).
			SetCreatedAt(freshnessFixtureTime.Add(time.Duration(i)*time.Second)).
			SetUpdatedAt(freshnessFixtureTime.Add(time.Duration(i)*time.Second)))
	}
	return client.PrRecord.CreateBulk(builders...).SaveX(ctx)
}

func newFreshnessFixtureCheckpoint(
	client *ent.Client,
	repoConfigID int,
	userID int,
	key string,
	sha string,
	ordinal int,
) *ent.CommitCheckpointCreate {
	return client.CommitCheckpoint.Create().
		SetEventID("checkpoint-" + key).
		SetUserID(userID).
		SetWorkspaceID("workspace-" + key).
		SetRepoConfigID(repoConfigID).
		SetCommitSha(sha).
		SetParentShas([]string{fmt.Sprintf("%040x", ordinal)}).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCapturedAt(freshnessFixtureTime.Add(time.Duration(ordinal) * time.Second))
}

func newFreshnessFixtureEvent(
	client *ent.Client,
	repoConfigID int,
	userID int,
	key string,
	checkpointID int,
	observedEndAt time.Time,
) *ent.ToolUsageEventCreate {
	builder := client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("workspace-" + key).
		SetRepoConfigID(repoConfigID).
		SetUserID(userID).
		SetToolSessionID("session-" + key).
		SetUsageUnit("token").
		SetInputTokens(10).
		SetOutputTokens(2).
		SetObservedStartAt(observedEndAt.Add(-time.Minute)).
		SetObservedEndAt(observedEndAt).
		SetDedupeKey("dedupe-" + key).
		SetCreatedAt(freshnessFixtureTime)
	if checkpointID > 0 {
		builder.SetCommitCheckpointID(checkpointID)
	}
	return builder
}

func saveFreshnessFixtureCheckpoints(
	ctx context.Context,
	client *ent.Client,
	builders []*ent.CommitCheckpointCreate,
) []*ent.CommitCheckpoint {
	out := make([]*ent.CommitCheckpoint, 0, len(builders))
	for start := 0; start < len(builders); start += freshnessFixtureBatchSize {
		end := min(start+freshnessFixtureBatchSize, len(builders))
		out = append(out, client.CommitCheckpoint.CreateBulk(builders[start:end]...).SaveX(ctx)...)
	}
	return out
}

func saveFreshnessFixtureSnapshots(
	ctx context.Context,
	client *ent.Client,
	builders []*ent.PRCommitUsageSnapshotCreate,
) {
	for start := 0; start < len(builders); start += freshnessFixtureBatchSize {
		end := min(start+freshnessFixtureBatchSize, len(builders))
		client.PRCommitUsageSnapshot.CreateBulk(builders[start:end]...).SaveX(ctx)
	}
}

func saveFreshnessFixtureEvents(
	ctx context.Context,
	client *ent.Client,
	builders []*ent.ToolUsageEventCreate,
) {
	for start := 0; start < len(builders); start += freshnessFixtureBatchSize {
		end := min(start+freshnessFixtureBatchSize, len(builders))
		client.ToolUsageEvent.CreateBulk(builders[start:end]...).SaveX(ctx)
	}
}

func assertAndResetFreshnessFixture(t *testing.T, fixture *freshnessPageFixture) {
	t.Helper()
	ctx := context.Background()
	if got := fixture.client.PrRecord.Query().CountX(ctx); got != len(fixture.prs) {
		t.Fatalf("PR fixture count = %d, want %d", got, len(fixture.prs))
	}
	if got := fixture.client.PRCommitUsageSnapshot.Query().CountX(ctx); got != fixture.snapshotCount {
		t.Fatalf("snapshot fixture count = %d, want %d", got, fixture.snapshotCount)
	}
	if got := fixture.client.CommitCheckpoint.Query().CountX(ctx); got != fixture.checkpointCount {
		t.Fatalf("checkpoint fixture count = %d, want %d", got, fixture.checkpointCount)
	}
	if got := fixture.client.ToolUsageEvent.Query().CountX(ctx); got != fixture.eventCount {
		t.Fatalf("event fixture count = %d, want %d", got, fixture.eventCount)
	}
	fixture.recorder.reset()
}

func TestEvaluatePRFreshnessPageMatchesGoldenSingleResults(t *testing.T) {
	fixture := seedParityFreshnessFixture(t)
	ctx := context.Background()

	got, err := NewService(fixture.client).EvaluatePRFreshnessPage(ctx, fixture.repo.ID, fixture.prs)
	if err != nil {
		t.Fatalf("EvaluatePRFreshnessPage error: %v", err)
	}
	if got == nil {
		t.Fatal("EvaluatePRFreshnessPage returned a nil map")
	}
	if len(got) != 5 {
		t.Fatalf("result count = %d, want exactly 5", len(got))
	}

	golden := map[int]*PRFreshness{
		1: {
			Status: UsageStatusFresh,
			Reason: "Usage snapshot is current.",
			Commits: []CommitFreshness{{
				CommitSHA:       "0000000000000000000000000000000000000001",
				Status:          UsageStatusFresh,
				Reason:          "Usage events were included.",
				CheckpointFound: true,
				UsageEventFound: true,
			}},
		},
		2: {
			Status: UsageStatusNoCheckpoint,
			Reason: "No checkpoint matched this PR commit.",
			Commits: []CommitFreshness{{
				CommitSHA:       "0000000000000000000000000000000000000002",
				Status:          UsageStatusNoCheckpoint,
				Reason:          "No checkpoint matched this PR commit.",
				CheckpointFound: false,
				UsageEventFound: false,
			}},
		},
		3: {
			Status: UsageStatusNoUsageEvents,
			Reason: "Checkpoint exists but no usage events are bound to it.",
			Commits: []CommitFreshness{{
				CommitSHA:       "0000000000000000000000000000000000000003",
				Status:          UsageStatusNoUsageEvents,
				Reason:          "Checkpoint exists but no usage events are bound to it.",
				CheckpointFound: true,
				UsageEventFound: false,
			}},
		},
		4: {
			Status: UsageStatusStaleSnapshot,
			Reason: "Usage events newer than the PR snapshot are bound to this checkpoint.",
			Commits: []CommitFreshness{{
				CommitSHA:       "0000000000000000000000000000000000000004",
				Status:          UsageStatusStaleSnapshot,
				Reason:          "Usage events newer than the PR snapshot are bound to this checkpoint.",
				CheckpointFound: true,
				UsageEventFound: true,
			}},
		},
		5: {
			Status:  UsageStatusPendingUpload,
			Reason:  "Unbound usage events exist for this repo and may still be waiting for checkpoint binding.",
			Commits: nil,
		},
	}

	var checkedAt time.Time
	for _, pr := range fixture.prs {
		want := golden[pr.ScmPrID]
		freshness, ok := got[pr.ID]
		if !ok {
			t.Errorf("result missing PR ID %d (SCM PR %d)", pr.ID, pr.ScmPrID)
			continue
		}
		if freshness.Status != want.Status {
			t.Errorf("PR %d status = %q, want %q", pr.ScmPrID, freshness.Status, want.Status)
		}
		if freshness.Reason != want.Reason {
			t.Errorf("PR %d reason = %q, want %q", pr.ScmPrID, freshness.Reason, want.Reason)
		}
		if freshness.CheckedAt.IsZero() || freshness.CheckedAt.Location() != time.UTC {
			t.Errorf("PR %d checked_at = %v (%v), want non-zero UTC", pr.ScmPrID, freshness.CheckedAt, freshness.CheckedAt.Location())
		}
		if checkedAt.IsZero() {
			checkedAt = freshness.CheckedAt
		} else if !freshness.CheckedAt.Equal(checkedAt) {
			t.Errorf("PR %d checked_at = %v, want page instant %v", pr.ScmPrID, freshness.CheckedAt, checkedAt)
		}
		if !reflect.DeepEqual(freshness.Commits, want.Commits) {
			t.Errorf("PR %d commits = %+v, want %+v", pr.ScmPrID, freshness.Commits, want.Commits)
		}
	}
}

func TestEvaluatePRFreshnessPageQueryCountIsConstant(t *testing.T) {
	type scaleCase struct {
		name           string
		prCount        int
		snapshotsPerPR int
		wantSnapshots  int
	}
	cases := []scaleCase{
		{name: "count-small", prCount: 5, snapshotsPerPR: 1, wantSnapshots: 5},
		{name: "count-large", prCount: 100, snapshotsPerPR: 20, wantSnapshots: 2000},
	}
	wantRoles := []freshnessFactQueryRole{
		freshnessQueryCheckpointFacts,
		freshnessQueryPendingEvents,
		freshnessQuerySnapshots,
	}
	var observedRoles [][]freshnessFactQueryRole

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := seedScaleFreshnessFixture(t, tc.prCount, tc.snapshotsPerPR)
			got, err := NewService(fixture.client).EvaluatePRFreshnessPage(context.Background(), fixture.repo.ID, fixture.prs)
			if err != nil {
				t.Fatalf("EvaluatePRFreshnessPage error: %v", err)
			}
			if len(got) != tc.prCount {
				t.Fatalf("result count = %d, want %d", len(got), tc.prCount)
			}
			for _, pr := range fixture.prs {
				freshness := got[pr.ID]
				if freshness == nil {
					t.Fatalf("result missing PR ID %d", pr.ID)
				}
				if freshness.Status != UsageStatusFresh || freshness.Reason != "Usage snapshot is current." {
					t.Fatalf("PR %d freshness = %q / %q, want exact fresh result", pr.ID, freshness.Status, freshness.Reason)
				}
				if len(freshness.Commits) != tc.snapshotsPerPR {
					t.Fatalf("PR %d commit diagnostics = %d, want %d", pr.ID, len(freshness.Commits), tc.snapshotsPerPR)
				}
			}

			statements := fixture.recorder.recordedStatements()
			if len(statements) != 3 {
				t.Fatalf("SQL statement count = %d, want exactly 3; statements = %+v", len(statements), statements)
			}
			roles := sortedFreshnessFactRoles(statements)
			if !reflect.DeepEqual(roles, wantRoles) {
				t.Fatalf("fact query roles = %v, want %v", roles, wantRoles)
			}
			assertFreshnessFactQueryArguments(t, statements, fixture.repo.ID, tc.prCount, tc.wantSnapshots)
			observedRoles = append(observedRoles, roles)
			t.Logf(
				"fixture totals: prs=%d snapshots=%d checkpoints=%d events=%d fact_queries=%d roles=%v",
				tc.prCount,
				fixture.snapshotCount,
				fixture.checkpointCount,
				fixture.eventCount,
				len(statements),
				roles,
			)
		})
	}

	if len(observedRoles) != 2 || !reflect.DeepEqual(observedRoles[0], observedRoles[1]) {
		t.Fatalf("small/large fact query roles differ: %v", observedRoles)
	}
}

func TestEvaluatePRFreshnessPageHandlesEmptyInputWithoutQueries(t *testing.T) {
	fixture := newRecordingFreshnessFixture(t)
	fixture.recorder.reset()

	got, err := NewService(fixture.client).EvaluatePRFreshnessPage(context.Background(), fixture.repo.ID, nil)
	if err != nil {
		t.Fatalf("EvaluatePRFreshnessPage error: %v", err)
	}
	if got == nil {
		t.Fatal("result map is nil, want empty non-nil map")
	}
	if len(got) != 0 {
		t.Fatalf("result count = %d, want 0", len(got))
	}
	if statements := fixture.recorder.recordedStatements(); len(statements) != 0 {
		t.Fatalf("SQL statement count = %d, want 0; statements = %+v", len(statements), statements)
	}
}

func TestEvaluatePRFreshnessPageHonorsCancellation(t *testing.T) {
	fixture := seedScaleFreshnessFixture(t, 1, 1)
	started, returned := fixture.recorder.blockSnapshotQuery()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := NewService(fixture.client).EvaluatePRFreshnessPage(ctx, fixture.repo.ID, fixture.prs)
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot fact query did not enter the recording driver")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EvaluatePRFreshnessPage error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EvaluatePRFreshnessPage did not return after cancellation")
	}
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked snapshot driver query did not return")
	}
	if got := fixture.recorder.inFlightQueries(); got != 0 {
		t.Fatalf("recording driver in-flight queries = %d, want 0", got)
	}
	statements := fixture.recorder.recordedStatements()
	if len(statements) != 1 || classifyFreshnessFactQuery(statements[0].Query) != freshnessQuerySnapshots {
		t.Fatalf("recorded statements after cancellation = %+v, want only snapshot fact query", statements)
	}
}

func TestEvaluatePRFreshnessPageValidatesInputsAndDeduplicatesIDs(t *testing.T) {
	t.Run("nil service", func(t *testing.T) {
		var service *Service
		_, err := service.EvaluatePRFreshnessPage(context.Background(), 1, nil)
		if err == nil || !strings.Contains(err.Error(), "evaluate PR freshness page: ent client is required") {
			t.Fatalf("error = %v, want operation-wrapped missing client error", err)
		}
	})

	t.Run("invalid repository ID", func(t *testing.T) {
		fixture := newRecordingFreshnessFixture(t)
		fixture.recorder.reset()
		_, err := NewService(fixture.client).EvaluatePRFreshnessPage(context.Background(), 0, nil)
		if err == nil || !strings.Contains(err.Error(), "evaluate PR freshness page: repo config ID must be positive") {
			t.Fatalf("error = %v, want operation-wrapped repository ID error", err)
		}
		if got := len(fixture.recorder.recordedStatements()); got != 0 {
			t.Fatalf("SQL statement count = %d, want 0", got)
		}
	})

	t.Run("nil PR element", func(t *testing.T) {
		fixture := newRecordingFreshnessFixture(t)
		fixture.recorder.reset()
		_, err := NewService(fixture.client).EvaluatePRFreshnessPage(context.Background(), fixture.repo.ID, []*ent.PrRecord{nil})
		if err == nil || !strings.Contains(err.Error(), "evaluate PR freshness page: PR at index 0 is nil") {
			t.Fatalf("error = %v, want operation-wrapped nil PR error", err)
		}
		if got := len(fixture.recorder.recordedStatements()); got != 0 {
			t.Fatalf("SQL statement count = %d, want 0", got)
		}
	})

	t.Run("duplicate IDs", func(t *testing.T) {
		fixture := seedScaleFreshnessFixture(t, 1, 1)
		pr := fixture.prs[0]
		got, err := NewService(fixture.client).EvaluatePRFreshnessPage(
			context.Background(),
			fixture.repo.ID,
			[]*ent.PrRecord{pr, pr},
		)
		if err != nil {
			t.Fatalf("EvaluatePRFreshnessPage error: %v", err)
		}
		if len(got) != 1 || got[pr.ID] == nil {
			t.Fatalf("deduplicated result = %+v, want one PR ID %d", got, pr.ID)
		}
		statements := fixture.recorder.recordedStatements()
		assertFreshnessFactQueryArguments(t, statements, fixture.repo.ID, 1, 1)
	})
}

func assertFreshnessFactQueryArguments(
	t *testing.T,
	statements []recordedFreshnessSQL,
	repoConfigID int,
	prCount int,
	checkpointCount int,
) {
	t.Helper()
	seen := make(map[freshnessFactQueryRole]int, 3)
	for _, statement := range statements {
		role := classifyFreshnessFactQuery(statement.Query)
		seen[role]++
		switch role {
		case freshnessQuerySnapshots:
			if len(statement.Args) != prCount {
				t.Errorf("snapshot query argument count = %d, want %d", len(statement.Args), prCount)
			}
		case freshnessQueryPendingEvents:
			if len(statement.Args) != 1 || !freshnessSQLArgumentEqualsInt(statement.Args[0], repoConfigID) {
				t.Errorf("pending-event query arguments = %v, want explicit repo_config_id %d", statement.Args, repoConfigID)
			}
		case freshnessQueryCheckpointFacts:
			if len(statement.Args) != checkpointCount {
				t.Errorf("checkpoint aggregate argument count = %d, want %d", len(statement.Args), checkpointCount)
			}
		default:
			t.Errorf("unexpected SQL shape: %s", statement.Query)
		}
	}
	for _, role := range []freshnessFactQueryRole{
		freshnessQuerySnapshots,
		freshnessQueryPendingEvents,
		freshnessQueryCheckpointFacts,
	} {
		if seen[role] != 1 {
			t.Errorf("fact query role %q count = %d, want 1", role, seen[role])
		}
	}
}

func freshnessSQLArgumentEqualsInt(value any, want int) bool {
	switch value := value.(type) {
	case int:
		return value == want
	case int32:
		return int(value) == want
	case int64:
		return int(value) == want
	default:
		return false
	}
}
