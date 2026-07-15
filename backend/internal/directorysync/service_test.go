package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeTokenRevoker struct {
	calls []revocationCall
}

type revocationCall struct {
	UserID    int
	RevokedAt time.Time
}

func (f *fakeTokenRevoker) RevokeUserTokens(_ context.Context, userID int, revokedAt time.Time) error {
	f.calls = append(f.calls, revocationCall{UserID: userID, RevokedAt: revokedAt})
	return nil
}

type fakeRelayDisablerResolver struct {
	disabler relay.UserDisabler
}

func (f fakeRelayDisablerResolver) ResolveRelayDisabler(_ context.Context, _ int) (relay.UserDisabler, error) {
	return f.disabler, nil
}

type fakeRelayDisabler struct {
	disabled []int64
}

func (f *fakeRelayDisabler) DisableUser(_ context.Context, userID int64) error {
	f.disabled = append(f.disabled, userID)
	return nil
}

type runListTestFixture struct {
	client         *ent.Client
	service        *Service
	sourceID       int
	expectedIDs    []int
	latestActiveID int
	detailRunID    int
}

func newRunListTestFixture(t *testing.T) runListTestFixture {
	t.Helper()
	client := testdb.Open(t)
	ctx := context.Background()
	source := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic run history").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)

	base := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	creates := make([]*ent.DirectorySyncRunCreate, 0, 125)
	for i := 0; i < 125; i++ {
		mode := []directorysyncrun.Mode{
			directorysyncrun.ModeValidate,
			directorysyncrun.ModePreview,
			directorysyncrun.ModeApply,
		}[i%3]
		create := client.DirectorySyncRun.Create().
			SetSourceID(source.ID).
			SetMode(mode).
			SetTrigger(directorysyncrun.TriggerManual).
			SetHTTPRequestCount(i + 1).
			SetDepartmentCount(i + 2).
			SetMemberCount(i + 3).
			SetInvalidMemberCount(i % 4).
			SetWarningCount((i % 5) + 1).
			SetWarnings([]map[string]any{{"message": fmt.Sprintf("warning-marker-%03d", i)}}).
			SetSummary(map[string]any{"summary_marker": fmt.Sprintf("summary-marker-%03d", i)}).
			SetPreviewDiff(map[string]any{"diff_marker": fmt.Sprintf("diff-marker-%03d", i)}).
			SetErrorMessage(fmt.Sprintf("error-marker-%03d", i))

		switch i {
		case 0:
			create.SetStatus(directorysyncrun.StatusQueued).
				SetPhase(directorysyncrun.PhaseValidating)
		case 1:
			create.SetMode(directorysyncrun.ModePreview).
				SetStatus(directorysyncrun.StatusQueued).
				SetPhase(directorysyncrun.PhaseValidating)
		case 2:
			create.SetMode(directorysyncrun.ModeApply).
				SetStatus(directorysyncrun.StatusQueued).
				SetPhase(directorysyncrun.PhaseValidating)
		default:
			startedAt := base.Add(time.Duration((i-3)/4) * time.Minute)
			status := directorysyncrun.StatusCompleted
			phase := directorysyncrun.PhaseCompleted
			if i%11 == 0 {
				status = directorysyncrun.StatusFailed
				phase = directorysyncrun.PhaseFailed
			} else if i%7 == 0 {
				status = directorysyncrun.StatusCompletedWithWarnings
			}
			create.SetStatus(status).
				SetPhase(phase).
				SetStartedAt(startedAt).
				SetCompletedAt(startedAt.Add(30 * time.Second))
		}
		creates = append(creates, create)
	}

	runs := client.DirectorySyncRun.CreateBulk(creates...).SaveX(ctx)
	sorted := append([]*ent.DirectorySyncRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.StartedAt == nil || right.StartedAt == nil {
			if left.StartedAt == nil && right.StartedAt == nil {
				return left.ID > right.ID
			}
			return left.StartedAt == nil
		}
		if left.StartedAt.Equal(*right.StartedAt) {
			return left.ID > right.ID
		}
		return left.StartedAt.After(*right.StartedAt)
	})
	expectedIDs := make([]int, len(sorted))
	for i, run := range sorted {
		expectedIDs[i] = run.ID
	}

	return runListTestFixture{
		client:         client,
		service:        NewService(client, ServiceOptions{}),
		sourceID:       source.ID,
		expectedIDs:    expectedIDs,
		latestActiveID: runs[2].ID,
		detailRunID:    runs[64].ID,
	}
}

