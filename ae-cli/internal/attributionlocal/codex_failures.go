package attributionlocal

import (
	"context"
	"database/sql"
	"net/url"
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

// codexFailedRequestTarget is the log target Codex writes HTTP completions to.
//
// It used to name codex_client::default_client, the target the reference
// codex_request_ids.py script trusted. Codex no longer writes there: measured on
// one machine that target held 0 of 23,732 rows while codex_http_client::client
// held 1,579, so doctor reported "no failed Codex requests in local logs" from a
// query that could not have found one. This is the same target the attribution
// reader already trusts for Request evidence.
const codexFailedRequestTarget = codexResponsesHTTPClientTarget

const codexFailureLookback = 30 * 24 * time.Hour
const codexFailureQueryTimeout = 3 * time.Second

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
	ctx, cancel := context.WithTimeout(context.Background(), codexFailureQueryTimeout)
	defer cancel()
	since := time.Now().UTC().Add(-codexFailureLookback)
	return parseCodexFailureSummary(ctx, dbPaths[0], limit, since)
}

func parseCodexFailures(dbPath string, limit int) ([]CodexFailedRequest, error) {
	summary, err := parseCodexFailureSummary(context.Background(), dbPath, limit, time.Time{})
	return summary.Recent, err
}

func parseCodexFailureSummary(ctx context.Context, dbPath string, limit int, since time.Time) (CodexFailureSummary, error) {
	var summary CodexFailureSummary
	if limit <= 0 {
		return summary, nil
	}
	db, err := sql.Open("sqlite", codexSQLiteReadOnlyDSN(dbPath))
	if err != nil {
		return summary, err
	}
	defer db.Close()

	summary.Recent, err = queryCodexFailures(ctx, db, limit, since, false)
	if err != nil {
		return summary, err
	}
	summary.RecentWithRequestID, err = queryCodexFailures(ctx, db, limit, since, true)
	if err != nil {
		return summary, err
	}
	return summary, nil
}

func queryCodexFailures(ctx context.Context, db *sql.DB, limit int, since time.Time, requireRequestID bool) ([]CodexFailedRequest, error) {
	if limit <= 0 {
		return nil, nil
	}
	sinceUnix := int64(0)
	if !since.IsZero() {
		sinceUnix = since.UTC().Unix()
	}
	requestIDFilter := ""
	if requireRequestID {
		requestIDFilter = `
		  AND (
		        feedback_log_body LIKE '%"x-request-id"%'
		     OR feedback_log_body LIKE '%"x-client-request-id"%'
		     OR feedback_log_body LIKE '%"x-kong-request-id"%'
		  )`
	}
	// The timestamp lower bound, descending timestamp order, and SQL LIMIT keep
	// doctor from walking an unbounded Codex transport log on every run.
	query := `
		SELECT id, ts, ts_nanos, thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND feedback_log_body LIKE '%Request completed method=POST%'
		  AND feedback_log_body LIKE '%api.path="responses"%'
		  AND (
		        feedback_log_body LIKE '%status=3%'
		     OR feedback_log_body LIKE '%status=4%'
		     OR feedback_log_body LIKE '%status=5%'
		  )
` + requestIDFilter + `
		ORDER BY ts DESC, ts_nanos DESC, id DESC
		LIMIT ?
	`
	rows, err := db.QueryContext(ctx, query, codexFailedRequestTarget, sinceUnix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	failures := make([]CodexFailedRequest, 0, limit)
	for rows.Next() {
		var (
			id      int64
			ts      int64
			tsNanos int64
			thread  sql.NullString
			body    string
		)
		if err := rows.Scan(&id, &ts, &tsNanos, &thread, &body); err != nil {
			return nil, err
		}
		failure, ok := parseCodexFailureLine(id, ts, tsNanos, thread.String, body)
		if !ok {
			continue
		}
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return failures, nil
}

func codexSQLiteReadOnlyDSN(dbPath string) string {
	u := url.URL{
		Scheme:   "file",
		Path:     dbPath,
		RawQuery: "mode=ro",
	}
	return u.String()
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
