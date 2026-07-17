package directorysync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/workitems"
	"go.uber.org/zap"
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

func (f *fakeTokenRevoker) RevokeUserTokensTx(_ context.Context, _ *ent.Tx, userID int, revokedAt time.Time) error {
	f.calls = append(f.calls, revocationCall{UserID: userID, RevokedAt: revokedAt})
	return nil
}

type relayDisablerFunc func(context.Context, int64) error

func (f relayDisablerFunc) DisableUser(ctx context.Context, userID int64) error {
	return f(ctx, userID)
}

type observingTxTokenRevoker struct {
	delegate       *auth.Service
	sawUncancelled bool
	sawFiveSecond  bool
}

type deadlineTxTokenRevoker struct{}

func (deadlineTxTokenRevoker) RevokeUserTokensTx(ctx context.Context, _ *ent.Tx, _ int, _ time.Time) error {
	<-ctx.Done()
	return ctx.Err()
}

func (r *observingTxTokenRevoker) RevokeUserTokensTx(ctx context.Context, tx *ent.Tx, userID int, revokedAt time.Time) error {
	r.sawUncancelled = ctx.Err() == nil
	deadline, ok := ctx.Deadline()
	remaining := time.Until(deadline)
	r.sawFiveSecond = ok && remaining > 0 && remaining <= 5*time.Second
	return r.delegate.RevokeUserTokensTx(ctx, tx, userID, revokedAt)
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
	client            *ent.Client
	service           *Service
	sourceID          int
	expectedIDs       []int
	latestActiveCases []latestActiveTestCase
	seededSummary     RunSummary
	detailRunID       int
}