func requireRunSummaryIDs(t *testing.T, items []RunSummary, want []int) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d", len(items), len(want))
	}
	for i := range want {
		if items[i].ID != want[i] {
			t.Fatalf("items[%d].id = %d, want %d", i, items[i].ID, want[i])
		}
	}
}

func TestListRunsDefaultsToTwenty(t *testing.T) {
	fixture := newRunListTestFixture(t)
	for _, request := range []RunListRequest{
		{SourceID: fixture.sourceID},
		{SourceID: fixture.sourceID, Limit: -1},
	} {
		page, err := fixture.service.ListRuns(context.Background(), request)
		if err != nil {
			t.Fatalf("ListRuns(%+v): %v", request, err)
		}
		if page.PageSize != DefaultRunPageSize || page.Page != 0 || page.Total != 125 {
			t.Fatalf("page = %+v, want page_size=%d page=0 total=125", page, DefaultRunPageSize)
		}
		requireRunSummaryIDs(t, page.Items, fixture.expectedIDs[:DefaultRunPageSize])
	}
}

func TestListRunsClampsLimitToOneHundred(t *testing.T) {
	fixture := newRunListTestFixture(t)
	for _, limit := range []int{101, 1000} {
		page, err := fixture.service.ListRuns(context.Background(), RunListRequest{
			SourceID: fixture.sourceID,
			Limit:    limit,
		})
		if err != nil {
			t.Fatalf("ListRuns(limit=%d): %v", limit, err)
		}
		if page.PageSize != MaxRunPageSize || len(page.Items) != MaxRunPageSize {
			t.Fatalf("limit %d returned page_size/items = %d/%d, want %d", limit, page.PageSize, len(page.Items), MaxRunPageSize)
		}
	}
}

func TestListRunsNormalizesNegativeOffset(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{
		SourceID: fixture.sourceID,
		Limit:    20,
		Offset:   -7,
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if page.Page != 0 {
		t.Fatalf("page = %d, want 0", page.Page)
	}
	requireRunSummaryIDs(t, page.Items, fixture.expectedIDs[:20])
}

func TestListRunsOrdersStartedAtThenIDDescending(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{
		SourceID: fixture.sourceID,
		Limit:    100,
		Offset:   3,
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	requireRunSummaryIDs(t, page.Items, fixture.expectedIDs[3:103])
	for i := 1; i < len(page.Items); i++ {
		previous, current := page.Items[i-1], page.Items[i]
		if previous.StartedAt != nil && current.StartedAt != nil && previous.StartedAt.Equal(*current.StartedAt) && previous.ID <= current.ID {
			t.Fatalf("equal started_at tie ordered ids %d then %d, want descending ids", previous.ID, current.ID)
		}
	}
}

func TestListRunsOrdersQueuedNullStartedAtFirst(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	requireRunSummaryIDs(t, page.Items[:3], fixture.expectedIDs[:3])
	for i := 0; i < 3; i++ {
		if page.Items[i].StartedAt != nil {
			t.Fatalf("items[%d].started_at = %v, want nil", i, page.Items[i].StartedAt)
		}
	}
}

func TestListRunsPagesTiesWithoutDuplicates(t *testing.T) {
	fixture := newRunListTestFixture(t)
	first, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID, Limit: 20})
	if err != nil {
		t.Fatalf("ListRuns first page: %v", err)
	}
	second, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID, Limit: 20, Offset: 20})
	if err != nil {
		t.Fatalf("ListRuns second page: %v", err)
	}
	unaligned, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID, Limit: 20, Offset: 21})
	if err != nil {
		t.Fatalf("ListRuns unaligned page: %v", err)
	}
	requireRunSummaryIDs(t, first.Items, fixture.expectedIDs[:20])
	requireRunSummaryIDs(t, second.Items, fixture.expectedIDs[20:40])
	requireRunSummaryIDs(t, unaligned.Items, fixture.expectedIDs[21:41])
	if unaligned.Page != 1 {
		t.Fatalf("unaligned page = %d, want floor(21/20)=1", unaligned.Page)
	}
	seen := make(map[int]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range second.Items {
		if _, ok := seen[item.ID]; ok {
			t.Fatalf("run %d appears on adjacent pages", item.ID)
		}
	}
}

