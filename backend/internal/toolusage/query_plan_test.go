package toolusage

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type recordedQuery struct {
	SQL  string
	Args []any
}

type recordingDriver struct {
	dialect.Driver
	queriesMu sync.Mutex
	queries   []recordedQuery
}

func (d *recordingDriver) Query(ctx context.Context, query string, args, v any) error {
	recorded := recordedQuery{SQL: query}
	if values, ok := args.([]any); ok {
		recorded.Args = append([]any(nil), values...)
	}
	d.queriesMu.Lock()
	d.queries = append(d.queries, recorded)
	d.queriesMu.Unlock()
	return d.Driver.Query(ctx, query, args, v)
}

func (d *recordingDriver) reset() {
	d.queriesMu.Lock()
	d.queries = nil
	d.queriesMu.Unlock()
}

func (d *recordingDriver) snapshot() []recordedQuery {
	d.queriesMu.Lock()
	defer d.queriesMu.Unlock()

	out := make([]recordedQuery, len(d.queries))
	for i, query := range d.queries {
		out[i] = recordedQuery{
			SQL:  query.SQL,
			Args: append([]any(nil), query.Args...),
		}
	}
	return out
}

type largeEventTestEnv struct {
	client   *ent.Client
	db       *sql.DB
	recorder *recordingDriver
	fixture  largeEventFixture
}

func openLargeEventTestEnv(t *testing.T) largeEventTestEnv {
	t.Helper()

	seedClient, dsn := testdb.OpenWithDSN(t)
	fixture := seedLargeEventFixture(t, seedClient)
	assertLargeEventFixtureShape(t, fixture)
	if count := seedClient.ToolUsageEvent.Query().CountX(context.Background()); count != largeEventFixtureSize {
		t.Fatalf("persisted scale events = %d, want %d", count, largeEventFixtureSize)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open scale fixture database: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), "ANALYZE tool_usage_events"); err != nil {
		db.Close()
		t.Fatalf("analyze scale fixture: %v", err)
	}

	driver := entsql.OpenDB(dialect.Postgres, db)
	recorder := &recordingDriver{Driver: driver}
	client := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close recording ent client: %v", err)
		}
	})

	return largeEventTestEnv{
		client:   client,
		db:       db,
		recorder: recorder,
		fixture:  fixture,
	}
}