type latestActiveTestCase struct {
	name     string
	sourceID int
	want     RunSummary
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
	runningStartedAt := base.Add(48 * time.Hour)
	terminalPreviewStartedAt := base.Add(50 * time.Hour)
	terminalApplyStartedAt := base.Add(49 * time.Hour)
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
			create.SetMode(directorysyncrun.ModeApply).
				SetStatus(directorysyncrun.StatusRunning).
				SetPhase(directorysyncrun.PhaseExecuting).
				SetStartedAt(runningStartedAt)
		case 1:
			create.SetMode(directorysyncrun.ModePreview).
				SetStatus(directorysyncrun.StatusCompleted).
				SetPhase(directorysyncrun.PhaseCompleted).
				SetStartedAt(terminalPreviewStartedAt).
				SetCompletedAt(terminalPreviewStartedAt.Add(30 * time.Second))
		case 2:
			create.SetMode(directorysyncrun.ModeApply).
				SetStatus(directorysyncrun.StatusFailed).
				SetPhase(directorysyncrun.PhaseFailed).
				SetStartedAt(terminalApplyStartedAt).
				SetCompletedAt(terminalApplyStartedAt.Add(30 * time.Second))
		case 3, 4, 5:
			create.SetMode(directorysyncrun.ModeValidate).
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
	queuedSource := client.DirectorySource.Create().
		SetName("Queued Active Example Directory").
		SetDescription("Synthetic queued active eligibility history").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	queuedRunningStartedAt := base.Add(46 * time.Hour)
	queuedTerminalStartedAt := base.Add(47 * time.Hour)
	queuedSourceRuns := client.DirectorySyncRun.CreateBulk(
		client.DirectorySyncRun.Create().
			SetSourceID(queuedSource.ID).
			SetMode(directorysyncrun.ModePreview).
			SetTrigger(directorysyncrun.TriggerSchedule).
			SetStatus(directorysyncrun.StatusRunning).
			SetPhase(directorysyncrun.PhaseExecuting).
			SetStartedAt(queuedRunningStartedAt),
		client.DirectorySyncRun.Create().
			SetSourceID(queuedSource.ID).
			SetMode(directorysyncrun.ModeApply).
			SetTrigger(directorysyncrun.TriggerSchedule).
			SetStatus(directorysyncrun.StatusQueued).
			SetPhase(directorysyncrun.PhaseValidating).
			SetHTTPRequestCount(211).
			SetDepartmentCount(212).
			SetMemberCount(213).
			SetInvalidMemberCount(214).
			SetWarningCount(215),
		client.DirectorySyncRun.Create().
			SetSourceID(queuedSource.ID).
			SetMode(directorysyncrun.ModePreview).
			SetTrigger(directorysyncrun.TriggerSchedule).
			SetStatus(directorysyncrun.StatusCompleted).
			SetPhase(directorysyncrun.PhaseCompleted).
			SetStartedAt(queuedTerminalStartedAt).
			SetCompletedAt(queuedTerminalStartedAt.Add(30*time.Second)),
		client.DirectorySyncRun.Create().
			SetSourceID(queuedSource.ID).
			SetMode(directorysyncrun.ModeValidate).
			SetTrigger(directorysyncrun.TriggerSchedule).
			SetStatus(directorysyncrun.StatusQueued).
			SetPhase(directorysyncrun.PhaseValidating),
	).SaveX(ctx)
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

	seededStartedAt := base.Add(15 * time.Minute)
	seededCompletedAt := seededStartedAt.Add(30 * time.Second)
	return runListTestFixture{
		client:      client,
		service:     NewService(client, ServiceOptions{}),
		sourceID:    source.ID,
		expectedIDs: expectedIDs,
		latestActiveCases: []latestActiveTestCase{
			{
				name:     "running apply",
				sourceID: source.ID,
				want: RunSummary{
					ID:                 runs[0].ID,
					SourceID:           source.ID,
					Mode:               directorysyncrun.ModeApply,
					Trigger:            directorysyncrun.TriggerManual,
					Status:             directorysyncrun.StatusRunning,
					Phase:              directorysyncrun.PhaseExecuting,
					StartedAt:          &runningStartedAt,
					HTTPRequestCount:   1,
					DepartmentCount:    2,
					MemberCount:        3,
					InvalidMemberCount: 0,
					WarningCount:       1,
				},
			},
			{
				name:     "queued apply",
				sourceID: queuedSource.ID,
				want: RunSummary{
					ID:                 queuedSourceRuns[1].ID,
					SourceID:           queuedSource.ID,
					Mode:               directorysyncrun.ModeApply,
					Trigger:            directorysyncrun.TriggerSchedule,
					Status:             directorysyncrun.StatusQueued,
					Phase:              directorysyncrun.PhaseValidating,
					HTTPRequestCount:   211,
					DepartmentCount:    212,
					MemberCount:        213,
					InvalidMemberCount: 214,
					WarningCount:       215,
				},
			},
		},
		seededSummary: RunSummary{
			ID:                 runs[65].ID,
			SourceID:           source.ID,
			Mode:               directorysyncrun.ModeApply,
			Trigger:            directorysyncrun.TriggerManual,
			Status:             directorysyncrun.StatusCompleted,
			Phase:              directorysyncrun.PhaseCompleted,
			StartedAt:          &seededStartedAt,
			CompletedAt:        &seededCompletedAt,
			HTTPRequestCount:   66,
			DepartmentCount:    67,
			MemberCount:        68,
			InvalidMemberCount: 1,
			WarningCount:       1,
		},
		detailRunID: runs[64].ID,
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

func requireRunSummary(t *testing.T, got, want RunSummary) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("id = %d, want %d", got.ID, want.ID)
	}
	if got.SourceID != want.SourceID {
		t.Errorf("source_id = %d, want %d", got.SourceID, want.SourceID)
	}
	if got.Mode != want.Mode {
		t.Errorf("mode = %q, want %q", got.Mode, want.Mode)
	}
	if got.Trigger != want.Trigger {
		t.Errorf("trigger = %q, want %q", got.Trigger, want.Trigger)
	}
	if got.Status != want.Status {
		t.Errorf("status = %q, want %q", got.Status, want.Status)
	}
	if got.Phase != want.Phase {
		t.Errorf("phase = %q, want %q", got.Phase, want.Phase)
	}
	if !optionalTimeEqual(got.StartedAt, want.StartedAt) {
		t.Errorf("started_at = %v, want %v", got.StartedAt, want.StartedAt)
	}
	if !optionalTimeEqual(got.CompletedAt, want.CompletedAt) {
		t.Errorf("completed_at = %v, want %v", got.CompletedAt, want.CompletedAt)
	}
	if got.HTTPRequestCount != want.HTTPRequestCount {
		t.Errorf("http_request_count = %d, want %d", got.HTTPRequestCount, want.HTTPRequestCount)
	}
	if got.DepartmentCount != want.DepartmentCount {
		t.Errorf("department_count = %d, want %d", got.DepartmentCount, want.DepartmentCount)
	}
	if got.MemberCount != want.MemberCount {
		t.Errorf("member_count = %d, want %d", got.MemberCount, want.MemberCount)
	}
	if got.InvalidMemberCount != want.InvalidMemberCount {
		t.Errorf("invalid_member_count = %d, want %d", got.InvalidMemberCount, want.InvalidMemberCount)
	}
	if got.WarningCount != want.WarningCount {
		t.Errorf("warning_count = %d, want %d", got.WarningCount, want.WarningCount)
	}
}

func optionalTimeEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

var runSummaryJSONKeyAllowlist = []string{
	"id",
	"source_id",
	"mode",
	"trigger",
	"status",
	"phase",
	"started_at",
	"completed_at",
	"http_request_count",
	"department_count",
	"member_count",
	"invalid_member_count",
	"warning_count",
}

