package toolusage

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestEventSummaryAndListShareFilterSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	fixture := seedEventFilterFixture(t, client)
	svc := NewQueryService(client)
	from, to := fixture.From, fixture.To

	tests := []struct {
		name   string
		filter queryFilter
		want   []string
	}{
		{"time inclusive", queryFilter{From: from, To: to}, []string{"time-from", "time-to"}},
		{"tool", queryFilter{Tool: "codex"}, []string{"q-session"}},
		{"repo", queryFilter{RepoID: fixture.Alpha.RepoConfigID}, []string{"q-dedupe"}},
		{"bound", queryFilter{BindingStatus: "bound"}, []string{"q-commit"}},
		{"unbound", queryFilter{BindingStatus: "unbound"}, []string{"q-source"}},
		{"q session", queryFilter{Q: "SESSION-NEEDLE"}, []string{"q-session"}},
		{"q event", queryFilter{Q: "EVENT-NEEDLE"}, []string{"q-event"}},
		{"q dedupe", queryFilter{Q: "DEDUPE-NEEDLE"}, []string{"q-dedupe"}},
		{"q commit", queryFilter{Q: "COMMIT-NEEDLE"}, []string{"q-commit"}},
		{"q source", queryFilter{Q: "SOURCE-NEEDLE.JSONL"}, []string{"q-source"}},
		{"q wildcard is literal", queryFilter{Q: "%"}, nil},
		{"q underscore is literal", queryFilter{Q: "LITERAL_UNDERSCORE"}, []string{"q-session"}},
		{"q backslash is literal", queryFilter{Q: "LITERAL\\BACKSLASH"}, []string{"q-source"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := tt.filter
			switch tt.name {
			case "time inclusive", "unbound":
				filter.ActorUserID = fixture.Alpha.UserID
				filter.ActorRole = string(user.RoleUser)
			case "tool", "repo", "bound":
				filter.ActorUserID = fixture.Beta.UserID
				filter.ActorRole = string(user.RoleAdmin)
				filter.UserID = fixture.Beta.UserID
			default:
				filter.ActorUserID = fixture.Beta.UserID
				filter.ActorRole = string(user.RoleAdmin)
			}
			assertSummaryAndListFilter(t, ctx, svc, fixture.EventNames, filter, tt.want)
		})
	}

	t.Run("source directory is not searchable", func(t *testing.T) {
		assertSummaryAndListFilter(t, ctx, svc, fixture.EventNames, queryFilter{
			ActorUserID: fixture.Beta.UserID,
			ActorRole:   string(user.RoleAdmin),
			Q:           "directory-only-needle",
		}, nil)
	})
}

func TestGetSummaryUsesDatabaseAggregates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	fixture := seedEventFilterFixture(t, client)

	var (
		logsMu sync.Mutex
		logs   []string
	)
	recordingClient, err := ent.Open(
		"postgres",
		dsn,
		ent.Debug(),
		ent.Log(func(values ...any) {
			logsMu.Lock()
			defer logsMu.Unlock()
			logs = append(logs, fmt.Sprint(values...))
		}),
	)
	if err != nil {
		t.Fatalf("open recording ent client: %v", err)
	}
	t.Cleanup(func() { recordingClient.Close() })

	summary, err := NewQueryService(recordingClient).GetSummary(ctx, SummaryRequest{
		ActorUserID: fixture.Beta.UserID,
		ActorRole:   string(user.RoleAdmin),
	})
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	want := &SummaryResponse{
		TotalEvents:   8,
		BoundEvents:   3,
		UnboundEvents: 5,
		ToolCounts: []ToolCountDTO{
			{Tool: "claude", Count: 4},
			{Tool: "codex", Count: 1},
			{Tool: "kiro", Count: 3},
		},
	}
	if !reflect.DeepEqual(summary, want) {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}

	logsMu.Lock()
	captured := append([]string(nil), logs...)
	logsMu.Unlock()
	var summaryQueries []string
	for _, entry := range captured {
		upper := strings.ToUpper(entry)
		if !strings.Contains(upper, `FROM "TOOL_USAGE_EVENTS"`) {
			continue
		}
		summaryQueries = append(summaryQueries, entry)
		if strings.Contains(strings.ToLower(entry), "raw_payload") {
			t.Errorf("summary query projects raw_payload: %s", entry)
		}
		if !strings.Contains(upper, "COUNT(") && !strings.Contains(upper, "GROUP BY") {
			t.Errorf("summary query is not an aggregate: %s", entry)
		}
	}
	if len(summaryQueries) < 4 {
		t.Errorf("captured %d summary aggregate queries, want at least 4; logs:\n%s", len(summaryQueries), strings.Join(captured, "\n"))
	}
}

