package directorysync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const (
	largeRunHistoryCount       = 2400
	largeRunHistoryBatchSize   = 200
	largeRunDiagnosticBlobSize = 4 * 1024
	largeRunResponseByteLimit  = 128 * 1024
)

type recordedRunQuery struct {
	SQL  string
	Args []any
}

type recordingRunDriver struct {
	dialect.Driver

	mu        sync.Mutex
	queries   []recordedRunQuery
	queryHook func(recordedRunQuery)
}

func (d *recordingRunDriver) Query(ctx context.Context, query string, args, value any) error {
	d.record(query, args)
	return d.Driver.Query(ctx, query, args, value)
}

func (d *recordingRunDriver) BeginTx(ctx context.Context, opts *sql.TxOptions) (dialect.Tx, error) {
	driver, ok := d.Driver.(interface {
		BeginTx(context.Context, *sql.TxOptions) (dialect.Tx, error)
	})
	if !ok {
		return nil, fmt.Errorf("recording driver does not support transaction options")
	}
	tx, err := driver.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &recordingRunTx{Tx: tx, recorder: d}, nil
}

type recordingRunTx struct {
	dialect.Tx
	recorder *recordingRunDriver
}

func (tx *recordingRunTx) Query(ctx context.Context, query string, args, value any) error {
	tx.recorder.record(query, args)
	return tx.Tx.Query(ctx, query, args, value)
}

func (d *recordingRunDriver) record(query string, args any) {
	values, ok := args.([]any)
	if !ok {
		values = []any{args}
	}
	recorded := recordedRunQuery{SQL: query, Args: append([]any(nil), values...)}

	d.mu.Lock()
	d.queries = append(d.queries, recorded)
	hook := d.queryHook
	d.mu.Unlock()

	if hook != nil {
		hook(recorded)
	}
}

func (d *recordingRunDriver) setQueryHook(hook func(recordedRunQuery)) {
	d.mu.Lock()
	d.queryHook = hook
	d.mu.Unlock()
}

func (d *recordingRunDriver) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries = nil
}

func (d *recordingRunDriver) snapshot() []recordedRunQuery {
	d.mu.Lock()
	defer d.mu.Unlock()
	queries := make([]recordedRunQuery, len(d.queries))
	for i, query := range d.queries {
		queries[i] = recordedRunQuery{
			SQL:  query.SQL,
			Args: append([]any(nil), query.Args...),
		}
	}
	return queries
}

type largeRunSeed struct {
	ID        int
	Mode      directorysyncrun.Mode
	Status    directorysyncrun.Status
	StartedAt *time.Time
}

type largeRunHistorySeed struct {
	Runs            []largeRunSeed
	ExpectedIDs     []int
	ActiveIDs       []int
	LatestActiveID  int
	BaselineApplyID int
	BlobBytes       int
}

type largeRunHistoryFixture struct {
	client          *ent.Client
	recordingClient *ent.Client
	rawDB           *sql.DB
	recorder        *recordingRunDriver
	service         *Service
	recordedService *Service
	source          *ent.DirectorySource
	seed            largeRunHistorySeed
}