func requireRunSummaryJSONKeys(t *testing.T, summary RunSummary) []byte {
	t.Helper()
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary %d: %v", summary.ID, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode summary %d: %v", summary.ID, err)
	}
	if len(fields) != len(runSummaryJSONKeyAllowlist) {
		t.Errorf("summary %d key count = %d, want %d: %s", summary.ID, len(fields), len(runSummaryJSONKeyAllowlist), encoded)
	}
	allowed := make(map[string]struct{}, len(runSummaryJSONKeyAllowlist))
	for _, key := range runSummaryJSONKeyAllowlist {
		allowed[key] = struct{}{}
		if _, ok := fields[key]; !ok {
			t.Errorf("summary %d missing key %q: %s", summary.ID, key, encoded)
		}
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			t.Errorf("summary %d contains unagreed key %q: %s", summary.ID, key, encoded)
		}
	}
	return encoded
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
		encoded := requireRunSummaryJSONKeys(t, item)
		for _, marker := range []string{"warning-marker-", "summary-marker-", "diff-marker-", "error-marker-"} {
			if strings.Contains(string(encoded), marker) {
				t.Fatalf("summary %d contains diagnostic marker %q: %s", item.ID, marker, encoded)
			}
		}
	}
	for _, item := range page.Items {
		if item.ID == fixture.seededSummary.ID {
			requireRunSummary(t, item, fixture.seededSummary)
			return
		}
	}
	t.Fatalf("seeded summary %d not found in first 100 items", fixture.seededSummary.ID)
}

func TestListRunsReturnsLatestActiveOutsideRequestedPage(t *testing.T) {
	fixture := newRunListTestFixture(t)
	for _, tt := range fixture.latestActiveCases {
		t.Run(tt.name, func(t *testing.T) {
			page, err := fixture.service.ListRuns(context.Background(), RunListRequest{
				SourceID: tt.sourceID,
				Limit:    20,
				Offset:   100,
			})
			if err != nil {
				t.Fatalf("ListRuns: %v", err)
			}
			if page.LatestActiveRun == nil {
				t.Fatalf("latest_active_run = nil, want id=%d mode=%q status=%q", tt.want.ID, tt.want.Mode, tt.want.Status)
			}
			requireRunSummary(t, *page.LatestActiveRun, tt.want)
			requireRunSummaryJSONKeys(t, *page.LatestActiveRun)
			for _, item := range page.Items {
				if item.ID == tt.want.ID {
					t.Fatalf("latest active run %d unexpectedly appears on requested history page", item.ID)
				}
			}
		})
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
	revisions := newDirectoryRevisionStore(t, ctx, client)
	svc := NewService(client, ServiceOptions{
		Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		WorkItemCountsInvalidator: revisions,
	})
	beforePreview := currentDirectoryRevision(t, ctx, revisions)

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
	if afterPreview := currentDirectoryRevision(t, ctx, revisions); afterPreview != beforePreview {
		t.Fatalf("revision after preview = %q, want unchanged %q", afterPreview, beforePreview)
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
	if afterApply := currentDirectoryRevision(t, ctx, revisions); afterApply == beforePreview {
		t.Fatalf("revision after successful apply = %q, want change from %q", afterApply, beforePreview)
	}
}

func TestServiceRunSourceRejectsOverlappingApply(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	revisions := newDirectoryRevisionStore(t, ctx, client)
	svc := NewService(client, ServiceOptions{
		Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		WorkItemCountsInvalidator: revisions,
	})
	beforeConflict := currentDirectoryRevision(t, ctx, revisions)

	first, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("first RunSource: %v", err)
	}
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err == nil {
		t.Fatal("second apply RunSource succeeded, want conflict")
	} else if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("second apply error = %T %v, want ConflictError", err, err)
	}
	if afterConflict := currentDirectoryRevision(t, ctx, revisions); afterConflict != beforeConflict {
		t.Fatalf("revision after apply conflict = %q, want unchanged %q", afterConflict, beforeConflict)
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

func TestServiceSourceUpdateAndDeleteInvalidateInSameTransaction(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
		source := createDirectoryTestSource(t, ctx, client, server.URL)
		revisions := newDirectoryRevisionStore(t, ctx, client)
		svc := NewService(client, ServiceOptions{
			Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
			Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
			WorkItemCountsInvalidator: revisions,
		})
		before := currentDirectoryRevision(t, ctx, revisions)

		updated, err := svc.UpdateSource(ctx, source.ID, directorySourceInput(source, "Updated Directory"))
		if err != nil {
			t.Fatalf("UpdateSource: %v", err)
		}
		if updated.Name != "Updated Directory" {
			t.Fatalf("updated name = %q, want Updated Directory", updated.Name)
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after == before {
			t.Fatalf("revision after source update = %q, want change", after)
		}
	})

	t.Run("delete", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
		source := createDirectoryTestSource(t, ctx, client, server.URL)
		revisions := newDirectoryRevisionStore(t, ctx, client)
		svc := NewService(client, ServiceOptions{WorkItemCountsInvalidator: revisions})
		before := currentDirectoryRevision(t, ctx, revisions)

		if err := svc.DeleteSource(ctx, source.ID); err != nil {
			t.Fatalf("DeleteSource: %v", err)
		}
		reloaded := client.DirectorySource.GetX(ctx, source.ID)
		if !reloaded.Deleted || reloaded.Enabled || reloaded.ScheduleEnabled {
			t.Fatalf("deleted source = %+v, want soft-deleted and disabled", reloaded)
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after == before {
			t.Fatalf("revision after source delete = %q, want change", after)
		}
	})
}

func TestServiceSourceMutationInvalidationFailureRollsBackState(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
		source := createDirectoryTestSource(t, ctx, client, server.URL)
		revisions := newDirectoryRevisionStore(t, ctx, client)
		failNextRevision := installDirectoryRevisionFailureHook(client)
		svc := NewService(client, ServiceOptions{
			Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
			Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
			WorkItemCountsInvalidator: revisions,
		})
		before := currentDirectoryRevision(t, ctx, revisions)
		failNextRevision()

		if _, err := svc.UpdateSource(ctx, source.ID, directorySourceInput(source, "Must Roll Back")); err == nil {
			t.Fatal("UpdateSource succeeded despite revision failure")
		}
		reloaded := client.DirectorySource.GetX(ctx, source.ID)
		if reloaded.Name != source.Name {
			t.Fatalf("source name after rollback = %q, want %q", reloaded.Name, source.Name)
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after != before {
			t.Fatalf("revision after failed source update = %q, want %q", after, before)
		}
	})

	t.Run("delete", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
		source := createDirectoryTestSource(t, ctx, client, server.URL)
		revisions := newDirectoryRevisionStore(t, ctx, client)
		failNextRevision := installDirectoryRevisionFailureHook(client)
		svc := NewService(client, ServiceOptions{WorkItemCountsInvalidator: revisions})
		before := currentDirectoryRevision(t, ctx, revisions)
		failNextRevision()

		if err := svc.DeleteSource(ctx, source.ID); err == nil {
			t.Fatal("DeleteSource succeeded despite revision failure")
		}
		reloaded := client.DirectorySource.GetX(ctx, source.ID)
		if reloaded.Deleted || !reloaded.Enabled {
			t.Fatalf("source after delete rollback deleted=%v enabled=%v, want false/true", reloaded.Deleted, reloaded.Enabled)
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after != before {
			t.Fatalf("revision after failed source delete = %q, want %q", after, before)
		}
	})
}

