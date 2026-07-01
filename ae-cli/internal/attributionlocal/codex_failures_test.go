package attributionlocal

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

type codexFailureRow struct {
	ts       int64
	tsNanos  int64
	target   string
	threadID string
	body     string
}

func buildCodexFailuresFixture(t *testing.T, rows []codexFailureRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs_2.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL,
		ts_nanos INTEGER NOT NULL,
		target TEXT NOT NULL,
		thread_id TEXT,
		feedback_log_body TEXT
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO logs (ts, ts_nanos, target, thread_id, feedback_log_body) VALUES (?, ?, ?, ?, ?)`,
			r.ts, r.tsNanos, r.target, r.threadID, r.body,
		); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	return path
}

const (
	codexFailOK  = `model_client.stream_responses_api{model=gpt-5.4 wire_api=responses http.method="POST" api.path="responses"}: Request completed method=POST url=https://relay.example.com/responses status=200 OK headers={"server": "nginx", "x-request-id": "ok-req", "x-client-request-id": "ok-client", "x-kong-request-id": "ok-kong"} version=HTTP/1.1`
	codexFail503 = `model_client.stream_responses_api{model=gpt-5.4 wire_api=responses http.method="POST" api.path="responses"}: Request completed method=POST url=https://relay.example.com/responses status=503 Service Unavailable headers={"server": "nginx", "x-request-id": "req-503", "x-client-request-id": "client-503", "x-kong-request-id": "kong-503"} version=HTTP/1.1`
	codexFail502 = `model_client.stream_responses_api{model=gpt-5.4 wire_api=responses http.method="POST" api.path="responses"}: Request completed method=POST url=https://relay.example.com/responses status=502 Bad Gateway headers={"server": "nginx", "date": "Fri, 12 Jun 2026 04:04:11 GMT", "content-type": "text/html"} version=HTTP/1.1`
	// codexScriptTrap embeds the reference Python script source as request body
	// content. It mentions "x-request-id" and "status=" but is NOT a completion
	// line on the trusted target, so it must never be parsed as a failure.
	codexScriptTrap = `websocket event: {"type":"response.created","response":{"id":"resp_trap","input":[{"text":"feedback_log_body LIKE '%Request completed method=POST%' status='unknown' x-request-id \"OpenAI\""}]}}`
)

func TestRecentCodexFailures_FiltersToNon2xxCompletions(t *testing.T) {
	dbPath := buildCodexFailuresFixture(t, []codexFailureRow{
		{ts: 100, target: codexFailedRequestTarget, threadID: "thread-a", body: codexFailOK},
		{ts: 200, target: codexFailedRequestTarget, threadID: "thread-b", body: codexFail503},
		{ts: 300, target: "codex_api::sse::responses", threadID: "thread-c", body: codexScriptTrap},
		{ts: 400, target: codexFailedRequestTarget, threadID: "thread-d", body: codexFail502},
	})

	failures, err := parseCodexFailures(dbPath, 10)
	if err != nil {
		t.Fatalf("parseCodexFailures: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("failure count = %d, want 2 (only non-2xx completions)", len(failures))
	}

	// Newest first: the 502 (ts=400) precedes the 503 (ts=200).
	if failures[0].StatusCode != 502 || failures[0].ThreadID != "thread-d" {
		t.Fatalf("failures[0] = %+v, want 502/thread-d", failures[0])
	}
	if failures[0].StatusText != "Bad Gateway" {
		t.Fatalf("failures[0].StatusText = %q, want %q", failures[0].StatusText, "Bad Gateway")
	}
	// nginx-only 502 has no upstream IDs.
	if failures[0].XRequestID != "" || failures[0].XKongRequestID != "" {
		t.Fatalf("failures[0] should have empty upstream IDs, got %+v", failures[0])
	}

	if failures[1].StatusCode != 503 {
		t.Fatalf("failures[1].StatusCode = %d, want 503", failures[1].StatusCode)
	}
	if failures[1].XRequestID != "req-503" ||
		failures[1].XClientRequestID != "client-503" ||
		failures[1].XKongRequestID != "kong-503" {
		t.Fatalf("failures[1] request IDs = %+v, want req-503/client-503/kong-503", failures[1])
	}
	if failures[1].URL != "https://relay.example.com/responses" {
		t.Fatalf("failures[1].URL = %q", failures[1].URL)
	}
}

func TestRecentCodexFailureSummary_SeparatesRecentWithRequestIDs(t *testing.T) {
	dbPath := buildCodexFailuresFixture(t, []codexFailureRow{
		{ts: 100, target: codexFailedRequestTarget, threadID: "with-id", body: codexFail503},
		{ts: 200, target: codexFailedRequestTarget, threadID: "without-id", body: codexFail502},
	})

	summary, err := parseCodexFailureSummary(context.Background(), dbPath, 1, time.Time{})
	if err != nil {
		t.Fatalf("parseCodexFailureSummary: %v", err)
	}
	if len(summary.Recent) != 1 || summary.Recent[0].ThreadID != "without-id" {
		t.Fatalf("Recent = %+v, want newest failure without-id", summary.Recent)
	}
	if len(summary.RecentWithRequestID) != 1 || summary.RecentWithRequestID[0].ThreadID != "with-id" {
		t.Fatalf("RecentWithRequestID = %+v, want newest failure with request id", summary.RecentWithRequestID)
	}
	if !summary.RecentWithRequestID[0].HasRequestID() {
		t.Fatalf("RecentWithRequestID[0] should carry a request id: %+v", summary.RecentWithRequestID[0])
	}
}

func TestRecentCodexFailures_RespectsLimit(t *testing.T) {
	dbPath := buildCodexFailuresFixture(t, []codexFailureRow{
		{ts: 100, target: codexFailedRequestTarget, threadID: "a", body: codexFail503},
		{ts: 200, target: codexFailedRequestTarget, threadID: "b", body: codexFail503},
		{ts: 300, target: codexFailedRequestTarget, threadID: "c", body: codexFail503},
	})

	failures, err := parseCodexFailures(dbPath, 2)
	if err != nil {
		t.Fatalf("parseCodexFailures: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("failure count = %d, want 2 (limit)", len(failures))
	}
	// Newest first by ts.
	if failures[0].ThreadID != "c" || failures[1].ThreadID != "b" {
		t.Fatalf("ordering = %s,%s, want c,b", failures[0].ThreadID, failures[1].ThreadID)
	}
}

func TestRecentCodexFailures_ZeroLimitReturnsNil(t *testing.T) {
	failures, err := RecentCodexFailures(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("RecentCodexFailures: %v", err)
	}
	if failures != nil {
		t.Fatalf("failures = %+v, want nil for zero limit", failures)
	}
}

func TestRecentCodexFailures_MissingDBReturnsNil(t *testing.T) {
	failures, err := RecentCodexFailures(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("RecentCodexFailures: %v", err)
	}
	if failures != nil {
		t.Fatalf("failures = %+v, want nil when no Codex log DB present", failures)
	}
}