func TestLargeEventFixturePreservesFiltersAndBounds(t *testing.T) {
	env := openLargeEventTestEnv(t)
	svc := NewQueryService(env.client)
	adminFilter := queryFilter{
		ActorUserID: env.fixture.BobUserID,
		ActorRole:   string(user.RoleAdmin),
	}

	tests := []struct {
		name    string
		filter  queryFilter
		matches func(largeEventRecord) bool
	}{
		{
			name: "regular actor scope",
			filter: queryFilter{
				ActorUserID: env.fixture.AliceUserID,
				ActorRole:   string(user.RoleUser),
				UserID:      env.fixture.BobUserID,
			},
			matches: func(record largeEventRecord) bool {
				return record.UserID == env.fixture.AliceUserID
			},
		},
		{
			name:   "admin user_id",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.UserID = env.fixture.AliceUserID }),
			matches: func(record largeEventRecord) bool {
				return record.UserID == env.fixture.AliceUserID
			},
		},
		{
			name: "inclusive time",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) {
				filter.From = env.fixture.BaseTime.Add(10 * time.Minute)
				filter.To = env.fixture.BaseTime.Add(20 * time.Minute)
			}),
			matches: func(record largeEventRecord) bool {
				return !record.Row.ObservedEndAt.Before(env.fixture.BaseTime.Add(10*time.Minute)) &&
					!record.Row.ObservedEndAt.After(env.fixture.BaseTime.Add(20*time.Minute))
			},
		},
		{
			name:   "tool",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Tool = "codex" }),
			matches: func(record largeEventRecord) bool {
				return record.Row.Tool == "codex"
			},
		},
		{
			name:   "repository",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.RepoID = env.fixture.AlphaRepoID }),
			matches: func(record largeEventRecord) bool {
				return record.RepoID == env.fixture.AlphaRepoID
			},
		},
		{
			name:   "bound",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.BindingStatus = "bound" }),
			matches: func(record largeEventRecord) bool {
				return record.Bound
			},
		},
		{
			name:   "unbound",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.BindingStatus = "unbound" }),
			matches: func(record largeEventRecord) bool {
				return !record.Bound
			},
		},
		{
			name:   "unrecognized binding status",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.BindingStatus = "unknown" }),
			matches: func(largeEventRecord) bool {
				return true
			},
		},
		{
			name:   "q tool_session_id",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Q = "SCALE-SESSION-0101" }),
			matches: func(record largeEventRecord) bool {
				return record.Ordinal == 101
			},
		},
		{
			name:   "q tool_event_id",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Q = "SCALE-EVENT-0202" }),
			matches: func(record largeEventRecord) bool {
				return record.Ordinal == 202
			},
		},
		{
			name:   "q dedupe_key",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Q = "SCALE-DEDUPE-0303" }),
			matches: func(record largeEventRecord) bool {
				return record.Ordinal == 303
			},
		},
		{
			name:   "q commit_sha",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Q = "COMMIT-NEEDLE-0040" }),
			matches: func(record largeEventRecord) bool {
				return record.Ordinal == largeEventCommitNeedleIndex
			},
		},
		{
			name:   "q source_basename",
			filter: withLargeEventFilter(adminFilter, func(filter *queryFilter) { filter.Q = "SOURCE-0404.JSONL" }),
			matches: func(record largeEventRecord) bool {
				return record.Ordinal == 404
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := matchingLargeEventRecords(env.fixture.Records, tt.matches)
			summary, err := svc.GetSummary(context.Background(), summaryRequestFromLargeEventFilter(tt.filter))
			if err != nil {
				t.Fatalf("GetSummary: %v", err)
			}
			assertLargeEventSummary(t, summary, expected)

			env.recorder.reset()
			rows, total, err := svc.ListEvents(
				context.Background(),
				listRequestFromLargeEventFilter(tt.filter, 100, 0),
			)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if total != summary.TotalEvents {
				t.Fatalf("list total = %d, summary total = %d", total, summary.TotalEvents)
			}
			if len(rows) > MaxEventPageSize {
				t.Fatalf("materialized rows = %d, want at most %d", len(rows), MaxEventPageSize)
			}
			wantRows := largeEventRows(expected)
			if len(wantRows) > MaxEventPageSize {
				wantRows = wantRows[:MaxEventPageSize]
			}
			if !reflect.DeepEqual(rows, wantRows) {
				t.Fatalf("visible rows differ:\n got: %+v\nwant: %+v", rows, wantRows)
			}

			listQuery := requireRecordedListQuery(t, env.recorder.snapshot())
			assertListSQLShape(t, listQuery)
			assertRawPayloadMarkerAbsent(t, listQuery)
		})
	}
}