func TestListRunsSummaryOmitsDiagnosticBlobs(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID, Limit: 100})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	for _, item := range page.Items {
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("marshal summary %d: %v", item.ID, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("decode summary %d: %v", item.ID, err)
		}
		for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
			if _, ok := fields[key]; ok {
				t.Fatalf("summary %d contains diagnostic key %q: %s", item.ID, key, encoded)
			}
		}
		for _, marker := range []string{"warning-marker-", "summary-marker-", "diff-marker-", "error-marker-"} {
			if strings.Contains(string(encoded), marker) {
				t.Fatalf("summary %d contains diagnostic marker %q: %s", item.ID, marker, encoded)
			}
		}
	}
}

func TestListRunsReturnsLatestActiveOutsideRequestedPage(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{
		SourceID: fixture.sourceID,
		Limit:    20,
		Offset:   100,
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if page.LatestActiveRun == nil || page.LatestActiveRun.ID != fixture.latestActiveID {
		t.Fatalf("latest_active_run = %+v, want id %d", page.LatestActiveRun, fixture.latestActiveID)
	}
	for _, item := range page.Items {
		if item.ID == fixture.latestActiveID {
			t.Fatalf("latest active run %d unexpectedly appears on requested history page", item.ID)
		}
	}
}

func TestListRunsReturnsEmptyItemsNotNull(t *testing.T) {
	fixture := newRunListTestFixture(t)
	page, err := fixture.service.ListRuns(context.Background(), RunListRequest{SourceID: fixture.sourceID + 1000})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("items = %#v, want non-nil empty slice", page.Items)
	}
	if page.LatestActiveRun != nil || page.Total != 0 {
		t.Fatalf("empty page = %+v, want total=0 latest_active_run=nil", page)
	}
}

func TestListRunsRejectsInvalidSourceID(t *testing.T) {
	client := testdb.Open(t)
	service := NewService(client, ServiceOptions{})
	if _, err := service.ListRuns(context.Background(), RunListRequest{}); err == nil {
		t.Fatal("ListRuns succeeded with source_id=0")
	} else if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("ListRuns error = %T %v, want ValidationError", err, err)
	}
}

func TestGetRunKeepsCompleteDiagnostics(t *testing.T) {
	fixture := newRunListTestFixture(t)
	run, err := fixture.service.GetRun(context.Background(), fixture.detailRunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal run detail: %v", err)
	}
	for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("decode run detail: %v", err)
		}
		if _, ok := fields[key]; !ok {
			t.Fatalf("run detail missing diagnostic key %q: %s", key, encoded)
		}
	}
	for _, marker := range []string{"warning-marker-064", "summary-marker-064", "diff-marker-064", "error-marker-064"} {
		if !strings.Contains(string(encoded), marker) {
			t.Fatalf("run detail missing marker %q: %s", marker, encoded)
		}
	}
}