func assertSummaryAndListFilter(
	t *testing.T,
	ctx context.Context,
	svc *QueryService,
	eventNames map[int]string,
	filter queryFilter,
	want []string,
) {
	t.Helper()

	rows, total, err := svc.ListEvents(ctx, ListEventsRequest{
		ActorUserID:   filter.ActorUserID,
		ActorRole:     filter.ActorRole,
		From:          filter.From,
		To:            filter.To,
		Tool:          filter.Tool,
		RepoID:        filter.RepoID,
		BindingStatus: filter.BindingStatus,
		UserID:        filter.UserID,
		Q:             filter.Q,
		Limit:         100,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		name, ok := eventNames[row.ID]
		if !ok {
			t.Fatalf("ListEvents returned unknown event ID %d", row.ID)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	normalizedWant := append(make([]string, 0, len(want)), want...)
	sort.Strings(normalizedWant)
	if len(got) != len(normalizedWant) {
		t.Fatalf("ListEvents names = %v, want %v", got, normalizedWant)
	}
	for i := range got {
		if got[i] != normalizedWant[i] {
			t.Fatalf("ListEvents names = %v, want %v", got, normalizedWant)
		}
	}
	if total != len(normalizedWant) {
		t.Fatalf("ListEvents total = %d, want %d", total, len(normalizedWant))
	}

	summary, err := svc.GetSummary(ctx, SummaryRequest{
		ActorUserID:   filter.ActorUserID,
		ActorRole:     filter.ActorRole,
		From:          filter.From,
		To:            filter.To,
		Tool:          filter.Tool,
		RepoID:        filter.RepoID,
		BindingStatus: filter.BindingStatus,
		UserID:        filter.UserID,
		Q:             filter.Q,
	})
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.TotalEvents != total {
		t.Fatalf("summary total = %d, list total = %d", summary.TotalEvents, total)
	}
	if summary.ToolCounts == nil {
		t.Fatal("summary tool counts = nil, want an empty slice")
	}
	toolCounts := make(map[string]int)
	bound, unbound := 0, 0
	for _, row := range rows {
		toolCounts[row.Tool]++
		if row.BindingStatus == "bound" {
			bound++
		} else {
			unbound++
		}
	}
	if summary.BoundEvents != bound || summary.UnboundEvents != unbound {
		t.Fatalf(
			"summary binding counts = %d/%d, list counts = %d/%d",
			summary.BoundEvents,
			summary.UnboundEvents,
			bound,
			unbound,
		)
	}
	for _, item := range summary.ToolCounts {
		if toolCounts[item.Tool] != item.Count {
			t.Fatalf("summary tool count for %q = %d, list count = %d", item.Tool, item.Count, toolCounts[item.Tool])
		}
		delete(toolCounts, item.Tool)
	}
	if len(toolCounts) != 0 {
		t.Fatalf("summary omitted list tool counts: %v", toolCounts)
	}
}

type boundedEventFixture struct {
	ActorUserID int
	EventIDs    []int
	ObservedAt  time.Time
	RawPayload  string
}

func seedBoundedEventFixture(t *testing.T, client *ent.Client) boundedEventFixture {
	t.Helper()

	ctx := context.Background()
	scope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(scope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetRole(user.RoleAdmin).
		ExecX(ctx)
	client.RepoConfig.UpdateOneID(scope.RepoConfigID).
		SetName("alpha").
		SetFullName("org/alpha").
		ExecX(ctx)

	observedAt := time.Date(2026, time.July, 15, 3, 0, 0, 0, time.UTC)
	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("bounded-list-checkpoint").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("bounded-list-sha").
		SetParentShas([]string{"bounded-list-parent"}).
		SetBindingSource(commitcheckpoint.BindingSourceManual).
		SetCapturedAt(observedAt.Add(-time.Minute)).
		SaveX(ctx)

	rawPayload := strings.Repeat("x", 16*1024)
	creates := make([]*ent.ToolUsageEventCreate, 0, 125)
	for i := 0; i < 125; i++ {
		create := client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID(fmt.Sprintf("tie-session-%03d", i)).
			SetToolEventID(fmt.Sprintf("tie-event-%03d", i)).
			SetDedupeKey(fmt.Sprintf("tie-dedupe-%03d", i)).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetRequestCount(i + 1).
			SetInputTokens(int64(1000 + i)).
			SetOutputTokens(int64(2000 + i)).
			SetCachedInputTokens(int64(3000 + i)).
			SetReasoningTokens(int64(4000 + i)).
			SetCreditUsage(float64(i) + 0.5).
			SetContextUsagePct(50).
			SetObservedStartAt(observedAt.Add(-time.Second)).
			SetObservedEndAt(observedAt).
			SetRawSourcePath(fmt.Sprintf("/synthetic/events/event-%03d.jsonl", i)).
			SetRawSourceLocator(fmt.Sprintf("line:%d", i+1)).
			SetRawPayload(map[string]any{"content": rawPayload})
		if i == 124 {
			create.SetCommitCheckpointID(checkpoint.ID)
		}
		creates = append(creates, create)
	}
	events := client.ToolUsageEvent.CreateBulk(creates...).SaveX(ctx)
	eventIDs := make([]int, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.ID)
	}

	return boundedEventFixture{
		ActorUserID: scope.UserID,
		EventIDs:    eventIDs,
		ObservedAt:  observedAt,
		RawPayload:  rawPayload,
	}
}

func listBoundedEvents(t *testing.T, svc *QueryService, fixture boundedEventFixture, limit, offset int) ([]EventListRow, int) {
	t.Helper()

	rows, total, err := svc.ListEvents(context.Background(), ListEventsRequest{
		ActorUserID: fixture.ActorUserID,
		ActorRole:   string(user.RoleAdmin),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return rows, total
}

func assertDescendingEventIDs(t *testing.T, rows []EventListRow, eventIDs []int, offset int) {
	t.Helper()

	for i, row := range rows {
		want := eventIDs[len(eventIDs)-1-offset-i]
		if row.ID != want {
			t.Fatalf("row %d id = %d, want %d", i, row.ID, want)
		}
	}
}

func TestListEventsDefaultsToTwenty(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	rows, total := listBoundedEvents(t, NewQueryService(client), fixture, 0, 0)

	if total != 125 {
		t.Fatalf("total = %d, want 125", total)
	}
	if len(rows) != 20 {
		t.Fatalf("rows = %d, want 20", len(rows))
	}
	assertDescendingEventIDs(t, rows, fixture.EventIDs, 0)

	first := rows[0]
	checkpointID := client.CommitCheckpoint.Query().
		Where(commitcheckpoint.CommitShaEQ("bounded-list-sha")).
		OnlyIDX(context.Background())
	wantFirst := EventListRow{
		ID:                 fixture.EventIDs[124],
		Tool:               "codex",
		RepoID:             client.RepoConfig.Query().OnlyIDX(context.Background()),
		RepoName:           "org/alpha",
		Username:           "alice",
		ToolSessionID:      "tie-session-124",
		ToolEventID:        "tie-event-124",
		DedupeKey:          "tie-dedupe-124",
		ObservedEndAt:      fixture.ObservedAt,
		RequestCount:       125,
		InputTokens:        1124,
		OutputTokens:       2124,
		CachedInputTokens:  3124,
		ReasoningTokens:    4124,
		CreditUsage:        124.5,
		CommitCheckpointID: &checkpointID,
		CommitSHA:          "bounded-list-sha",
		SourceBasename:     "event-124.jsonl",
		BindingStatus:      "bound",
	}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("first row = %+v, want %+v", first, wantFirst)
	}
}

func TestListEventsClampsLimitToOneHundred(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	svc := NewQueryService(client)
	for _, limit := range []int{101, 1000} {
		rows, total := listBoundedEvents(t, svc, fixture, limit, 0)
		if total != 125 {
			t.Fatalf("limit %d total = %d, want 125", limit, total)
		}
		if len(rows) != 100 {
			t.Fatalf("limit %d rows = %d, want 100", limit, len(rows))
		}
		assertDescendingEventIDs(t, rows, fixture.EventIDs, 0)
	}
}

func TestListEventsNormalizesNegativeOffset(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	svc := NewQueryService(client)
	zeroOffset, zeroTotal := listBoundedEvents(t, svc, fixture, 20, 0)
	negativeOffset, negativeTotal := listBoundedEvents(t, svc, fixture, 20, -5)

	if zeroTotal != 125 || negativeTotal != 125 {
		t.Fatalf("totals = %d/%d, want 125/125", zeroTotal, negativeTotal)
	}
	if !reflect.DeepEqual(negativeOffset, zeroOffset) {
		t.Fatalf("negative offset rows differ from zero offset: negative=%v zero=%v", negativeOffset, zeroOffset)
	}
}

func TestListEventsOrdersEqualObservedEndAtByIDDescending(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	rows, total := listBoundedEvents(t, NewQueryService(client), fixture, 100, 0)

	if total != 125 || len(rows) != 100 {
		t.Fatalf("total/rows = %d/%d, want 125/100", total, len(rows))
	}
	assertDescendingEventIDs(t, rows, fixture.EventIDs, 0)
}

func TestListEventsPagesWithoutTieDuplicatesOrOmissions(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	svc := NewQueryService(client)
	first, firstTotal := listBoundedEvents(t, svc, fixture, 20, 0)
	second, secondTotal := listBoundedEvents(t, svc, fixture, 20, 20)

	if firstTotal != 125 || secondTotal != 125 {
		t.Fatalf("totals = %d/%d, want 125/125", firstTotal, secondTotal)
	}
	assertDescendingEventIDs(t, first, fixture.EventIDs, 0)
	assertDescendingEventIDs(t, second, fixture.EventIDs, 20)
	seen := make(map[int]struct{}, len(first)+len(second))
	for _, row := range append(first, second...) {
		if _, duplicate := seen[row.ID]; duplicate {
			t.Fatalf("event id %d appeared on both pages", row.ID)
		}
		seen[row.ID] = struct{}{}
	}
	if len(seen) != 40 {
		t.Fatalf("unique ids = %d, want 40", len(seen))
	}
}

func TestListEventsPrimarySelectOmitsRawPayload(t *testing.T) {
	t.Parallel()

	client, dsn := testdb.OpenWithDSN(t)
	fixture := seedBoundedEventFixture(t, client)
	var (
		logsMu sync.Mutex
		logs   []string
	)
	recordingClient, err := ent.Open(
		"postgres",
		dsn,
		ent.Debug(),
		ent.Log(func(values ...any) {
			logsMu.Lock()
			defer logsMu.Unlock()
			logs = append(logs, fmt.Sprint(values...))
		}),
	)
	if err != nil {
		t.Fatalf("open recording ent client: %v", err)
	}
	t.Cleanup(func() { recordingClient.Close() })

	rows, total := listBoundedEvents(t, NewQueryService(recordingClient), fixture, 20, 0)
	if total != 125 || len(rows) != 20 {
		t.Fatalf("total/rows = %d/%d, want 125/20", total, len(rows))
	}

	logsMu.Lock()
	captured := append([]string(nil), logs...)
	logsMu.Unlock()
	selectedByTable := make(map[string]string)
	var primaryQuery string
	for _, entry := range captured {
		upper := strings.ToUpper(entry)
		for _, table := range []string{"tool_usage_events", "repo_configs", "users", "commit_checkpoints"} {
			marker := ` FROM "` + strings.ToUpper(table) + `"`
			if index := strings.Index(upper, marker); index >= 0 {
				if table != "tool_usage_events" || (strings.Contains(upper, " ORDER BY ") && strings.Contains(upper, " LIMIT ")) {
					selectedByTable[table] = entry[:index]
					if table == "tool_usage_events" {
						primaryQuery = entry
					}
				}
			}
		}
	}
	primary, ok := selectedByTable["tool_usage_events"]
	if !ok {
		t.Fatalf("primary bounded list query not captured; logs:\n%s", strings.Join(captured, "\n"))
	}
	primaryUpper := strings.ToUpper(primaryQuery)
	if !strings.Contains(primaryUpper, `"OBSERVED_END_AT" DESC`) || !strings.Contains(primaryUpper, `"ID" DESC`) {
		t.Errorf("primary list query lacks stable descending order: %s", primaryQuery)
	}
	if !strings.Contains(primaryUpper, " LIMIT ") {
		t.Errorf("primary list query lacks LIMIT: %s", primaryQuery)
	}
	for _, field := range []string{"raw_payload", "workspace_id", "observed_start_at", "context_usage_pct", "created_at"} {
		if strings.Contains(strings.ToLower(primary), `"`+field+`"`) {
			t.Errorf("primary list select includes %s: %s", field, primary)
		}
	}

	edgeForbidden := map[string][]string{
		"repo_configs":       {"clone_url", "webhook_secret", "relay_group_id"},
		"users":              {"email", "relay_auth_password", "ldap_dn"},
		"commit_checkpoints": {"parent_shas", "agent_snapshot", "captured_at"},
	}
	for table, forbidden := range edgeForbidden {
		selected, ok := selectedByTable[table]
		if !ok {
			t.Errorf("projected %s edge query not captured; logs:\n%s", table, strings.Join(captured, "\n"))
			continue
		}
		for _, field := range forbidden {
			if strings.Contains(strings.ToLower(selected), `"`+field+`"`) {
				t.Errorf("%s edge select includes %s: %s", table, field, selected)
			}
		}
	}
}

func TestListEventsKeepsAdminDetailRawPayloadComplete(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	fixture := seedBoundedEventFixture(t, client)
	detail, err := NewQueryService(client).GetEventDetail(context.Background(), GetEventDetailRequest{
		ActorUserID: fixture.ActorUserID,
		ActorRole:   string(user.RoleAdmin),
		EventID:     fixture.EventIDs[len(fixture.EventIDs)-1],
	})
	if err != nil {
		t.Fatalf("GetEventDetail: %v", err)
	}
	if got, ok := detail.RawPayload["content"].(string); !ok || got != fixture.RawPayload {
		t.Fatalf("raw payload content length = %d, want %d", len(got), len(fixture.RawPayload))
	}
	if detail.RawSourcePath != "/synthetic/events/event-124.jsonl" || detail.RawSourceLocator != "line:125" {
		t.Fatalf("raw source = %q/%q, want full admin diagnostics", detail.RawSourcePath, detail.RawSourceLocator)
	}
}

func TestListEventsScopesRegularUserToOwnRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	viewerScope := seedToolUsageScope(t, client)
	adminScope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(adminScope.UserID).
		SetRole(user.RoleAdmin).
		ExecX(ctx)

	viewerObservedAt := time.Now().Add(-5 * time.Minute).UTC()
	adminObservedAt := time.Now().Add(-4 * time.Minute).UTC()

	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(viewerScope.WorkspaceID).
		SetRepoConfigID(viewerScope.RepoConfigID).
		SetUserID(viewerScope.UserID).
		SetToolSessionID("sess-viewer").
		SetToolEventID("viewer-event").
		SetDedupeKey("viewer-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(viewerObservedAt.Add(-1 * time.Second)).
		SetObservedEndAt(viewerObservedAt).
		SetRawSourcePath("/Users/admin/.claude/projects/viewer.jsonl").
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(adminScope.WorkspaceID).
		SetRepoConfigID(adminScope.RepoConfigID).
		SetUserID(adminScope.UserID).
		SetToolSessionID("sess-admin").
		SetToolEventID("admin-event").
		SetDedupeKey("admin-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(adminObservedAt.Add(-1 * time.Second)).
		SetObservedEndAt(adminObservedAt).
		SetRawSourcePath("/Users/admin/.codex/sessions/admin.jsonl").
		SaveX(ctx)

	svc := NewQueryService(client)
	rows, total, err := svc.ListEvents(ctx, ListEventsRequest{
		ActorUserID: viewerScope.UserID,
		ActorRole:   string(user.RoleUser),
		From:        time.Now().Add(-24 * time.Hour).UTC(),
		To:          time.Now().UTC(),
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("got total=%d rows=%d, want 1/1", total, len(rows))
	}
	if rows[0].ToolSessionID != "sess-viewer" {
		t.Fatalf("row session=%q, want sess-viewer", rows[0].ToolSessionID)
	}
	if rows[0].SourceBasename != "viewer.jsonl" {
		t.Fatalf("source basename=%q, want viewer.jsonl", rows[0].SourceBasename)
	}
}

func TestSearchEventUsersReturnsOnlyUsersWithEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	noEventScope := seedToolUsageScope(t, client)

	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetRole(user.RoleAdmin).
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetRole(user.RoleUser).
		ExecX(ctx)
	client.User.UpdateOneID(noEventScope.UserID).
		SetUsername("carol").
		SetEmail("carol@example.net").
		ExecX(ctx)

	aliceLatest := time.Now().Add(-2 * time.Minute).UTC()
	bobLatest := time.Now().Add(-10 * time.Minute).UTC()
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-1").
		SetDedupeKey("alice-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-1 * time.Second)).
		SetObservedEndAt(aliceLatest).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-2").
		SetDedupeKey("alice-2").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-2 * time.Second)).
		SetObservedEndAt(aliceLatest.Add(-1 * time.Second)).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID(bobScope.WorkspaceID).
		SetRepoConfigID(bobScope.RepoConfigID).
		SetUserID(bobScope.UserID).
		SetToolSessionID("bob-session-1").
		SetDedupeKey("bob-1").
		SetUsageUnit(toolusageevent.UsageUnitCredit).
		SetObservedStartAt(bobLatest.Add(-1 * time.Second)).
		SetObservedEndAt(bobLatest).
		SaveX(ctx)

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Limit: 20})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users=%d, want 2: %+v", len(users), users)
	}
	if users[0].Email != "alice@example.com" || users[0].EventCount != 2 {
		t.Fatalf("first user=%+v, want alice with 2 events", users[0])
	}
	if users[1].Email != "bob@example.org" || users[1].EventCount != 1 {
		t.Fatalf("second user=%+v, want bob with 1 event", users[1])
	}
}

func TestSearchEventUsersFiltersByEmailOrUsernameAndClampsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		ExecX(ctx)

	for i, scope := range []TestToolUsageScope{aliceScope, bobScope} {
		observedAt := time.Now().Add(time.Duration(-i) * time.Minute).UTC()
		client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID(fmt.Sprintf("search-session-%d", i)).
			SetDedupeKey(fmt.Sprintf("search-dedupe-%d", i)).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetObservedStartAt(observedAt.Add(-1 * time.Second)).
			SetObservedEndAt(observedAt).
			SaveX(ctx)
	}

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "EXAMPLE.ORG", Limit: 100})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users=%d, want 1: %+v", len(users), users)
	}
	if users[0].Username != "bob" || users[0].Email != "bob@example.org" {
		t.Fatalf("user=%+v, want bob", users[0])
	}

	users, err = svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "ali", Limit: 0})
	if err != nil {
		t.Fatalf("SearchEventUsers username: %v", err)
	}
	if len(users) != 1 || users[0].Email != "alice@example.com" {
		t.Fatalf("users=%+v, want alice", users)
	}
}