func newLargeRunHistoryFixture(t *testing.T) largeRunHistoryFixture {
	t.Helper()
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	source := client.DirectorySource.Create().
		SetName("Large Example Directory").
		SetDescription("Synthetic scale history").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	seed := seedLargeRunHistory(t, client, source.ID)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open recording postgres client: %v", err)
	}
	recorder := &recordingRunDriver{Driver: entsql.OpenDB(dialect.Postgres, db)}
	recordingClient := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() {
		if err := recordingClient.Close(); err != nil {
			t.Errorf("close recording ent client: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, "ANALYZE directory_sync_runs"); err != nil {
		t.Fatalf("analyze seeded directory sync runs: %v", err)
	}

	return largeRunHistoryFixture{
		client:          client,
		recordingClient: recordingClient,
		rawDB:           db,
		recorder:        recorder,
		service:         NewService(client, ServiceOptions{}),
		recordedService: NewService(recordingClient, ServiceOptions{}),
		source:          source,
		seed:            seed,
	}
}

func seedLargeRunHistory(t *testing.T, client *ent.Client, sourceID int) largeRunHistorySeed {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	activeStatuses := map[int]directorysyncrun.Status{
		0: directorysyncrun.StatusQueued,
		3: directorysyncrun.StatusRunning,
		6: directorysyncrun.StatusQueued,
		9: directorysyncrun.StatusRunning,
	}
	terminalStatuses := []directorysyncrun.Status{
		directorysyncrun.StatusCompleted,
		directorysyncrun.StatusCompletedWithWarnings,
		directorysyncrun.StatusFailed,
	}
	modes := []directorysyncrun.Mode{
		directorysyncrun.ModePreview,
		directorysyncrun.ModeApply,
		directorysyncrun.ModeValidate,
	}

	seed := largeRunHistorySeed{Runs: make([]largeRunSeed, 0, largeRunHistoryCount)}
	distinctStartedAt := make(map[time.Time]struct{}, 64)
	modeCounts := make(map[directorysyncrun.Mode]int, len(modes))
	statusCounts := make(map[directorysyncrun.Status]int, 5)

	for batchStart := 0; batchStart < largeRunHistoryCount; batchStart += largeRunHistoryBatchSize {
		creates := make([]*ent.DirectorySyncRunCreate, 0, largeRunHistoryBatchSize)
		batchSeeds := make([]largeRunSeed, 0, largeRunHistoryBatchSize)
		for i := batchStart; i < batchStart+largeRunHistoryBatchSize; i++ {
			mode := modes[i%len(modes)]
			status, active := activeStatuses[i]
			if !active {
				status = terminalStatuses[(i/len(modes))%len(terminalStatuses)]
			}
			phase := directorysyncrun.PhaseCompleted
			switch status {
			case directorysyncrun.StatusQueued:
				phase = directorysyncrun.PhaseValidating
			case directorysyncrun.StatusRunning:
				phase = directorysyncrun.PhaseExecuting
			case directorysyncrun.StatusFailed:
				phase = directorysyncrun.PhaseFailed
			}

			warningMarker := sizedRunMarker("warning-marker", i)
			summaryMarker := sizedRunMarker("summary-marker", i)
			previewMarker := sizedRunMarker("preview-marker", i)
			errorMarker := fmt.Sprintf("error-marker-%04d", i)
			warnings := []map[string]any{{"marker": warningMarker}}
			summary := map[string]any{"marker": summaryMarker}
			previewDiff := map[string]any{"marker": previewMarker}
			seed.BlobBytes += encodedJSONSize(t, warnings) + encodedJSONSize(t, summary) + encodedJSONSize(t, previewDiff) + len(errorMarker)

			create := client.DirectorySyncRun.Create().
				SetSourceID(sourceID).
				SetMode(mode).
				SetTrigger([]directorysyncrun.Trigger{directorysyncrun.TriggerManual, directorysyncrun.TriggerSchedule}[i%2]).
				SetStatus(status).
				SetPhase(phase).
				SetHTTPRequestCount((i % 7) + 1).
				SetDepartmentCount((i % 31) + 1).
				SetMemberCount((i % 211) + 1).
				SetInvalidMemberCount(i % 5).
				SetWarningCount((i % 3) + 1).
				SetWarnings(warnings).
				SetSummary(summary).
				SetPreviewDiff(previewDiff).
				SetErrorMessage(errorMarker)

			var startedAt *time.Time
			if status != directorysyncrun.StatusQueued {
				value := base.Add(time.Duration(i%64) * time.Minute)
				startedAt = &value
				distinctStartedAt[value] = struct{}{}
				create.SetStartedAt(value)
				if status != directorysyncrun.StatusRunning {
					create.SetCompletedAt(value.Add(30 * time.Second))
				}
			}
			creates = append(creates, create)
			batchSeeds = append(batchSeeds, largeRunSeed{Mode: mode, Status: status, StartedAt: startedAt})
			modeCounts[mode]++
			statusCounts[status]++
		}

		created := client.DirectorySyncRun.CreateBulk(creates...).SaveX(ctx)
		if len(created) != largeRunHistoryBatchSize {
			t.Fatalf("created batch size = %d, want %d", len(created), largeRunHistoryBatchSize)
		}
		for i := range created {
			batchSeeds[i].ID = created[i].ID
			seed.Runs = append(seed.Runs, batchSeeds[i])
		}
	}

	if got := client.DirectorySyncRun.Query().Where(directorysyncrun.SourceIDEQ(sourceID)).CountX(ctx); got != largeRunHistoryCount {
		t.Fatalf("seeded run count = %d, want %d", got, largeRunHistoryCount)
	}
	if len(distinctStartedAt) != 64 {
		t.Fatalf("distinct non-null started_at values = %d, want 64", len(distinctStartedAt))
	}
	for _, mode := range modes {
		if modeCounts[mode] != largeRunHistoryCount/len(modes) {
			t.Fatalf("mode %q count = %d, want %d", mode, modeCounts[mode], largeRunHistoryCount/len(modes))
		}
	}
	wantStatusCounts := map[directorysyncrun.Status]int{
		directorysyncrun.StatusQueued:                2,
		directorysyncrun.StatusRunning:               2,
		directorysyncrun.StatusCompleted:             799,
		directorysyncrun.StatusCompletedWithWarnings: 800,
		directorysyncrun.StatusFailed:                797,
	}
	for status, want := range wantStatusCounts {
		if statusCounts[status] != want {
			t.Fatalf("status %q count = %d, want %d", status, statusCounts[status], want)
		}
	}

	ordered := append([]largeRunSeed(nil), seed.Runs...)
	sort.Slice(ordered, func(i, j int) bool { return largeRunSeedBefore(ordered[i], ordered[j]) })
	seed.ExpectedIDs = make([]int, len(ordered))
	for i, run := range ordered {
		seed.ExpectedIDs[i] = run.ID
	}

	active := make([]largeRunSeed, 0, len(activeStatuses))
	for _, run := range seed.Runs {
		if run.Mode == directorysyncrun.ModeApply && (run.Status == directorysyncrun.StatusQueued || run.Status == directorysyncrun.StatusRunning) {
			t.Fatalf("apply run %d is unexpectedly active", run.ID)
		}
		if run.Mode == directorysyncrun.ModeApply && run.Status == directorysyncrun.StatusCompleted && seed.BaselineApplyID == 0 {
			seed.BaselineApplyID = run.ID
		}
		if run.Mode == directorysyncrun.ModePreview && (run.Status == directorysyncrun.StatusQueued || run.Status == directorysyncrun.StatusRunning) {
			active = append(active, run)
			seed.ActiveIDs = append(seed.ActiveIDs, run.ID)
		}
	}
	sort.Slice(active, func(i, j int) bool { return largeRunSeedBefore(active[i], active[j]) })
	if len(active) != len(activeStatuses) {
		t.Fatalf("active preview count = %d, want %d", len(active), len(activeStatuses))
	}
	seed.LatestActiveID = active[0].ID
	if seed.BaselineApplyID == 0 {
		t.Fatal("missing completed apply baseline run")
	}
	return seed
}

func sizedRunMarker(kind string, index int) string {
	prefix := fmt.Sprintf("%s-%04d:", kind, index)
	return prefix + strings.Repeat("x", largeRunDiagnosticBlobSize-len(prefix))
}

func encodedJSONSize(t *testing.T, value any) int {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture diagnostic: %v", err)
	}
	return len(encoded)
}

func largeRunSeedBefore(left, right largeRunSeed) bool {
	if left.StartedAt == nil || right.StartedAt == nil {
		if left.StartedAt == nil && right.StartedAt != nil {
			return true
		}
		if left.StartedAt != nil && right.StartedAt == nil {
			return false
		}
		return left.ID > right.ID
	}
	if !left.StartedAt.Equal(*right.StartedAt) {
		return left.StartedAt.After(*right.StartedAt)
	}
	return left.ID > right.ID
}

type runQueryRole string

const (
	runQueryRoleCount   runQueryRole = "count"
	runQueryRolePrimary runQueryRole = "primary"
	runQueryRoleActive  runQueryRole = "active"
)