func TestServicePreviewDoesNotUpdateFactsAndApplyDoes(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	preview, err := svc.RunSource(ctx, source.ID, "preview", "manual")
	if err != nil {
		t.Fatalf("preview RunSource: %v", err)
	}
	if preview.Status != "queued" {
		t.Fatalf("preview status = %s, want queued", preview.Status)
	}
	preview, err = svc.ExecuteRun(ctx, preview.ID)
	if err != nil {
		t.Fatalf("preview ExecuteRun: %v", err)
	}
	if preview.Status != "completed" {
		t.Fatalf("executed preview status = %s, want completed", preview.Status)
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 0 {
		t.Fatalf("preview member count = %d, want 0", count)
	}

	apply, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if apply.Status != "queued" {
		t.Fatalf("apply status = %s, want queued", apply.Status)
	}
	apply, err = svc.ExecuteRun(ctx, apply.ID)
	if err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}
	if apply.Status != "completed" {
		t.Fatalf("executed apply status = %s, want completed", apply.Status)
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 1 {
		t.Fatalf("apply member count = %d, want 1", count)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if reloaded.LastSuccessfulRunID == nil || *reloaded.LastSuccessfulRunID != apply.ID {
		t.Fatalf("last_successful_run_id = %v, want %d", reloaded.LastSuccessfulRunID, apply.ID)
	}
}

func TestServiceRunSourceRejectsOverlappingApply(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	first, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("first RunSource: %v", err)
	}
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err == nil {
		t.Fatal("second apply RunSource succeeded, want conflict")
	} else if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("second apply error = %T %v, want ConflictError", err, err)
	}

	if _, err := svc.ExecuteRun(ctx, first.ID); err != nil {
		t.Fatalf("first ExecuteRun: %v", err)
	}
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err != nil {
		t.Fatalf("apply after first completion: %v", err)
	}
}

func TestServiceRunSourceRejectsConcurrentApplyCreation(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	firstCreateEntered := make(chan struct{})
	releaseFirstCreate := make(chan struct{})
	var hookMu sync.Mutex
	blockedFirstCreate := false
	client.DirectorySyncRun.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			hookMu.Lock()
			shouldBlock := m.Op().Is(ent.OpCreate) && !blockedFirstCreate
			if shouldBlock {
				blockedFirstCreate = true
			}
			hookMu.Unlock()
			if shouldBlock {
				close(firstCreateEntered)
				<-releaseFirstCreate
			}
			return next.Mutate(ctx, m)
		})
	})
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	firstErr := make(chan error, 1)
	go func() {
		_, err := svc.RunSource(ctx, source.ID, "apply", "manual")
		firstErr <- err
	}()
	<-firstCreateEntered
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err == nil {
		close(releaseFirstCreate)
		t.Fatal("concurrent apply RunSource succeeded, want conflict")
	} else if _, ok := err.(*ConflictError); !ok {
		close(releaseFirstCreate)
		t.Fatalf("concurrent apply error = %T %v, want ConflictError", err, err)
	}
	close(releaseFirstCreate)
	if err := <-firstErr; err != nil {
		t.Fatalf("first RunSource: %v", err)
	}
}