func TestServiceApplyInvalidationFailureRollsBackFactsAndPointers(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	revisions := newDirectoryRevisionStore(t, ctx, client)
	failNextRevision := installDirectoryRevisionFailureHook(client)
	svc := NewService(client, ServiceOptions{
		Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		WorkItemCountsInvalidator: revisions,
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("RunSource: %v", err)
	}
	before := currentDirectoryRevision(t, ctx, revisions)
	failNextRevision()

	if _, err := svc.ExecuteRun(ctx, run.ID); err == nil {
		t.Fatal("ExecuteRun succeeded despite revision failure")
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 0 {
		t.Fatalf("member count after rollback = %d, want 0", count)
	}
	reloadedSource := client.DirectorySource.GetX(ctx, source.ID)
	if reloadedSource.LastSuccessfulRunID != nil || reloadedSource.LastRunID != nil {
		t.Fatalf("source pointers after rollback = %v/%v, want nil/nil", reloadedSource.LastSuccessfulRunID, reloadedSource.LastRunID)
	}
	if reloadedRun := client.DirectorySyncRun.GetX(ctx, run.ID); reloadedRun.Status != "failed" {
		t.Fatalf("run status after failed apply = %s, want failed", reloadedRun.Status)
	}
	if after := currentDirectoryRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after failed apply = %q, want %q", after, before)
	}
}

func TestServiceFailedApplyDoesNotInvalidate(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	revisions := newDirectoryRevisionStore(t, ctx, client)
	svc := NewService(client, ServiceOptions{
		Executor:                  NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		WorkItemCountsInvalidator: revisions,
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("RunSource: %v", err)
	}
	before := currentDirectoryRevision(t, ctx, revisions)
	server.Close()

	if _, err := svc.ExecuteRun(ctx, run.ID); err == nil {
		t.Fatal("ExecuteRun succeeded after directory endpoint closed")
	}
	if after := currentDirectoryRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after failed apply = %q, want %q", after, before)
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

func TestCurrentSnapshotReturnsLatestSuccessfulApplyRunVersion(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldCompletedAt := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	newCompletedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "alice@example.com", oldCompletedAt)
	newSource := createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "bob@example.org", newCompletedAt)

	snapshot, ok, err := CurrentSnapshot(ctx, client)
	if err != nil {
		t.Fatalf("CurrentSnapshot: %v", err)
	}
	if !ok {
		t.Fatal("CurrentSnapshot ok = false, want true")
	}
	if snapshot.SourceID != newSource.ID {
		t.Fatalf("snapshot source ID = %d, want %d", snapshot.SourceID, newSource.ID)
	}
	persistedSource, err := client.DirectorySource.Get(ctx, newSource.ID)
	if err != nil {
		t.Fatalf("reload new source: %v", err)
	}
	if persistedSource.LastSuccessfulRunID == nil || snapshot.RunID != *persistedSource.LastSuccessfulRunID {
		t.Fatalf("snapshot run ID = %d, want %v", snapshot.RunID, persistedSource.LastSuccessfulRunID)
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

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob missing from current source", page.Items)
	}
}

func TestServiceListOffboardingCandidatesExtremePageReturnsEmpty(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "present@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})
	svc := NewService(client, ServiceOptions{})

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{
		SourceID: source.ID,
		Page:     math.MaxInt,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates extreme page: %v", err)
	}
	if page.Page != math.MaxInt || page.PageSize != 20 || page.Total != 1 || len(page.Items) != 0 {
		t.Fatalf("extreme page = %+v, want page=%d page_size=20 total=1 and no items", page, math.MaxInt)
	}
}

func TestServiceOffboardingCandidatePageAndCountShareBoundedContract(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	completedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	source := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "present@example.com", completedAt)
	source = client.DirectorySource.GetX(ctx, source.ID)
	runID := *source.LastSuccessfulRunID

	initialUsers := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{
		{username: "action-failed", email: "failed@example.com"},
		{username: "action-partial", email: "partial@example.com"},
		{username: "disabled", email: "disabled@example.com"},
		{username: "present", email: "present@example.com"},
		{username: "regular-alpha", email: "regular-alpha@example.com"},
		{username: "regular-beta", email: "regular-beta@example.com"},
	})
	failedAction := createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[0], directoryoffboardingaction.StatusFailed)
	partialAction := createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[1], directoryoffboardingaction.StatusPartialFailed)
	createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[2], directoryoffboardingaction.StatusSucceeded)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres for duplicate username fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP INDEX users_username_key`); err != nil {
		t.Fatalf("drop isolated username index for tie fixture: %v", err)
	}
	tiedUsers := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{
		{username: "same-name", email: "same-name-one@example.com"},
		{username: "same-name", email: "same-name-two@example.com"},
	})
	if tiedUsers[0].ID >= tiedUsers[1].ID {
		t.Fatalf("tie fixture ids = %d, %d, want ascending creation ids", tiedUsers[0].ID, tiedUsers[1].ID)
	}

	bulkFixtures := make([]offboardingUserFixture, 500)
	for index := range bulkFixtures {
		bulkFixtures[index] = offboardingUserFixture{
			username: fmt.Sprintf("bulk-%03d", index),
			email:    fmt.Sprintf("bulk-%03d@example.com", index),
		}
	}
	createRelayBoundUsers(t, ctx, client, bulkFixtures)

	recorder := &entQueryRecorder{}
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(recorder.Log))
	if err != nil {
		t.Fatalf("open logged ent client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	svc := NewService(loggedClient, ServiceOptions{})

	recorder.Reset()
	count, err := svc.CountOffboardingCandidates(ctx, source.ID)
	if err != nil {
		t.Fatalf("CountOffboardingCandidates: %v", err)
	}
	if count != 506 {
		t.Fatalf("candidate count = %d, want 506", count)
	}
	if queryCount := recorder.Count(); queryCount != 2 {
		t.Fatalf("count query count = %d, want snapshot + COUNT only; queries:\n%s", queryCount, recorder.Joined())
	}

	recorder.Reset()
	defaultPage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates default page: %v", err)
	}
	if defaultPage.Page != 1 || defaultPage.PageSize != 20 || len(defaultPage.Items) != 20 || defaultPage.Total != count {
		t.Fatalf("default page = %+v, want page=1 page_size=20 len=20 total=%d", defaultPage, count)
	}
	if queryCount := recorder.Count(); queryCount != 4 {
		t.Fatalf("page query count = %d, want snapshot + count + page + action batch; queries:\n%s", queryCount, recorder.Joined())
	}
	assertOffboardingActionMetadata(t, defaultPage.Items, failedAction)
	assertOffboardingActionMetadata(t, defaultPage.Items, partialAction)
	assertCandidateAbsent(t, defaultPage.Items, initialUsers[2].ID, "succeeded disable action")
	assertCandidateAbsent(t, defaultPage.Items, initialUsers[3].ID, "current directory membership")

	maxPage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates max page: %v", err)
	}
	if maxPage.PageSize != 100 || len(maxPage.Items) != 100 {
		t.Fatalf("max page size = %d len=%d, want 100", maxPage.PageSize, len(maxPage.Items))
	}

	tiePage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Query: "same-name", PageSize: 20})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates tie page: %v", err)
	}
	if len(tiePage.Items) != 2 || tiePage.Items[0].UserID != tiedUsers[0].ID || tiePage.Items[1].UserID != tiedUsers[1].ID {
		t.Fatalf("tie page ids = %v, want [%d %d]", candidateUserIDs(tiePage.Items), tiedUsers[0].ID, tiedUsers[1].ID)
	}

	seen := make(map[int]struct{}, count)
	for pageNumber := 1; len(seen) < count; pageNumber++ {
		page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Page: pageNumber, PageSize: 73})
		if err != nil {
			t.Fatalf("ListOffboardingCandidates page %d: %v", pageNumber, err)
		}
		if page.Total != count {
			t.Fatalf("page %d total = %d, want %d", pageNumber, page.Total, count)
		}
		if len(page.Items) == 0 {
			t.Fatalf("page %d empty before collecting total=%d, collected=%d", pageNumber, count, len(seen))
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.UserID]; duplicate {
				t.Fatalf("candidate user %d repeated across stable pages", item.UserID)
			}
			seen[item.UserID] = struct{}{}
		}
	}
	if len(seen) != count {
		t.Fatalf("page union size = %d, want count %d", len(seen), count)
	}
}

func TestServiceOffboardingPageQueryCountStaysConstantAsFixtureGrows(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "present@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))

	initial := make([]offboardingUserFixture, 25)
	for index := range initial {
		initial[index] = offboardingUserFixture{username: fmt.Sprintf("initial-%03d", index), email: fmt.Sprintf("initial-%03d@example.com", index)}
	}
	createRelayBoundUsers(t, ctx, client, initial)

	recorder := &entQueryRecorder{}
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(recorder.Log))
	if err != nil {
		t.Fatalf("open logged ent client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	svc := NewService(loggedClient, ServiceOptions{})

	recorder.Reset()
	if _, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{PageSize: 20}); err != nil {
		t.Fatalf("ListOffboardingCandidates small fixture: %v", err)
	}
	smallFixtureQueries := recorder.Count()
	if !recorder.ContainsBoundedCurrentSnapshotQuery() {
		t.Fatalf("current snapshot query was not database-bounded with ORDER BY/LIMIT; queries:\n%s", recorder.Joined())
	}

	growth := make([]offboardingUserFixture, 500)
	for index := range growth {
		growth[index] = offboardingUserFixture{username: fmt.Sprintf("growth-%03d", index), email: fmt.Sprintf("growth-%03d@example.com", index)}
	}
	createRelayBoundUsers(t, ctx, client, growth)

	recorder.Reset()
	if _, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{PageSize: 20}); err != nil {
		t.Fatalf("ListOffboardingCandidates grown fixture: %v", err)
	}
	if grownFixtureQueries := recorder.Count(); grownFixtureQueries != smallFixtureQueries || grownFixtureQueries != 4 {
		t.Fatalf("query count small=%d grown=%d, want constant 4; queries:\n%s", smallFixtureQueries, grownFixtureQueries, recorder.Joined())
	}
}

func TestServiceDisableCandidateRejectsMismatchedConfirmationWithoutSideEffects(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{RelayDisablers: fakeRelayDisablerResolver{disabler: disabler}, TokenRevoker: revoker})

	_, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		UserID:            bob.ID,
		ConfirmEmail:      "alice@example.com",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded with mismatched confirmation, want validation error")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	assertNoOffboardingSideEffects(t, ctx, client, disabler, revoker)
}

func TestServiceDisableCandidateRechecksNewDirectoryMembershipWithoutSideEffects(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{RelayDisablers: fakeRelayDisablerResolver{disabler: disabler}, TokenRevoker: revoker})

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates before member re-add: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("initial candidates = %+v, want bob", page.Items)
	}
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-bob").
		SetEmailNormalized("bob@example.org").
		SetDisplayName("Bob").
		SetDepartmentExternalID("dept-current").
		SetLastSeenRunID(page.Items[0].DirectoryRunID).
		SaveX(ctx)

	_, err = svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded after directory member re-add, want conflict")
	}
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("error = %T %v, want ConflictError", err, err)
	}
	assertNoOffboardingSideEffects(t, ctx, client, disabler, revoker)
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
	revisions := newDirectoryRevisionStore(t, ctx, client)
	svc := NewService(client, ServiceOptions{
		Credentials:               staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		WorkItemCountsInvalidator: revisions,
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
	beforeInvalidUpdate := currentDirectoryRevision(t, ctx, revisions)
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
	if afterInvalidUpdate := currentDirectoryRevision(t, ctx, revisions); afterInvalidUpdate != beforeInvalidUpdate {
		t.Fatalf("revision after invalid source update = %q, want %q", afterInvalidUpdate, beforeInvalidUpdate)
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

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob only", page.Items)
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

func TestServiceOffboardingFinalizationSurvivesCancelledRequestContext(t *testing.T) {
	client := testdb.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	completedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", completedAt)
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	revisions := newDirectoryRevisionStore(t, ctx, client)
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	revoker := &observingTxTokenRevoker{delegate: authService}
	disableCalls := 0
	disabler := relayDisablerFunc(func(_ context.Context, userID int64) error {
		disableCalls++
		if userID != int64(*bob.RelayUserID) {
			t.Fatalf("DisableUser userID = %d, want %d", userID, *bob.RelayUserID)
		}
		cancel()
		return nil
	})
	revokedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	svc := NewService(client, ServiceOptions{
		RelayDisablers:            fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:              revoker,
		WorkItemCountsInvalidator: revisions,
		Now:                       func() time.Time { return revokedAt },
	})
	before := currentDirectoryRevision(t, ctx, revisions)

	action, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err != nil {
		t.Fatalf("DisableRelayUserForCandidate: %v", err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("request context error = %v, want canceled", ctx.Err())
	}
	if disableCalls != 1 {
		t.Fatalf("relay disable calls = %d, want 1", disableCalls)
	}
	if !revoker.sawUncancelled || !revoker.sawFiveSecond {
		t.Fatalf("finalization context uncancelled=%v five_second_deadline=%v, want true/true", revoker.sawUncancelled, revoker.sawFiveSecond)
	}
	if action.Status != directoryoffboardingaction.StatusSucceeded {
		t.Fatalf("action status = %s, want succeeded", action.Status)
	}
	reloaded := client.User.GetX(context.Background(), bob.ID)
	if reloaded.TokenValidAfter == nil || !reloaded.TokenValidAfter.Equal(revokedAt) {
		t.Fatalf("token_valid_after = %v, want %v", reloaded.TokenValidAfter, revokedAt)
	}
	if after := currentDirectoryRevision(t, context.Background(), revisions); after == before {
		t.Fatalf("revision after offboarding finalization = %q, want change", after)
	}
}

func TestServiceOffboardingFinalizationDeadlineStillRecordsPartialFailure(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	completedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", completedAt)
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	revisions := newDirectoryRevisionStore(t, ctx, client)
	disabler := &fakeRelayDisabler{}
	svc := NewService(client, ServiceOptions{
		RelayDisablers:            fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:              deadlineTxTokenRevoker{},
		WorkItemCountsInvalidator: revisions,
		Now:                       func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) },
	})
	before := currentDirectoryRevision(t, ctx, revisions)

	action, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DisableRelayUserForCandidate error = %v, want context deadline exceeded", err)
	}
	if len(disabler.disabled) != 1 {
		t.Fatalf("relay disable calls = %v, want one successful call", disabler.disabled)
	}
	if action == nil || action.Status != directoryoffboardingaction.StatusPartialFailed {
		t.Fatalf("action after finalization deadline = %+v, want partial_failed", action)
	}
	if reloaded := client.User.GetX(ctx, bob.ID); reloaded.TokenValidAfter != nil {
		t.Fatalf("token_valid_after after deadline rollback = %v, want nil", reloaded.TokenValidAfter)
	}
	if after := currentDirectoryRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after deadline rollback = %q, want %q", after, before)
	}
	stored := client.DirectoryOffboardingAction.GetX(ctx, action.ID)
	if stored.Status != directoryoffboardingaction.StatusPartialFailed {
		t.Fatalf("stored action status = %s, want partial_failed", stored.Status)
	}
}

func TestServiceOffboardingFinalizationRevisionFailureRollsBackLocalSuccess(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	completedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", completedAt)
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	revisions := newDirectoryRevisionStore(t, ctx, client)
	failNextRevision := installDirectoryRevisionFailureHook(client)
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	disabler := &fakeRelayDisabler{}
	svc := NewService(client, ServiceOptions{
		RelayDisablers:            fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:              authService,
		WorkItemCountsInvalidator: revisions,
		Now:                       func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) },
	})
	before := currentDirectoryRevision(t, ctx, revisions)
	failNextRevision()

	action, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded despite revision failure")
	}
	if len(disabler.disabled) != 1 {
		t.Fatalf("relay disable calls = %v, want one successful call", disabler.disabled)
	}
	if action == nil || action.Status != directoryoffboardingaction.StatusPartialFailed {
		t.Fatalf("action after finalization rollback = %+v, want partial_failed", action)
	}
	if reloaded := client.User.GetX(ctx, bob.ID); reloaded.TokenValidAfter != nil {
		t.Fatalf("token_valid_after after finalization rollback = %v, want nil", reloaded.TokenValidAfter)
	}
	if after := currentDirectoryRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after finalization rollback = %q, want %q", after, before)
	}
	stored := client.DirectoryOffboardingAction.GetX(ctx, action.ID)
	if stored.Status != directoryoffboardingaction.StatusPartialFailed {
		t.Fatalf("stored action status = %s, want partial_failed", stored.Status)
	}
}

func TestServiceOffboardingValidationAndConflictDoNotInvalidate(t *testing.T) {
	t.Run("email validation", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
		bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
		revisions := newDirectoryRevisionStore(t, ctx, client)
		svc := NewService(client, ServiceOptions{WorkItemCountsInvalidator: revisions})
		before := currentDirectoryRevision(t, ctx, revisions)

		if _, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
			UserID:            bob.ID,
			ConfirmEmail:      "alice@example.com",
			Reason:            offboardingReasonMissingFromDirectory,
			PerformedByUserID: bob.ID,
		}); err == nil {
			t.Fatal("DisableRelayUserForCandidate succeeded with mismatched email")
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after != before {
			t.Fatalf("revision after validation failure = %q, want %q", after, before)
		}
	})

	t.Run("current membership conflict", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "bob@example.org", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
		bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
		revisions := newDirectoryRevisionStore(t, ctx, client)
		svc := NewService(client, ServiceOptions{WorkItemCountsInvalidator: revisions})
		before := currentDirectoryRevision(t, ctx, revisions)

		if _, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
			UserID:            bob.ID,
			ConfirmEmail:      "bob@example.org",
			Reason:            offboardingReasonMissingFromDirectory,
			PerformedByUserID: bob.ID,
		}); err == nil {
			t.Fatal("DisableRelayUserForCandidate succeeded for current member")
		} else if _, ok := err.(*ConflictError); !ok {
			t.Fatalf("error = %T %v, want ConflictError", err, err)
		}
		if after := currentDirectoryRevision(t, ctx, revisions); after != before {
			t.Fatalf("revision after membership conflict = %q, want %q", after, before)
		}
	})
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

type offboardingUserFixture struct {
	username string
	email    string
}

func createRelayBoundUsers(t *testing.T, ctx context.Context, client *ent.Client, fixtures []offboardingUserFixture) []*ent.User {
	t.Helper()
	builders := make([]*ent.UserCreate, 0, len(fixtures))
	for index, fixture := range fixtures {
		builders = append(builders, client.User.Create().
			SetUsername(fixture.username).
			SetEmail(fixture.email).
			SetAuthSource(entuser.AuthSourceRelaySSO).
			SetRole(entuser.RoleUser).
			SetRelayUserID(100000+index))
	}
	users, err := client.User.CreateBulk(builders...).Save(ctx)
	if err != nil {
		t.Fatalf("create %d relay-bound users: %v", len(fixtures), err)
	}
	return users
}

func createDirectoryOffboardingAction(t *testing.T, ctx context.Context, client *ent.Client, sourceID, runID int, user *ent.User, status directoryoffboardingaction.Status) *ent.DirectoryOffboardingAction {
	t.Helper()
	action, err := client.DirectoryOffboardingAction.Create().
		SetSourceID(sourceID).
		SetUserID(user.ID).
		SetRelayUserID(*user.RelayUserID).
		SetDirectoryRunID(runID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(status).
		SetReason(offboardingReasonMissingFromDirectory).
		SetPerformedByUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create %s offboarding action for user %d: %v", status, user.ID, err)
	}
	return action
}

func assertOffboardingActionMetadata(t *testing.T, candidates []OffboardingCandidate, want *ent.DirectoryOffboardingAction) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.UserID != want.UserID {
			continue
		}
		if candidate.OffboardingStatus != string(want.Status) || candidate.OffboardingActionID == nil || *candidate.OffboardingActionID != want.ID {
			t.Fatalf("candidate action metadata = %+v, want status=%s id=%d", candidate, want.Status, want.ID)
		}
		return
	}
	t.Fatalf("candidate for action user %d not found in page", want.UserID)
}

func assertCandidateAbsent(t *testing.T, candidates []OffboardingCandidate, userID int, reason string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.UserID == userID {
			t.Fatalf("candidate user %d present despite %s", userID, reason)
		}
	}
}

func candidateUserIDs(candidates []OffboardingCandidate) []int {
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.UserID)
	}
	return ids
}

func assertNoOffboardingSideEffects(t *testing.T, ctx context.Context, client *ent.Client, disabler *fakeRelayDisabler, revoker *fakeTokenRevoker) {
	t.Helper()
	if len(disabler.disabled) != 0 || len(revoker.calls) != 0 {
		t.Fatalf("side effects disabled=%v revocations=%v, want none", disabler.disabled, revoker.calls)
	}
	if count := client.DirectoryOffboardingAction.Query().CountX(ctx); count != 0 {
		t.Fatalf("offboarding action count = %d, want 0", count)
	}
}

type entQueryRecorder struct {
	mu      sync.Mutex
	queries []string
}

func (r *entQueryRecorder) Log(values ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, fmt.Sprint(values...))
}

func (r *entQueryRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = nil
}

func (r *entQueryRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

func (r *entQueryRecorder) Joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.queries, "\n")
}

func (r *entQueryRecorder) ContainsBoundedCurrentSnapshotQuery() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, query := range r.queries {
		lower := strings.ToLower(query)
		if strings.Contains(lower, "directory_sync_runs") && strings.Contains(lower, "order by") && strings.Contains(lower, "limit") {
			return true
		}
	}
	return false
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

func directorySourceInput(source *ent.DirectorySource, name string) SourceInput {
	return SourceInput{
		Name:             name,
		Description:      source.Description,
		Scope:            source.Scope.String(),
		Enabled:          source.Enabled,
		DSL:              source.Dsl,
		ScheduleEnabled:  source.ScheduleEnabled,
		ScheduleInterval: source.ScheduleInterval.String(),
		ScheduleTimezone: source.ScheduleTimezone,
	}
}

func newDirectoryRevisionStore(t *testing.T, ctx context.Context, client *ent.Client) *workitems.RevisionStore {
	t.Helper()
	store := workitems.NewRevisionStore(client)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("initialize work item revision: %v", err)
	}
	return store
}

func currentDirectoryRevision(t *testing.T, ctx context.Context, store *workitems.RevisionStore) string {
	t.Helper()
	revision, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("read work item revision: %v", err)
	}
	return revision
}

func installDirectoryRevisionFailureHook(client *ent.Client) func() {
	var mu sync.Mutex
	failNext := false
	client.SystemSetting.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			mu.Lock()
			shouldFail := failNext
			if shouldFail {
				failNext = false
			}
			mu.Unlock()
			if shouldFail {
				return nil, fmt.Errorf("injected work item revision failure")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	return func() {
		mu.Lock()
		failNext = true
		mu.Unlock()
	}
}

func stringsReplaceAll(input string, replacements map[string]string) string {
	out := input
	for old, replacement := range replacements {
		out = strings.ReplaceAll(out, old, replacement)
	}
	return out
}