func TestRunPageAndLatestActiveShareRepeatableReadSnapshot(t *testing.T) {
	writer, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	source := writer.DirectorySource.Create().
		SetName("Snapshot Example Directory").
		SetDescription("Synthetic snapshot consistency test").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	startedAt := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	run := writer.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode(directorysyncrun.ModeApply).
		SetTrigger(directorysyncrun.TriggerManual).
		SetStatus(directorysyncrun.StatusRunning).
		SetPhase(directorysyncrun.PhaseExecuting).
		SetStartedAt(startedAt).
		SaveX(ctx)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open snapshot reader: %v", err)
	}
	recorder := &recordingRunDriver{Driver: entsql.OpenDB(dialect.Postgres, db)}
	reader := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close snapshot reader: %v", err)
		}
	})

	activeQueryReached := make(chan struct{})
	releaseActiveQuery := make(chan struct{})
	var activeOnce sync.Once
	recorder.setQueryHook(func(query recordedRunQuery) {
		role, err := classifyRunQuerySQL(query.SQL)
		if err == nil && role == runQueryRoleActive {
			activeOnce.Do(func() { close(activeQueryReached) })
			<-releaseActiveQuery
		}
	})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseActiveQuery) }) }
	defer release()

	type listResult struct {
		Page RunPage
		Err  error
	}
	resultCh := make(chan listResult, 1)
	go func() {
		page, err := NewService(reader, ServiceOptions{}).ListRuns(ctx, RunListRequest{SourceID: source.ID})
		resultCh <- listResult{Page: page, Err: err}
	}()

	select {
	case <-activeQueryReached:
	case <-time.After(5 * time.Second):
		t.Fatal("latest-active query did not start")
	}
	completedAt := startedAt.Add(time.Minute)
	writer.DirectorySyncRun.UpdateOneID(run.ID).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(completedAt).
		SaveX(ctx)
	release()

	var result listResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ListRuns did not finish")
	}
	if result.Err != nil {
		t.Fatalf("ListRuns: %v", result.Err)
	}
	if len(result.Page.Items) != 1 || result.Page.Items[0].Status != directorysyncrun.StatusRunning {
		t.Fatalf("page items = %+v, want the running snapshot", result.Page.Items)
	}
	if result.Page.LatestActiveRun == nil || result.Page.LatestActiveRun.ID != run.ID || result.Page.LatestActiveRun.Status != directorysyncrun.StatusRunning {
		t.Fatalf("latest active = %+v, want running run %d from the same snapshot", result.Page.LatestActiveRun, run.ID)
	}
	if got := writer.DirectorySyncRun.GetX(ctx, run.ID).Status; got != directorysyncrun.StatusCompleted {
		t.Fatalf("authoritative run status = %s, want completed", got)
	}
	requireRecordedRunQueryRoles(t, recorder.snapshot())
}

func TestLargeRunHistoryBoundsBytesAndProjection(t *testing.T) {
	fixture := newLargeRunHistoryFixture(t)
	ctx := context.Background()

	defaultPage, err := fixture.service.ListRuns(ctx, RunListRequest{SourceID: fixture.source.ID})
	if err != nil {
		t.Fatalf("ListRuns default page: %v", err)
	}
	requireLargeRunPage(t, defaultPage, fixture.seed.ExpectedIDs[:DefaultRunPageSize], largeRunHistoryCount, 0, DefaultRunPageSize, fixture.seed.LatestActiveID)

	maximumPage, err := fixture.service.ListRuns(ctx, RunListRequest{SourceID: fixture.source.ID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListRuns maximum page: %v", err)
	}
	requireLargeRunPage(t, maximumPage, fixture.seed.ExpectedIDs[:MaxRunPageSize], largeRunHistoryCount, 0, MaxRunPageSize, fixture.seed.LatestActiveID)

	latePage, err := fixture.service.ListRuns(ctx, RunListRequest{SourceID: fixture.source.ID, Limit: 100, Offset: 2200})
	if err != nil {
		t.Fatalf("ListRuns late page: %v", err)
	}
	requireLargeRunPage(t, latePage, fixture.seed.ExpectedIDs[2200:2300], largeRunHistoryCount, 22, 100, fixture.seed.LatestActiveID)

	encoded, err := json.Marshal(maximumPage)
	if err != nil {
		t.Fatalf("marshal 100-row run page: %v", err)
	}
	if len(encoded) >= largeRunResponseByteLimit {
		t.Fatalf("100-row RunPage bytes = %d, want < %d", len(encoded), largeRunResponseByteLimit)
	}
	for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("100-row RunPage contains diagnostic key %q", key)
		}
	}
	for _, marker := range []string{"warning-marker-", "summary-marker-", "preview-marker-", "error-marker-"} {
		if strings.Contains(string(encoded), marker) {
			t.Fatalf("100-row RunPage contains diagnostic marker %q", marker)
		}
	}
	for _, item := range maximumPage.Items {
		requireRunSummaryJSONKeys(t, item)
	}
	if got := len(sizedRunMarker("warning-marker", 0)); got != largeRunDiagnosticBlobSize {
		t.Fatalf("diagnostic marker bytes = %d, want %d", got, largeRunDiagnosticBlobSize)
	}
	t.Logf("fixture_runs=%d fixture_batch=%d marker_bytes=%d fixture_blob_bytes=%d dto_bytes=%d", largeRunHistoryCount, largeRunHistoryBatchSize, largeRunDiagnosticBlobSize, fixture.seed.BlobBytes, len(encoded))
}

func TestLargeRunHistoryStablePages(t *testing.T) {
	fixture := newLargeRunHistoryFixture(t)
	ctx := context.Background()

	for _, limit := range []int{20, 50, 100} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			gotIDs := make([]int, 0, largeRunHistoryCount)
			seen := make(map[int]struct{}, largeRunHistoryCount)
			for offset := 0; offset < largeRunHistoryCount; offset += limit {
				page, err := fixture.service.ListRuns(ctx, RunListRequest{SourceID: fixture.source.ID, Limit: limit, Offset: offset})
				if err != nil {
					t.Fatalf("ListRuns limit=%d offset=%d: %v", limit, offset, err)
				}
				wantEnd := offset + limit
				if wantEnd > largeRunHistoryCount {
					wantEnd = largeRunHistoryCount
				}
				requireLargeRunPage(t, page, fixture.seed.ExpectedIDs[offset:wantEnd], largeRunHistoryCount, offset/limit, limit, fixture.seed.LatestActiveID)
				for _, item := range page.Items {
					if _, duplicate := seen[item.ID]; duplicate {
						t.Fatalf("run id %d repeated at offset %d", item.ID, offset)
					}
					seen[item.ID] = struct{}{}
					gotIDs = append(gotIDs, item.ID)
				}
			}
			if len(gotIDs) != largeRunHistoryCount {
				t.Fatalf("traversed ids = %d, want %d", len(gotIDs), largeRunHistoryCount)
			}
			for i, id := range fixture.seed.ExpectedIDs {
				if gotIDs[i] != id {
					t.Fatalf("traversed id[%d] = %d, want %d", i, gotIDs[i], id)
				}
			}
			t.Logf("limit=%d pages=%d unique_ids=%d", limit, largeRunHistoryCount/limit, len(seen))
		})
	}
}

func TestLargeRunHistoryDetailRemainsComplete(t *testing.T) {
	fixture := newLargeRunHistoryFixture(t)
	ctx := context.Background()
	const detailIndex = 1234
	detailSeed := fixture.seed.Runs[detailIndex]

	run, err := fixture.service.GetRun(ctx, detailSeed.ID)
	if err != nil {
		t.Fatalf("GetRun(%d): %v", detailSeed.ID, err)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal run detail: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode run detail: %v", err)
	}
	for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("detail missing diagnostic key %q", key)
		}
	}
	for _, marker := range []string{
		sizedRunMarker("warning-marker", detailIndex),
		sizedRunMarker("summary-marker", detailIndex),
		sizedRunMarker("preview-marker", detailIndex),
		fmt.Sprintf("error-marker-%04d", detailIndex),
	} {
		if !strings.Contains(string(encoded), marker) {
			t.Fatalf("detail %d missing marker prefix %q", run.ID, marker[:min(len(marker), 32)])
		}
	}
	t.Logf("detail_id=%d detail_bytes=%d marker_bytes=%d", run.ID, len(encoded), largeRunDiagnosticBlobSize)
}