func TestServiceApplyRollsBackFactsWhenSourcePointerUpdateFails(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	client.DirectorySource.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			mutation, ok := m.(*ent.DirectorySourceMutation)
			if ok && m.Op().Is(ent.OpUpdateOne) {
				if _, exists := mutation.LastSuccessfulRunID(); exists {
					return nil, fmt.Errorf("injected source pointer failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err == nil {
		t.Fatal("ExecuteRun succeeded, want injected source pointer failure")
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 0 {
		t.Fatalf("member count after failed apply = %d, want 0", count)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if reloaded.LastSuccessfulRunID != nil {
		t.Fatalf("last_successful_run_id = %v, want nil", reloaded.LastSuccessfulRunID)
	}
}

func TestCurrentSourceIDUsesLatestSuccessfulApplyRun(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldCompletedAt := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	newCompletedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "alice@example.com", oldCompletedAt)
	newSource := createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "alice@example.com", newCompletedAt)
	if _, err := client.DirectorySource.UpdateOneID(oldSource.ID).SetDescription("Edited after latest sync").Save(ctx); err != nil {
		t.Fatalf("update old source: %v", err)
	}

	sourceID, ok, err := CurrentSourceID(ctx, client)
	if err != nil {
		t.Fatalf("CurrentSourceID: %v", err)
	}
	if !ok || sourceID != newSource.ID {
		t.Fatalf("current source = %d/%v, want new source %d", sourceID, ok, newSource.ID)
	}
}

func TestServiceListDepartmentsReturnsDisplayPathAndFiltersByIt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(2).
		SetMemberCount(0).
		SetCompletedAt(time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("1684075").
		SetName("Department Alpha").
		SetPath("1.488797.1684075").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("1684207").
		SetParentExternalID("1684075").
		SetName("Team One").
		SetPath("1.488797.1684075.1684077.1684207").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	svc := NewService(client, ServiceOptions{})

	items, err := svc.ListDepartments(ctx, source.ID, "Team One")
	if err != nil {
		t.Fatalf("ListDepartments: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %#v", len(items), items)
	}
	if items[0].Path != "1.488797.1684075.1684077.1684207" {
		t.Fatalf("path = %q, want raw numeric source path", items[0].Path)
	}
	if items[0].DisplayPath != "Department Alpha / Team One" {
		t.Fatalf("display_path = %q, want name-based hierarchy", items[0].DisplayPath)
	}

	items, err = svc.ListDepartments(ctx, source.ID, "Department Alpha / Team One")
	if err != nil {
		t.Fatalf("ListDepartments by display path: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "1684207" {
		t.Fatalf("display path search items = %#v, want child department", items)
	}
}

func TestServiceListOffboardingCandidatesUsesCurrentSourceWhenSourceIDMissing(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	older := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "bob@example.org", older)
	createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "alice@example.com", latest)
	if _, err := client.DirectorySource.UpdateOneID(oldSource.ID).SetDescription("Edited after latest sync").Save(ctx); err != nil {
		t.Fatalf("update old source: %v", err)
	}
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)
	svc := NewService(client, ServiceOptions{})

	candidates, err := svc.ListOffboardingCandidates(ctx, 0, "")
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob missing from current source", candidates)
	}
}

func TestServiceDisableCandidateRejectsStaleSourceIDWhenUserExistsInCurrentSource(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	older := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "alice@example.com", older)
	createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "bob@example.org", latest)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{
		RelayDisablers: fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:   revoker,
	})

	_, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          oldSource.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded with stale source id, want conflict")
	}
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("error = %T %v, want ConflictError", err, err)
	}
	if len(disabler.disabled) != 0 || len(revoker.calls) != 0 {
		t.Fatalf("side effects disabled=%v revocations=%v, want none", disabler.disabled, revoker.calls)
	}
}