func TestLargeEventFixtureStablePages(t *testing.T) {
	env := openLargeEventTestEnv(t)
	svc := NewQueryService(env.client)
	expected := matchingLargeEventRecords(env.fixture.Records, func(largeEventRecord) bool { return true })
	wantRows := largeEventRows(expected)

	for _, limit := range []int{20, 50, 100} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			allRows := make([]EventListRow, 0, largeEventFixtureSize)
			for offset := 0; offset < largeEventFixtureSize; offset += limit {
				rows, total, err := svc.ListEvents(context.Background(), ListEventsRequest{
					ActorUserID: env.fixture.BobUserID,
					ActorRole:   string(user.RoleAdmin),
					Limit:       limit,
					Offset:      offset,
				})
				if err != nil {
					t.Fatalf("ListEvents offset %d: %v", offset, err)
				}
				if total != largeEventFixtureSize {
					t.Fatalf("offset %d total = %d, want %d", offset, total, largeEventFixtureSize)
				}
				if len(rows) > limit {
					t.Fatalf("offset %d rows = %d, want at most %d", offset, len(rows), limit)
				}
				allRows = append(allRows, rows...)
			}

			if len(allRows) != largeEventFixtureSize {
				t.Fatalf("concatenated rows = %d, want %d", len(allRows), largeEventFixtureSize)
			}
			seen := make(map[int]struct{}, len(allRows))
			for i, row := range allRows {
				if _, duplicate := seen[row.ID]; duplicate {
					t.Fatalf("event id %d is duplicated at position %d", row.ID, i)
				}
				seen[row.ID] = struct{}{}
				if i > 0 && !largeEventRowBefore(allRows[i-1], row) {
					t.Fatalf("rows out of global order at %d: previous=%+v current=%+v", i, allRows[i-1], row)
				}
			}
			if !reflect.DeepEqual(allRows, wantRows) {
				t.Fatalf("concatenated visible rows differ from fixture order")
			}
		})
	}
}