func TestLargeRunHistoryQueryPlans(t *testing.T) {
	fixture := newLargeRunHistoryFixture(t)
	ctx := context.Background()
	request := RunListRequest{SourceID: fixture.source.ID, Limit: 100, Offset: 400}

	fixture.recorder.reset()
	page, err := fixture.recordedService.ListRuns(ctx, request)
	if err != nil {
		t.Fatalf("record ListRuns: %v", err)
	}
	requireLargeRunPage(t, page, fixture.seed.ExpectedIDs[400:500], largeRunHistoryCount, 4, 100, fixture.seed.LatestActiveID)
	queries := requireRecordedRunQueryRoles(t, fixture.recorder.snapshot())
	for role, query := range queries {
		if err := validateRunQuerySQL(role, query.SQL); err != nil {
			t.Fatalf("validate recorded %s SQL: %v\n%s", role, err, query.SQL)
		}
		requireRunQueryArguments(t, role, query, request)
	}

	countPlan := explainRecordedRunQuery(t, fixture.rawDB, queries[runQueryRoleCount])
	if !countPlan.hasNodeType("Aggregate") {
		t.Fatalf("count plan has no Aggregate: %s", countPlan.summary())
	}
	primaryPlan := explainRecordedRunQuery(t, fixture.rawDB, queries[runQueryRolePrimary])
	limits := primaryPlan.nodesByType("Limit")
	if len(limits) == 0 || limits[0].ActualRows > MaxRunPageSize {
		t.Fatalf("primary plan Limit rows = %v, want <= %d: %s", nodeActualRows(limits), MaxRunPageSize, primaryPlan.summary())
	}
	const generalRunIndex = "directorysyncrun_source_id_started_at_id"
	if !primaryPlan.hasIndex(generalRunIndex) {
		t.Fatalf("primary plan indexes = %v, want %s: %s", primaryPlan.indexNames(), generalRunIndex, primaryPlan.summary())
	}

	activePresentPlan := explainRecordedRunQuery(t, fixture.rawDB, queries[runQueryRoleActive])
	const partialActiveRunIndex = "directory_sync_runs_active_started_id"
	if !activePresentPlan.hasIndex(partialActiveRunIndex) {
		t.Fatalf("active-present plan indexes = %v, want %s: %s", activePresentPlan.indexNames(), partialActiveRunIndex, activePresentPlan.summary())
	}
	if activePresentPlan.hasNodeType("Sort") {
		t.Fatalf("active-present plan contains Sort: %s", activePresentPlan.summary())
	}
	if activePresentPlan.maxMaterializedRows() > 1 {
		t.Fatalf("active-present max actual rows = %.0f, want <= 1: %s", activePresentPlan.maxMaterializedRows(), activePresentPlan.summary())
	}

	if err := fixture.client.DirectorySyncRun.Update().
		Where(directorysyncrun.IDIn(fixture.seed.ActiveIDs...)).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		Exec(ctx); err != nil {
		t.Fatalf("terminalize active preview rows: %v", err)
	}
	if got := fixture.client.DirectorySyncRun.Query().Where(directorysyncrun.SourceIDEQ(fixture.source.ID)).CountX(ctx); got != largeRunHistoryCount {
		t.Fatalf("history count after terminalizing active rows = %d, want %d", got, largeRunHistoryCount)
	}
	if _, err := fixture.rawDB.ExecContext(ctx, "ANALYZE directory_sync_runs"); err != nil {
		t.Fatalf("reanalyze terminal run history: %v", err)
	}

	fixture.recorder.reset()
	withoutActive, err := fixture.recordedService.ListRuns(ctx, request)
	if err != nil {
		t.Fatalf("record ListRuns without active rows: %v", err)
	}
	if withoutActive.LatestActiveRun != nil {
		t.Fatalf("latest_active_run = %+v after terminalization, want nil", withoutActive.LatestActiveRun)
	}
	terminalQueries := requireRecordedRunQueryRoles(t, fixture.recorder.snapshot())
	terminalActiveQuery := terminalQueries[runQueryRoleActive]
	if err := validateRunQuerySQL(runQueryRoleActive, terminalActiveQuery.SQL); err != nil {
		t.Fatalf("validate recorded no-active SQL: %v\n%s", err, terminalActiveQuery.SQL)
	}
	requireRunQueryArguments(t, runQueryRoleActive, terminalActiveQuery, request)
	activeAbsentPlan := explainRecordedRunQuery(t, fixture.rawDB, terminalActiveQuery)
	if activeAbsentPlan.hasNodeType("Sort") {
		t.Fatalf("no-active plan contains Sort: %s", activeAbsentPlan.summary())
	}
	if activeAbsentPlan.maxMaterializedRows() != 0 {
		t.Fatalf("no-active max actual rows = %.0f, want 0: %s", activeAbsentPlan.maxMaterializedRows(), activeAbsentPlan.summary())
	}
	if activeAbsentPlan.rowsRemovedByFilter() != 0 {
		t.Fatalf("no-active rows removed by filter = %.0f, want 0: %s", activeAbsentPlan.rowsRemovedByFilter(), activeAbsentPlan.summary())
	}
	if !activeAbsentPlan.hasAnyIndex(partialActiveRunIndex, "directorysyncrun_source_id_status") {
		t.Fatalf("no-active plan indexes = %v, want predicate-compatible active or source/status index: %s", activeAbsentPlan.indexNames(), activeAbsentPlan.summary())
	}

	t.Logf("count_plan=%s", countPlan.summary())
	t.Logf("primary_plan=%s", primaryPlan.summary())
	t.Logf("active_present_plan=%s", activePresentPlan.summary())
	t.Logf("active_absent_plan=%s", activeAbsentPlan.summary())
}