func TestServiceCreateAndUpdateSourceRejectLiteralSecretsBeforePersisting(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	svc := NewService(client, ServiceOptions{
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	secretDSL := strings.Replace(validDirectoryDSL, "      url: https://directory.example.com/api/departments\n", "      url: https://directory.example.com/api/departments\n      headers:\n        Authorization: Bearer test-token\n", 1)

	if _, err := svc.CreateSource(ctx, SourceInput{
		Name:    "Unsafe Directory",
		Enabled: true,
		DSL:     secretDSL,
	}); err == nil {
		t.Fatal("CreateSource succeeded with literal secret, want validation error")
	} else if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("CreateSource error = %T %v, want ValidationError", err, err)
	}
	if count := client.DirectorySource.Query().CountX(ctx); count != 0 {
		t.Fatalf("source count = %d, want 0", count)
	}

	source, err := svc.CreateSource(ctx, SourceInput{
		Name:    "Safe Directory",
		Enabled: true,
		DSL:     validDirectoryDSL,
	})
	if err != nil {
		t.Fatalf("CreateSource safe: %v", err)
	}
	if _, err := svc.UpdateSource(ctx, source.ID, SourceInput{
		Name:    "Unsafe Directory",
		Enabled: true,
		DSL:     secretDSL,
	}); err == nil {
		t.Fatal("UpdateSource succeeded with literal secret, want validation error")
	} else if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("UpdateSource error = %T %v, want ValidationError", err, err)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if strings.Contains(reloaded.Dsl, "Authorization") {
		t.Fatalf("persisted unsafe DSL: %s", reloaded.Dsl)
	}
}

func TestCurrentSourceIDIgnoresSuccessfulRunWithoutCompletedAt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	incompleteSource := client.DirectorySource.Create().
		SetName("Incomplete Directory").
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	incompleteRun := client.DirectorySyncRun.Create().
		SetSourceID(incompleteSource.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(incompleteSource.ID).
		SetLastRunID(incompleteRun.ID).
		SetLastSuccessfulRunID(incompleteRun.ID).
		SaveX(ctx)
	currentSource := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", latest)

	sourceID, ok, err := CurrentSourceID(ctx, client)
	if err != nil {
		t.Fatalf("CurrentSourceID: %v", err)
	}
	if !ok || sourceID != currentSource.ID {
		t.Fatalf("current source = %d/%v, want %d", sourceID, ok, currentSource.ID)
	}
}

func TestServiceOffboardingCandidateAndDisableRevokesTokens(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}

	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)

	candidates, err := svc.ListOffboardingCandidates(ctx, source.ID, "")
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob only", candidates)
	}

	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc = NewService(client, ServiceOptions{
		Executor:       NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:    staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		RelayDisablers: fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:   revoker,
		Now:            func() time.Time { return time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC) },
	})
	action, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            "missing_from_latest_full_company_directory",
		PerformedByUserID: bob.ID,
	})
	if err != nil {
		t.Fatalf("DisableRelayUserForCandidate: %v", err)
	}
	if action.Status != "succeeded" {
		t.Fatalf("action status = %s, want succeeded", action.Status)
	}
	if len(disabler.disabled) != 1 || disabler.disabled[0] != 99 {
		t.Fatalf("disabled = %v, want [99]", disabler.disabled)
	}
	if len(revoker.calls) != 1 || revoker.calls[0].UserID != bob.ID {
		t.Fatalf("revocations = %+v, want bob", revoker.calls)
	}
}

func TestServiceProviderWithoutDisableCapabilityReturnsValidationError(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)

	_, err = svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            user.ID,
		ConfirmEmail:      "alice@example.com",
		Reason:            "missing_from_latest_full_company_directory",
		PerformedByUserID: user.ID,
	})
	if err == nil {
		t.Fatal("expected validation error when relay disabler is not configured")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
}

func newDirectoryServiceTestServer(t *testing.T, memberEmails []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Directory-API-Key") != "test-directory-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/departments":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"departments": []map[string]any{
				{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
			}}})
		case "/users":
			users := make([]map[string]any, 0, len(memberEmails))
			for _, email := range memberEmails {
				users = append(users, map[string]any{"id": email, "email": email, "name": email, "status": "active"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"users": users}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func createDirectoryTestSource(t *testing.T, ctx context.Context, client *ent.Client, baseURL string) *ent.DirectorySource {
	t.Helper()
	raw := stringsReplaceAll(validDirectoryDSL, map[string]string{
		"https://directory.example.com/api/departments": baseURL + "/departments",
		"https://directory.example.com/api/users":       baseURL + "/users",
	})
	return client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic directory source").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl(raw).
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
}

func createDirectorySnapshot(t *testing.T, ctx context.Context, client *ent.Client, name, departmentID, memberEmail string, completedAt time.Time) *ent.DirectorySource {
	t.Helper()
	source := client.DirectorySource.Create().
		SetName(name).
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(1).
		SetMemberCount(1).
		SetCompletedAt(completedAt).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName("Department " + strings.TrimPrefix(departmentID, "dept-")).
		SetPath("Department " + strings.TrimPrefix(departmentID, "dept-")).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID(memberEmail).
		SetEmailNormalized(memberEmail).
		SetDisplayName(memberEmail).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source
}

func stringsReplaceAll(input string, replacements map[string]string) string {
	out := input
	for old, replacement := range replacements {
		out = strings.ReplaceAll(out, old, replacement)
	}
	return out
}