func TestLargeEventFixtureQueryPlans(t *testing.T) {
	env := openLargeEventTestEnv(t)
	svc := NewQueryService(env.client)

	listCases := []struct {
		name string
		req  ListEventsRequest
	}{
		{
			name: "global default",
			req: ListEventsRequest{
				ActorUserID: env.fixture.BobUserID,
				ActorRole:   string(user.RoleAdmin),
				Limit:       100,
			},
		},
		{
			name: "regular user default",
			req: ListEventsRequest{
				ActorUserID: env.fixture.AliceUserID,
				ActorRole:   string(user.RoleUser),
				Limit:       100,
			},
		},
	}
	for _, tt := range listCases {
		t.Run(tt.name, func(t *testing.T) {
			env.recorder.reset()
			rows, _, err := svc.ListEvents(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(rows) != MaxEventPageSize {
				t.Fatalf("materialized rows = %d, want %d", len(rows), MaxEventPageSize)
			}

			listQuery := requireRecordedListQuery(t, env.recorder.snapshot())
			assertListSQLShape(t, listQuery)
			plan := explainRecordedQuery(t, env.db, listQuery)
			limitNode, ok := findPlanNode(plan, func(node explainPlanNode) bool {
				return node.NodeType == "Limit"
			})
			if !ok {
				t.Fatalf("list plan has no Limit node:\n%s", formatExplainPlan(plan))
			}
			if limitNode.ActualRows > MaxEventPageSize {
				t.Fatalf("Limit Actual Rows = %.0f, want at most %d:\n%s", limitNode.ActualRows, MaxEventPageSize, formatExplainPlan(plan))
			}
			selectedIndex, ok := planUsesOneOfIndexes(plan,
				"toolusageevent_observed_end_at_id",
				"toolusageevent_user_id_observed_end_at_id",
			)
			if !ok {
				t.Fatalf("default list plan uses no event order index:\n%s", formatExplainPlan(plan))
			}
			t.Logf("selected event order index: %s", selectedIndex)
		})
	}

	t.Run("summary aggregates", func(t *testing.T) {
		env.recorder.reset()
		summary, err := svc.GetSummary(context.Background(), SummaryRequest{
			ActorUserID: env.fixture.BobUserID,
			ActorRole:   string(user.RoleAdmin),
		})
		if err != nil {
			t.Fatalf("GetSummary: %v", err)
		}
		if summary.TotalEvents != largeEventFixtureSize {
			t.Fatalf("summary total = %d, want %d", summary.TotalEvents, largeEventFixtureSize)
		}

		summaryQueries := recordedSummaryQueries(env.recorder.snapshot())
		if len(summaryQueries) != 4 {
			t.Fatalf("captured summary queries = %d, want 4:\n%s", len(summaryQueries), formatRecordedQueries(env.recorder.snapshot()))
		}
		for _, query := range summaryQueries {
			assertSummarySQLShape(t, query)
			plan := explainRecordedQuery(t, env.db, query)
			aggregate, ok := findPlanNode(plan, func(node explainPlanNode) bool {
				return node.NodeType == "Aggregate"
			})
			if !ok {
				t.Fatalf("summary plan has no Aggregate node for %s:\n%s", query.SQL, formatExplainPlan(plan))
			}
			if aggregate.ActualRows > 3 {
				t.Fatalf("summary Aggregate Actual Rows = %.0f, want aggregate rows rather than %d events:\n%s", aggregate.ActualRows, largeEventFixtureSize, formatExplainPlan(plan))
			}
		}
	})
}

func withLargeEventFilter(base queryFilter, update func(*queryFilter)) queryFilter {
	update(&base)
	return base
}

func assertLargeEventFixtureShape(t *testing.T, fixture largeEventFixture) {
	t.Helper()

	if len(fixture.Records) != largeEventFixtureSize {
		t.Fatalf("fixture records = %d, want %d", len(fixture.Records), largeEventFixtureSize)
	}
	if len(fixture.RawPayload) != largeEventRawPayloadSize {
		t.Fatalf("raw payload length = %d, want %d", len(fixture.RawPayload), largeEventRawPayloadSize)
	}
	if !strings.HasPrefix(fixture.RawPayload, largeEventRawPayloadMarker) {
		t.Fatalf("raw payload lacks marker %q", largeEventRawPayloadMarker)
	}

	users := make(map[int]struct{}, 2)
	repos := make(map[int]struct{}, 2)
	tools := make(map[string]struct{}, 3)
	observedTimes := make(map[time.Time]struct{}, 64)
	sessions := make(map[string]struct{}, largeEventFixtureSize)
	events := make(map[string]struct{}, largeEventFixtureSize)
	dedupeKeys := make(map[string]struct{}, largeEventFixtureSize)
	sources := make(map[string]struct{}, largeEventFixtureSize)
	bound := 0
	for _, record := range fixture.Records {
		users[record.UserID] = struct{}{}
		repos[record.RepoID] = struct{}{}
		tools[record.Row.Tool] = struct{}{}
		observedTimes[record.Row.ObservedEndAt] = struct{}{}
		sessions[record.Row.ToolSessionID] = struct{}{}
		events[record.Row.ToolEventID] = struct{}{}
		dedupeKeys[record.Row.DedupeKey] = struct{}{}
		sources[record.Row.SourceBasename] = struct{}{}
		if record.Bound {
			bound++
		}
	}
	if len(users) != 2 || len(repos) != 2 || len(tools) != 3 {
		t.Fatalf("fixture dimensions users/repos/tools = %d/%d/%d, want 2/2/3", len(users), len(repos), len(tools))
	}
	if bound != largeEventFixtureSize/2 {
		t.Fatalf("bound events = %d, want %d", bound, largeEventFixtureSize/2)
	}
	if len(observedTimes) != 64 {
		t.Fatalf("distinct observed_end_at values = %d, want 64", len(observedTimes))
	}
	for name, count := range map[string]int{
		"tool sessions":    len(sessions),
		"tool events":      len(events),
		"dedupe keys":      len(dedupeKeys),
		"source basenames": len(sources),
	} {
		if count != largeEventFixtureSize {
			t.Fatalf("distinct %s = %d, want %d", name, count, largeEventFixtureSize)
		}
	}
}

func summaryRequestFromLargeEventFilter(filter queryFilter) SummaryRequest {
	return SummaryRequest{
		ActorUserID:   filter.ActorUserID,
		ActorRole:     filter.ActorRole,
		From:          filter.From,
		To:            filter.To,
		Tool:          filter.Tool,
		RepoID:        filter.RepoID,
		BindingStatus: filter.BindingStatus,
		UserID:        filter.UserID,
		Q:             filter.Q,
	}
}

func listRequestFromLargeEventFilter(filter queryFilter, limit, offset int) ListEventsRequest {
	return ListEventsRequest{
		ActorUserID:   filter.ActorUserID,
		ActorRole:     filter.ActorRole,
		From:          filter.From,
		To:            filter.To,
		Tool:          filter.Tool,
		RepoID:        filter.RepoID,
		BindingStatus: filter.BindingStatus,
		UserID:        filter.UserID,
		Q:             filter.Q,
		Limit:         limit,
		Offset:        offset,
	}
}

func matchingLargeEventRecords(records []largeEventRecord, matches func(largeEventRecord) bool) []largeEventRecord {
	matched := make([]largeEventRecord, 0, len(records))
	for _, record := range records {
		if matches(record) {
			matched = append(matched, record)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return largeEventRowBefore(matched[i].Row, matched[j].Row)
	})
	return matched
}

func largeEventRows(records []largeEventRecord) []EventListRow {
	rows := make([]EventListRow, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.Row)
	}
	return rows
}