func TestLargeRunHistoryPreservesPreviewAndApplyStateSemantics(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server, serverState := newSequencedLargeHistoryDirectoryServer(t)
	t.Cleanup(server.Close)
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	seed := seedLargeRunHistory(t, client, source.ID)

	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(seed.BaselineApplyID).
		SetLastSuccessfulRunID(seed.BaselineApplyID).
		SaveX(ctx)
	legacyDepartment := client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-legacy").
		SetName("Department Legacy").
		SetPath("Department Legacy").
		SetLastSeenRunID(seed.BaselineApplyID).
		SaveX(ctx)
	legacyMember := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-legacy").
		SetEmailNormalized("legacy@example.org").
		SetDisplayName("Legacy Member").
		SetDepartmentExternalID(legacyDepartment.ExternalID).
		SetLastSeenRunID(seed.BaselineApplyID).
		SaveX(ctx)
	client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(legacyMember.ID).
		SetMemberExternalID(legacyMember.ExternalID).
		SetMemberEmailNormalized(legacyMember.EmailNormalized).
		SetDepartmentExternalID(legacyDepartment.ExternalID).
		SetLastSeenRunID(seed.BaselineApplyID).
		SaveX(ctx)
	relayUserID := 7001
	candidateUser := client.User.Create().
		SetUsername("departed-user").
		SetEmail("departed@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(relayUserID).
		SaveX(ctx)
	action := client.DirectoryOffboardingAction.Create().
		SetSourceID(source.ID).
		SetUserID(candidateUser.ID).
		SetRelayUserID(relayUserID).
		SetDirectoryRunID(seed.BaselineApplyID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(directoryoffboardingaction.StatusFailed).
		SetReason(offboardingReasonMissingFromDirectory).
		SetErrorMessage("synthetic upstream rejection").
		SetPerformedByUserID(candidateUser.ID).
		SaveX(ctx)

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	service := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	})
	initialFacts := captureDirectoryFacts(t, client, source.ID)
	initialOffboarding := captureOffboardingState(t, service, client, source.ID)
	if len(initialOffboarding.Candidates) != 1 || len(initialOffboarding.Actions) != 1 {
		t.Fatalf("initial offboarding state = %+v, want one candidate and one action", initialOffboarding)
	}
	initialLastRunID, initialLastSuccessfulRunID := sourceRunPointers(t, client, source.ID)
	if initialLastRunID != seed.BaselineApplyID || initialLastSuccessfulRunID != seed.BaselineApplyID {
		t.Fatalf("initial source pointers = %d/%d, want %d/%d", initialLastRunID, initialLastSuccessfulRunID, seed.BaselineApplyID, seed.BaselineApplyID)
	}

	preview := runAndExecuteLargeHistorySource(t, service, source.ID, directorysyncrun.ModePreview, false)
	if got := captureDirectoryFacts(t, client, source.ID); !reflect.DeepEqual(got, initialFacts) {
		t.Fatalf("facts changed after preview:\ngot  %+v\nwant %+v", got, initialFacts)
	}
	if got := captureOffboardingState(t, service, client, source.ID); !reflect.DeepEqual(got, initialOffboarding) {
		t.Fatalf("offboarding state changed after preview:\ngot  %+v\nwant %+v", got, initialOffboarding)
	}
	lastRunID, lastSuccessfulRunID := sourceRunPointers(t, client, source.ID)
	if lastRunID != preview.ID || lastSuccessfulRunID != seed.BaselineApplyID {
		t.Fatalf("pointers after preview = %d/%d, want %d/%d", lastRunID, lastSuccessfulRunID, preview.ID, seed.BaselineApplyID)
	}

	failedApply := runAndExecuteLargeHistorySource(t, service, source.ID, directorysyncrun.ModeApply, true)
	if failedApply.Status != directorysyncrun.StatusFailed {
		t.Fatalf("failed apply status = %q, want failed", failedApply.Status)
	}
	if got := captureDirectoryFacts(t, client, source.ID); !reflect.DeepEqual(got, initialFacts) {
		t.Fatalf("facts changed after failed apply:\ngot  %+v\nwant %+v", got, initialFacts)
	}
	if got := captureOffboardingState(t, service, client, source.ID); !reflect.DeepEqual(got, initialOffboarding) {
		t.Fatalf("offboarding state changed after failed apply:\ngot  %+v\nwant %+v", got, initialOffboarding)
	}
	lastRunID, lastSuccessfulRunID = sourceRunPointers(t, client, source.ID)
	if lastRunID != preview.ID || lastSuccessfulRunID != seed.BaselineApplyID {
		t.Fatalf("pointers after failed apply = %d/%d, want unchanged %d/%d", lastRunID, lastSuccessfulRunID, preview.ID, seed.BaselineApplyID)
	}

	successfulApply := runAndExecuteLargeHistorySource(t, service, source.ID, directorysyncrun.ModeApply, false)
	successFacts := captureDirectoryFacts(t, client, source.ID)
	wantSuccessFacts := directoryFactsState{
		Departments: []string{fmt.Sprintf("dept-alpha|||Department Alpha|Department Alpha|%d", successfulApply.ID)},
		Members:     []string{fmt.Sprintf("alice@example.com|dept-alpha|%d", successfulApply.ID)},
		Memberships: []string{fmt.Sprintf("alice@example.com|dept-alpha|%d", successfulApply.ID)},
	}
	if !reflect.DeepEqual(successFacts, wantSuccessFacts) {
		t.Fatalf("facts after successful apply:\ngot  %+v\nwant %+v", successFacts, wantSuccessFacts)
	}
	lastRunID, lastSuccessfulRunID = sourceRunPointers(t, client, source.ID)
	if lastRunID != successfulApply.ID || lastSuccessfulRunID != successfulApply.ID {
		t.Fatalf("pointers after successful apply = %d/%d, want %d/%d", lastRunID, lastSuccessfulRunID, successfulApply.ID, successfulApply.ID)
	}
	reloadedAction := client.DirectoryOffboardingAction.GetX(ctx, action.ID)
	if reloadedAction.Status != directoryoffboardingaction.StatusFailed || reloadedAction.DirectoryRunID != seed.BaselineApplyID {
		t.Fatalf("offboarding action after successful apply = %+v, want original failed action", reloadedAction)
	}
	if got := client.DirectorySyncRun.Query().Where(directorysyncrun.SourceIDEQ(source.ID)).CountX(ctx); got != largeRunHistoryCount+3 {
		t.Fatalf("run count after preview/failed apply/successful apply = %d, want %d", got, largeRunHistoryCount+3)
	}
	requestCount, sequenceErrors := serverState()
	if requestCount != 5 || len(sequenceErrors) != 0 {
		t.Fatalf("executor request sequence = count:%d errors:%v, want five deterministic requests", requestCount, sequenceErrors)
	}
	t.Logf("state_semantics=preview_preserved failed_apply_preserved successful_apply_replaced history_rows=%d preview_id=%d failed_apply_id=%d successful_apply_id=%d", largeRunHistoryCount, preview.ID, failedApply.ID, successfulApply.ID)
}