func TestGetEventDetailRedactsRawFieldsForRegularUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	scope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(scope.UserID).
		SetRole(user.RoleUser).
		ExecX(ctx)

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-detail-1").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("detail-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(time.Now().Add(-10 * time.Minute).UTC()).
		SaveX(ctx)

	event := client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetUserID(scope.UserID).
		SetToolSessionID("detail-session").
		SetToolEventID("detail-event").
		SetDedupeKey("detail-dedupe").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(time.Now().Add(-9 * time.Minute).UTC()).
		SetObservedEndAt(time.Now().Add(-9 * time.Minute).UTC()).
		SetRawSourcePath("/Users/admin/.claude/projects/detail.jsonl").
		SetRawSourceLocator("line:42").
		SetRawPayload(map[string]any{"kind": "assistant", "value": "secret"}).
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	pr := client.PrRecord.Create().
		SetRepoConfigID(scope.RepoConfigID).
		SetScmPrID(38).
		SetTitle("detail pr").
		SetStatus(prrecord.StatusOpen).
		SetAuthor("arthas").
		SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("detail-sha").
		SetCommitCheckpointID(checkpoint.ID).
		SetCapturedAt(checkpoint.CapturedAt).
		SaveX(ctx)

	svc := NewQueryService(client)

	userDetail, err := svc.GetEventDetail(ctx, GetEventDetailRequest{
		ActorUserID: scope.UserID,
		ActorRole:   string(user.RoleUser),
		EventID:     event.ID,
	})
	if err != nil {
		t.Fatalf("GetEventDetail regular user: %v", err)
	}
	if userDetail.RawSourcePath != "" {
		t.Fatalf("RawSourcePath=%q, want redacted empty", userDetail.RawSourcePath)
	}
	if userDetail.RawSourceLocator != "" {
		t.Fatalf("RawSourceLocator=%q, want redacted empty", userDetail.RawSourceLocator)
	}
	if userDetail.RawPayload != nil {
		t.Fatalf("RawPayload=%v, want nil", userDetail.RawPayload)
	}
	if len(userDetail.MatchedPRs) != 1 || userDetail.MatchedPRs[0].ScmPRID != 38 {
		t.Fatalf("MatchedPRs=%+v, want one PR #38", userDetail.MatchedPRs)
	}

	adminDetail, err := svc.GetEventDetail(ctx, GetEventDetailRequest{
		ActorUserID: scope.UserID,
		ActorRole:   string(user.RoleAdmin),
		EventID:     event.ID,
	})
	if err != nil {
		t.Fatalf("GetEventDetail admin: %v", err)
	}
	if adminDetail.RawSourcePath != "/Users/admin/.claude/projects/detail.jsonl" {
		t.Fatalf("RawSourcePath=%q, want full path", adminDetail.RawSourcePath)
	}
	if adminDetail.RawSourceLocator != "line:42" {
		t.Fatalf("RawSourceLocator=%q, want line:42", adminDetail.RawSourceLocator)
	}
	if adminDetail.RawPayload == nil || adminDetail.RawPayload["kind"] != "assistant" {
		t.Fatalf("RawPayload=%v, want assistant payload", adminDetail.RawPayload)
	}
}