func largeEventRowBefore(left, right EventListRow) bool {
	if left.ObservedEndAt.Equal(right.ObservedEndAt) {
		return left.ID > right.ID
	}
	return left.ObservedEndAt.After(right.ObservedEndAt)
}

func assertLargeEventSummary(t *testing.T, got *SummaryResponse, records []largeEventRecord) {
	t.Helper()

	toolCounts := make(map[string]int, 3)
	bound := 0
	for _, record := range records {
		toolCounts[record.Row.Tool]++
		if record.Bound {
			bound++
		}
	}
	tools := make([]string, 0, len(toolCounts))
	for tool := range toolCounts {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	wantCounts := make([]ToolCountDTO, 0, len(tools))
	for _, tool := range tools {
		wantCounts = append(wantCounts, ToolCountDTO{Tool: tool, Count: toolCounts[tool]})
	}
	want := &SummaryResponse{
		TotalEvents:   len(records),
		BoundEvents:   bound,
		UnboundEvents: len(records) - bound,
		ToolCounts:    wantCounts,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func requireRecordedListQuery(t *testing.T, queries []recordedQuery) recordedQuery {
	t.Helper()

	var matched []recordedQuery
	for _, query := range queries {
		upper := strings.ToUpper(query.SQL)
		if strings.Contains(upper, `FROM "TOOL_USAGE_EVENTS"`) &&
			strings.Contains(upper, " ORDER BY ") &&
			strings.Contains(upper, " LIMIT ") {
			matched = append(matched, query)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("captured bounded list queries = %d, want 1:\n%s", len(matched), formatRecordedQueries(queries))
	}
	return matched[0]
}

func recordedSummaryQueries(queries []recordedQuery) []recordedQuery {
	out := make([]recordedQuery, 0, 4)
	for _, query := range queries {
		upper := strings.ToUpper(query.SQL)
		if strings.Contains(upper, `FROM "TOOL_USAGE_EVENTS"`) &&
			(strings.Contains(upper, "COUNT(") || strings.Contains(upper, "GROUP BY")) {
			out = append(out, query)
		}
	}
	return out
}

func assertListSQLShape(t *testing.T, query recordedQuery) {
	t.Helper()

	upper := strings.ToUpper(query.SQL)
	if !strings.Contains(upper, `"OBSERVED_END_AT" DESC`) || !strings.Contains(upper, `"ID" DESC`) {
		t.Fatalf("list SQL lacks stable descending order: %s", query.SQL)
	}
	if !strings.Contains(upper, " LIMIT ") {
		t.Fatalf("list SQL lacks LIMIT: %s", query.SQL)
	}
	projection := sqlProjection(query.SQL)
	if strings.Contains(strings.ToLower(projection), `"raw_payload"`) {
		t.Fatalf("list SQL projects raw_payload: %s", query.SQL)
	}
}

func assertRawPayloadMarkerAbsent(t *testing.T, query recordedQuery) {
	t.Helper()

	if strings.Contains(query.SQL, largeEventRawPayloadMarker) {
		t.Fatalf("list SQL contains raw payload marker: %s", query.SQL)
	}
	for _, arg := range query.Args {
		if strings.Contains(fmt.Sprint(arg), largeEventRawPayloadMarker) {
			t.Fatalf("list SQL args contain raw payload marker: %v", query.Args)
		}
	}
}

func assertSummarySQLShape(t *testing.T, query recordedQuery) {
	t.Helper()

	upper := strings.ToUpper(query.SQL)
	if strings.Contains(upper, " LIMIT ") || strings.Contains(upper, " OFFSET ") {
		t.Fatalf("summary SQL is paginated: %s", query.SQL)
	}
	if !strings.Contains(upper, "COUNT(") && !strings.Contains(upper, "GROUP BY") {
		t.Fatalf("summary SQL is not aggregate-shaped: %s", query.SQL)
	}
	projection := strings.ToLower(sqlProjection(query.SQL))
	for _, eventField := range []string{
		"workspace_id",
		"repo_config_id",
		"user_id",
		"tool_session_id",
		"tool_event_id",
		"observed_start_at",
		"observed_end_at",
		"request_count",
		"usage_unit",
		"input_tokens",
		"output_tokens",
		"cached_input_tokens",
		"reasoning_tokens",
		"credit_usage",
		"context_usage_pct",
		"commit_checkpoint_id",
		"dedupe_key",
		"raw_source_path",
		"raw_source_locator",
		"raw_payload",
		"created_at",
	} {
		if strings.Contains(projection, `"`+eventField+`"`) {
			t.Fatalf("summary SQL returns event field %s: %s", eventField, query.SQL)
		}
	}
}

func sqlProjection(query string) string {
	upper := strings.ToUpper(query)
	index := strings.Index(upper, ` FROM "TOOL_USAGE_EVENTS"`)
	if index < 0 {
		return query
	}
	return query[:index]
}

type explainPlanNode struct {
	NodeType   string            `json:"Node Type"`
	ActualRows float64           `json:"Actual Rows"`
	IndexName  string            `json:"Index Name"`
	Plans      []explainPlanNode `json:"Plans"`
}

type explainDocument struct {
	Plan explainPlanNode `json:"Plan"`
}

func explainRecordedQuery(t *testing.T, db *sql.DB, query recordedQuery) explainPlanNode {
	t.Helper()

	var raw []byte
	explainSQL := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + query.SQL
	if err := db.QueryRowContext(context.Background(), explainSQL, query.Args...).Scan(&raw); err != nil {
		t.Fatalf("explain recorded query: %v\nSQL: %s\nargs: %v", err, query.SQL, query.Args)
	}
	var documents []explainDocument
	if err := json.Unmarshal(raw, &documents); err != nil {
		t.Fatalf("decode explain JSON: %v\n%s", err, raw)
	}
	if len(documents) != 1 {
		t.Fatalf("explain documents = %d, want 1\n%s", len(documents), raw)
	}
	return documents[0].Plan
}

func findPlanNode(root explainPlanNode, matches func(explainPlanNode) bool) (explainPlanNode, bool) {
	if matches(root) {
		return root, true
	}
	for _, child := range root.Plans {
		if node, ok := findPlanNode(child, matches); ok {
			return node, true
		}
	}
	return explainPlanNode{}, false
}

func planUsesOneOfIndexes(root explainPlanNode, names ...string) (string, bool) {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	node, ok := findPlanNode(root, func(node explainPlanNode) bool {
		_, allowed := allowed[node.IndexName]
		return allowed
	})
	return node.IndexName, ok
}

func formatExplainPlan(plan explainPlanNode) string {
	out, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", plan)
	}
	return string(out)
}

func formatRecordedQueries(queries []recordedQuery) string {
	lines := make([]string, 0, len(queries))
	for _, query := range queries {
		lines = append(lines, fmt.Sprintf("SQL: %s\nargs: %v", query.SQL, query.Args))
	}
	return strings.Join(lines, "\n")
}