type directoryFactsState struct {
	Departments []string
	Members     []string
	Memberships []string
}

type directoryOffboardingState struct {
	Actions    []string
	Candidates []string
}

func captureDirectoryFacts(t *testing.T, client *ent.Client, sourceID int) directoryFactsState {
	t.Helper()
	ctx := context.Background()
	state := directoryFactsState{
		Departments: make([]string, 0),
		Members:     make([]string, 0),
		Memberships: make([]string, 0),
	}
	departments := client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorydepartment.FieldExternalID)).
		AllX(ctx)
	for _, department := range departments {
		state.Departments = append(state.Departments, fmt.Sprintf("%s|%s|%s|%s|%s|%d",
			department.ExternalID,
			optionalString(department.ParentExternalID),
			optionalString(department.EffectiveParentExternalID),
			department.Name,
			department.Path,
			department.LastSeenRunID,
		))
	}
	members := client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymember.FieldEmailNormalized)).
		AllX(ctx)
	for _, member := range members {
		state.Members = append(state.Members, fmt.Sprintf("%s|%s|%d", member.EmailNormalized, member.DepartmentExternalID, member.LastSeenRunID))
	}
	memberships := client.DirectoryMemberDepartment.Query().
		Where(directorymemberdepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymemberdepartment.FieldMemberEmailNormalized), ent.Asc(directorymemberdepartment.FieldDepartmentExternalID)).
		AllX(ctx)
	for _, membership := range memberships {
		state.Memberships = append(state.Memberships, fmt.Sprintf("%s|%s|%d", membership.MemberEmailNormalized, membership.DepartmentExternalID, membership.LastSeenRunID))
	}
	return state
}

func captureOffboardingState(t *testing.T, service *Service, client *ent.Client, sourceID int) directoryOffboardingState {
	t.Helper()
	ctx := context.Background()
	state := directoryOffboardingState{Actions: make([]string, 0), Candidates: make([]string, 0)}
	actions := client.DirectoryOffboardingAction.Query().
		Where(directoryoffboardingaction.SourceIDEQ(sourceID)).
		Order(ent.Asc(directoryoffboardingaction.FieldID)).
		AllX(ctx)
	for _, action := range actions {
		state.Actions = append(state.Actions, fmt.Sprintf("%d|%d|%s|%d", action.ID, action.UserID, action.Status, action.DirectoryRunID))
	}
	candidates, err := service.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{
		SourceID: sourceID,
		Page:     1,
		PageSize: maxOffboardingPageSize,
	})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	for _, candidate := range candidates.Items {
		actionID := 0
		if candidate.OffboardingActionID != nil {
			actionID = *candidate.OffboardingActionID
		}
		state.Candidates = append(state.Candidates, fmt.Sprintf("%d|%s|%d|%s|%d", candidate.UserID, candidate.Reason, candidate.DirectoryRunID, candidate.OffboardingStatus, actionID))
	}
	return state
}

func sourceRunPointers(t *testing.T, client *ent.Client, sourceID int) (int, int) {
	t.Helper()
	source := client.DirectorySource.GetX(context.Background(), sourceID)
	lastRunID, lastSuccessfulRunID := 0, 0
	if source.LastRunID != nil {
		lastRunID = *source.LastRunID
	}
	if source.LastSuccessfulRunID != nil {
		lastSuccessfulRunID = *source.LastSuccessfulRunID
	}
	return lastRunID, lastSuccessfulRunID
}

func runAndExecuteLargeHistorySource(t *testing.T, service *Service, sourceID int, mode directorysyncrun.Mode, wantFailure bool) *ent.DirectorySyncRun {
	t.Helper()
	run, err := service.RunSource(context.Background(), sourceID, string(mode), string(directorysyncrun.TriggerManual))
	if err != nil {
		t.Fatalf("RunSource(%s): %v", mode, err)
	}
	completed, err := service.ExecuteRun(context.Background(), run.ID)
	if wantFailure {
		if err == nil {
			t.Fatalf("ExecuteRun(%s) succeeded, want injected failure", mode)
		}
		return completed
	}
	if err != nil {
		t.Fatalf("ExecuteRun(%s): %v", mode, err)
	}
	return completed
}

func newSequencedLargeHistoryDirectoryServer(t *testing.T) (*httptest.Server, func() (int, []string)) {
	t.Helper()
	var mu sync.Mutex
	requestCount := 0
	var sequenceErrors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		requestNumber := requestCount
		if r.Header.Get("X-Directory-API-Key") != "test-directory-secret" {
			sequenceErrors = append(sequenceErrors, fmt.Sprintf("request %d missing synthetic credential", requestNumber))
		}
		mu.Unlock()

		if requestNumber == 3 {
			if r.URL.Path != "/departments" {
				mu.Lock()
				sequenceErrors = append(sequenceErrors, fmt.Sprintf("request 3 path = %s, want /departments", r.URL.Path))
				mu.Unlock()
			}
			http.Error(w, "synthetic apply failure", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/departments":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"departments": []map[string]any{{
				"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha",
			}}}})
		case "/users":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"users": []map[string]any{{
				"id": "member-alice", "email": "alice@example.com", "name": "Alice", "status": "active",
			}}}})
		default:
			mu.Lock()
			sequenceErrors = append(sequenceErrors, fmt.Sprintf("request %d unexpected path %s", requestNumber, r.URL.Path))
			mu.Unlock()
			http.NotFound(w, r)
		}
	}))
	return server, func() (int, []string) {
		mu.Lock()
		defer mu.Unlock()
		return requestCount, append([]string(nil), sequenceErrors...)
	}
}

func requireRecordedRunQueryRoles(t *testing.T, queries []recordedRunQuery) map[runQueryRole]recordedRunQuery {
	t.Helper()
	roles := make(map[runQueryRole]recordedRunQuery, 3)
	for _, query := range queries {
		role, err := classifyRunQuerySQL(query.SQL)
		if err != nil {
			t.Fatalf("classify recorded SQL: %v\n%s", err, query.SQL)
		}
		if previous, duplicate := roles[role]; duplicate {
			t.Fatalf("duplicate %s query captured:\nfirst: %s\nsecond: %s", role, previous.SQL, query.SQL)
		}
		roles[role] = query
	}
	for _, role := range []runQueryRole{runQueryRoleCount, runQueryRolePrimary, runQueryRoleActive} {
		if _, ok := roles[role]; !ok {
			t.Fatalf("missing recorded %s query; captured %d queries", role, len(queries))
		}
	}
	if len(roles) != 3 || len(queries) != 3 {
		t.Fatalf("recorded query count = %d roles=%d, want exactly 3", len(queries), len(roles))
	}
	return roles
}

