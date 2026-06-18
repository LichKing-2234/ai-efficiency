package attributionlocal

import (
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

// CodexFailedRequest is a single non-2xx Codex Responses API call recovered from
// the local ~/.codex/logs_2.sqlite log database. It carries the upstream request
// identifiers so a user can report them without needing any tooling of their own.
type CodexFailedRequest struct {
	LogID            int64
	Timestamp        time.Time
	StatusCode       int
	StatusText       string
	URL              string
	ThreadID         string
	XRequestID       string
	XClientRequestID string
	XKongRequestID   string
}

func (r CodexFailedRequest) HasRequestID() bool {
	return strings.TrimSpace(r.XRequestID) != "" ||
		strings.TrimSpace(r.XClientRequestID) != "" ||
		strings.TrimSpace(r.XKongRequestID) != ""
}

type CodexFailureSummary struct {
	Recent              []CodexFailedRequest
	RecentWithRequestID []CodexFailedRequest
}

// codexFailedRequestTarget mirrors the source the reference codex_request_ids.py
// script trusts: HTTP completions logged by the default client transport.
const codexFailedRequestTarget = "codex_client::default_client"

var (
	reFailLine             = regexp.MustCompile(`url=(\S+) status=(\d{3}) (.*?) headers=`)
	reFailHdrRequestID     = regexp.MustCompile(`"x-request-id":\s*"([^"]+)"`)
	reFailHdrClientReqID   = regexp.MustCompile(`"x-client-request-id":\s*"([^"]+)"`)
	reFailHdrKongRequestID = regexp.MustCompile(`"x-kong-request-id":\s*"([^"]+)"`)
)

// RecentCodexFailures returns up to limit of the most recent non-2xx Codex
// Responses requests found in the user's local Codex log database. It returns
// (nil, nil) when no Codex log database is present so callers can render a
// neutral "none" state rather than an error.
func RecentCodexFailures(homeDir string, limit int) ([]CodexFailedRequest, error) {
	summary, err := RecentCodexFailureSummary(homeDir, limit)
	return summary.Recent, err
}

// RecentCodexFailureSummary returns both the most recent failed Codex Responses
// requests and the most recent failed requests that carry upstream request IDs.
func RecentCodexFailureSummary(homeDir string, limit int) (CodexFailureSummary, error) {
	if limit <= 0 {
		return CodexFailureSummary{}, nil
	}
	dbPaths := findCodexSQLiteFiles(homeDir)
	if len(dbPaths) == 0 {
		return CodexFailureSummary{}, nil
	}
	return parseCodexFailureSummary(dbPaths[0], limit)
}

func parseCodexFailures(dbPath string, limit int) ([]CodexFailedRequest, error) {
	summary, err := parseCodexFailureSummary(dbPath, limit)
	return summary.Recent, err
}

func parseCodexFailureSummary(dbPath string, limit int) (CodexFailureSummary, error) {
	var summary CodexFailureSummary
	if limit <= 0 {
		return summary, nil
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return summary, err
	}
	defer db.Close()

	// Walk newest-first using the ts index. The LIKE clauses narrow the scan to
	// HTTP completion lines whose status is not 2xx (3xx/4xx/5xx) before we parse
	// in Go; we stop once `limit` genuine failures are collected.
	rows, err := db.Query(`
		SELECT id, ts, ts_nanos, thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND feedback_log_body LIKE '%Request completed method=POST%'
		  AND feedback_log_body LIKE '%api.path="responses"%'
		  AND (
		        feedback_log_body LIKE '%status=3%'
		     OR feedback_log_body LIKE '%status=4%'
		     OR feedback_log_body LIKE '%status=5%'
		  )
		ORDER BY ts DESC, ts_nanos DESC, id DESC
	`, codexFailedRequestTarget)
	if err != nil {
		return summary, err
	}
	defer rows.Close()

	summary.Recent = make([]CodexFailedRequest, 0, limit)
	summary.RecentWithRequestID = make([]CodexFailedRequest, 0, limit)
	for rows.Next() {
		var (
			id      int64
			ts      int64
			tsNanos int64
			thread  sql.NullString
			body    string
		)
		if err := rows.Scan(&id, &ts, &tsNanos, &thread, &body); err != nil {
			return summary, err
		}
		failure, ok := parseCodexFailureLine(id, ts, tsNanos, thread.String, body)
		if !ok {
			continue
		}
		if len(summary.Recent) < limit {
			summary.Recent = append(summary.Recent, failure)
		}
		if failure.HasRequestID() && len(summary.RecentWithRequestID) < limit {
			summary.RecentWithRequestID = append(summary.RecentWithRequestID, failure)
		}
		if len(summary.Recent) >= limit && len(summary.RecentWithRequestID) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func parseCodexFailureLine(id, ts, tsNanos int64, threadID, body string) (CodexFailedRequest, bool) {
	m := reFailLine.FindStringSubmatch(body)
	if len(m) != 4 {
		return CodexFailedRequest{}, false
	}
	code, err := strconv.Atoi(m[2])
	if err != nil {
		return CodexFailedRequest{}, false
	}
	if code >= 200 && code < 300 {
		return CodexFailedRequest{}, false
	}
	failure := CodexFailedRequest{
		LogID:            id,
		Timestamp:        time.Unix(ts, tsNanos).UTC(),
		StatusCode:       code,
		StatusText:       strings.TrimSpace(m[3]),
		URL:              strings.TrimSpace(m[1]),
		ThreadID:         strings.TrimSpace(threadID),
		XRequestID:       firstSubmatch(reFailHdrRequestID, body),
		XClientRequestID: firstSubmatch(reFailHdrClientReqID, body),
		XKongRequestID:   firstSubmatch(reFailHdrKongRequestID, body),
	}
	return failure, true
}

func firstSubmatch(re *regexp.Regexp, body string) string {
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