func classifyRunQuerySQL(query string) (runQueryRole, error) {
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	if !strings.HasPrefix(normalized, "select ") || !strings.Contains(normalized, `"directory_sync_runs"`) {
		return "", fmt.Errorf("not a directory sync run SELECT")
	}
	if strings.Contains(normalized, "count(") {
		return runQueryRoleCount, nil
	}
	hasModePredicate := regexp.MustCompile(`\."mode"\s+in\s*\(`).MatchString(normalized)
	hasStatusPredicate := regexp.MustCompile(`\."status"\s+in\s*\(`).MatchString(normalized)
	if hasModePredicate || hasStatusPredicate {
		if !hasModePredicate || !hasStatusPredicate {
			return "", fmt.Errorf("partial active query predicates")
		}
		return runQueryRoleActive, nil
	}
	return runQueryRolePrimary, nil
}

func requireRunQueryArguments(t *testing.T, role runQueryRole, query recordedRunQuery, request RunListRequest) {
	t.Helper()
	var want []string
	switch role {
	case runQueryRoleCount:
		want = []string{fmt.Sprint(request.SourceID)}
	case runQueryRolePrimary:
		want = []string{fmt.Sprint(request.SourceID)}
		bounds := regexp.MustCompile(`(?i)\blimit\s+` + fmt.Sprint(request.Limit) + `\s+offset\s+` + fmt.Sprint(request.Offset) + `\b`)
		if !bounds.MatchString(query.SQL) {
			t.Fatalf("primary query missing exact LIMIT %d OFFSET %d: %s", request.Limit, request.Offset, query.SQL)
		}
	case runQueryRoleActive:
		want = []string{
			fmt.Sprint(request.SourceID),
			string(directorysyncrun.ModePreview),
			string(directorysyncrun.ModeApply),
			string(directorysyncrun.StatusQueued),
			string(directorysyncrun.StatusRunning),
		}
		if !regexp.MustCompile(`(?i)\blimit\s+1\b`).MatchString(query.SQL) {
			t.Fatalf("active query missing exact LIMIT 1: %s", query.SQL)
		}
	default:
		t.Fatalf("unknown query role %q", role)
	}
	got := make([]string, len(query.Args))
	for i, arg := range query.Args {
		got[i] = fmt.Sprint(arg)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s query args = %v, want %v", role, got, want)
	}
}

type explainRunPlan struct {
	NodeType            string           `json:"Node Type"`
	IndexName           string           `json:"Index Name"`
	ActualRows          float64          `json:"Actual Rows"`
	ActualLoops         float64          `json:"Actual Loops"`
	RowsRemovedByFilter float64          `json:"Rows Removed by Filter"`
	Plans               []explainRunPlan `json:"Plans"`
}

func explainRecordedRunQuery(t *testing.T, db *sql.DB, query recordedRunQuery) explainRunPlan {
	t.Helper()
	var encoded []byte
	if err := db.QueryRowContext(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.SQL, query.Args...).Scan(&encoded); err != nil {
		t.Fatalf("EXPLAIN recorded query: %v\n%s\nargs=%v", err, query.SQL, query.Args)
	}
	var documents []struct {
		Plan explainRunPlan `json:"Plan"`
	}
	if err := json.Unmarshal(encoded, &documents); err != nil {
		t.Fatalf("decode EXPLAIN JSON: %v\n%s", err, encoded)
	}
	if len(documents) != 1 {
		t.Fatalf("EXPLAIN documents = %d, want 1", len(documents))
	}
	return documents[0].Plan
}

func (p explainRunPlan) walk(visit func(explainRunPlan)) {
	visit(p)
	for _, child := range p.Plans {
		child.walk(visit)
	}
}

func (p explainRunPlan) hasNodeType(nodeType string) bool {
	found := false
	p.walk(func(node explainRunPlan) {
		if node.NodeType == nodeType {
			found = true
		}
	})
	return found
}

func (p explainRunPlan) nodesByType(nodeType string) []explainRunPlan {
	var nodes []explainRunPlan
	p.walk(func(node explainRunPlan) {
		if node.NodeType == nodeType {
			nodes = append(nodes, node)
		}
	})
	return nodes
}

func (p explainRunPlan) indexNames() []string {
	seen := make(map[string]struct{})
	p.walk(func(node explainRunPlan) {
		if node.IndexName != "" {
			seen[node.IndexName] = struct{}{}
		}
	})
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p explainRunPlan) hasIndex(name string) bool {
	return p.hasAnyIndex(name)
}

func (p explainRunPlan) hasAnyIndex(names ...string) bool {
	want := make(map[string]struct{}, len(names))
	for _, name := range names {
		want[name] = struct{}{}
	}
	for _, name := range p.indexNames() {
		if _, ok := want[name]; ok {
			return true
		}
	}
	return false
}

func (p explainRunPlan) maxMaterializedRows() float64 {
	maximum := float64(0)
	p.walk(func(node explainRunPlan) {
		rows := node.ActualRows * node.ActualLoops
		if rows > maximum {
			maximum = rows
		}
	})
	return maximum
}

func (p explainRunPlan) rowsRemovedByFilter() float64 {
	total := float64(0)
	p.walk(func(node explainRunPlan) {
		total += node.RowsRemovedByFilter * node.ActualLoops
	})
	return total
}

func (p explainRunPlan) summary() string {
	parts := make([]string, 0, 4)
	p.walk(func(node explainRunPlan) {
		part := fmt.Sprintf("%s(rows=%.0f loops=%.0f removed=%.0f", node.NodeType, node.ActualRows, node.ActualLoops, node.RowsRemovedByFilter)
		if node.IndexName != "" {
			part += " index=" + node.IndexName
		}
		parts = append(parts, part+")")
	})
	return strings.Join(parts, " -> ")
}

func nodeActualRows(nodes []explainRunPlan) []float64 {
	rows := make([]float64, len(nodes))
	for i, node := range nodes {
		rows[i] = node.ActualRows
	}
	return rows
}

func requireLargeRunPage(t *testing.T, page RunPage, wantIDs []int, total, wantPage, pageSize, latestActiveID int) {
	t.Helper()
	if page.Total != total || page.Page != wantPage || page.PageSize != pageSize {
		t.Fatalf("page metadata = total:%d page:%d page_size:%d, want total:%d page:%d page_size:%d", page.Total, page.Page, page.PageSize, total, wantPage, pageSize)
	}
	if len(page.Items) != len(wantIDs) {
		t.Fatalf("item count = %d, want %d", len(page.Items), len(wantIDs))
	}
	for i, item := range page.Items {
		if item.ID != wantIDs[i] {
			t.Fatalf("item[%d].id = %d, want %d", i, item.ID, wantIDs[i])
		}
	}
	if page.LatestActiveRun == nil || page.LatestActiveRun.ID != latestActiveID {
		t.Fatalf("latest_active_run = %+v, want id %d", page.LatestActiveRun, latestActiveID)
	}
}

func TestLargeRunHistorySQLValidationRejectsSyntheticBadSQL(t *testing.T) {
	projection := strings.Join([]string{
		`"r"."id"`,
		`"r"."source_id"`,
		`"r"."mode"`,
		`"r"."trigger"`,
		`"r"."status"`,
		`"r"."phase"`,
		`"r"."started_at"`,
		`"r"."completed_at"`,
		`"r"."http_request_count"`,
		`"r"."department_count"`,
		`"r"."member_count"`,
		`"r"."invalid_member_count"`,
		`"r"."warning_count"`,
	}, ", ")
	primary := `SELECT ` + projection + ` FROM "directory_sync_runs" AS "r" WHERE "r"."source_id" = $1 ORDER BY "r"."started_at" DESC, "r"."id" DESC LIMIT $2 OFFSET $3`
	active := `SELECT ` + projection + ` FROM "directory_sync_runs" AS "r" WHERE "r"."source_id" = $1 AND "r"."mode" IN ($2, $3) AND "r"."status" IN ($4, $5) ORDER BY "r"."started_at" DESC, "r"."id" DESC LIMIT $6`
	count := `SELECT COUNT("r"."id") FROM "directory_sync_runs" AS "r" WHERE "r"."source_id" = $1`
	for role, query := range map[runQueryRole]string{
		runQueryRoleCount:   count,
		runQueryRolePrimary: primary,
		runQueryRoleActive:  active,
	} {
		if err := validateRunQuerySQL(role, query); err != nil {
			t.Fatalf("validate good %s SQL: %v", role, err)
		}
	}

	tests := []struct {
		name    string
		role    runQueryRole
		query   string
		wantErr string
	}{
		{
			name:    "extra sort expression",
			role:    runQueryRolePrimary,
			query:   strings.Replace(primary, `"r"."id" DESC`, `"r"."id" DESC, "r"."created_at" DESC`, 1),
			wantErr: `primary query order = "started_at DESC, id DESC, created_at DESC", want "started_at DESC, id DESC"`,
		},
		{
			name:    "selected diagnostic column",
			role:    runQueryRolePrimary,
			query:   strings.Replace(primary, `"r"."warning_count"`, `"r"."warning_count", "r"."error_message"`, 1),
			wantErr: `primary query selects diagnostic field "error_message"`,
		},
		{
			name:    "missing active predicates",
			role:    runQueryRoleActive,
			query:   strings.Replace(active, ` AND "r"."mode" IN ($2, $3) AND "r"."status" IN ($4, $5)`, "", 1),
			wantErr: "active query missing mode/status predicates",
		},
		{
			name:    "missing limit",
			role:    runQueryRolePrimary,
			query:   strings.Replace(primary, " LIMIT $2", "", 1),
			wantErr: "primary query missing LIMIT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunQuerySQL(tt.role, tt.query)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validation error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

var qualifiedRunColumnPattern = regexp.MustCompile(`"[^"]+"\."([^"]+)"`)

func validateRunQuerySQL(role runQueryRole, query string) error {
	normalized := strings.Join(strings.Fields(query), " ")
	lower := strings.ToLower(normalized)
	selectClause, ok := sqlClause(normalized, lower, "select ", " from ")
	if !ok {
		return fmt.Errorf("%s query missing SELECT/FROM", role)
	}
	selectedFields := qualifiedRunColumns(selectClause)
	for _, field := range selectedFields {
		switch field {
		case directorysyncrun.FieldWarnings,
			directorysyncrun.FieldSummary,
			directorysyncrun.FieldPreviewDiff,
			directorysyncrun.FieldErrorMessage:
			return fmt.Errorf("%s query selects diagnostic field %q", role, field)
		}
	}

	if role == runQueryRoleCount {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(selectClause)), "count(") || len(selectedFields) != 1 || selectedFields[0] != directorysyncrun.FieldID {
			return fmt.Errorf("count query must aggregate only run id")
		}
		return nil
	}
	if len(selectedFields) != len(runSummaryFields) {
		return fmt.Errorf("%s query projection has %d fields, want %d", role, len(selectedFields), len(runSummaryFields))
	}
	for i, field := range runSummaryFields {
		if selectedFields[i] != field {
			return fmt.Errorf("%s query projection field %d = %q, want %q", role, i, selectedFields[i], field)
		}
	}
	if !regexp.MustCompile(`(?i)\blimit\s+(\$[0-9]+|[0-9]+)`).MatchString(normalized) {
		return fmt.Errorf("%s query missing LIMIT", role)
	}
	if role == runQueryRoleActive && (!regexp.MustCompile(`(?i)\."mode"\s+in\s*\(`).MatchString(normalized) || !regexp.MustCompile(`(?i)\."status"\s+in\s*\(`).MatchString(normalized)) {
		return fmt.Errorf("active query missing mode/status predicates")
	}

	orderClause, ok := sqlClause(normalized, lower, " order by ", " limit ")
	if !ok {
		return fmt.Errorf("%s query missing ORDER BY", role)
	}
	gotOrder := normalizedRunOrder(orderClause)
	const wantOrder = "started_at DESC, id DESC"
	if gotOrder != wantOrder {
		return fmt.Errorf("%s query order = %q, want %q", role, gotOrder, wantOrder)
	}
	return nil
}

func sqlClause(original, lower, start, end string) (string, bool) {
	startIndex := strings.Index(lower, start)
	if startIndex < 0 {
		return "", false
	}
	startIndex += len(start)
	endOffset := strings.Index(lower[startIndex:], end)
	if endOffset < 0 {
		return "", false
	}
	return strings.TrimSpace(original[startIndex : startIndex+endOffset]), true
}

func qualifiedRunColumns(clause string) []string {
	matches := qualifiedRunColumnPattern.FindAllStringSubmatch(clause, -1)
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		columns = append(columns, strings.ToLower(match[1]))
	}
	return columns
}

func normalizedRunOrder(clause string) string {
	expressions := strings.Split(clause, ",")
	normalized := make([]string, 0, len(expressions))
	for _, expression := range expressions {
		columns := qualifiedRunColumns(expression)
		if len(columns) != 1 {
			normalized = append(normalized, strings.TrimSpace(expression))
			continue
		}
		direction := ""
		fields := strings.Fields(expression)
		if len(fields) > 0 {
			last := strings.ToUpper(fields[len(fields)-1])
			if last == "ASC" || last == "DESC" {
				direction = " " + last
			}
		}
		normalized = append(normalized, columns[0]+direction)
	}
	return strings.Join(normalized, ", ")
}
