package attributionlocal

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	_ "github.com/glebarez/go-sqlite"
)

func TestWriteFile_WritesFixtureContent(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "fixture.txt", "hello")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data = %q, want %q", string(data), "hello")
	}
}

func TestBuildCodexSQLiteFixture_CreatesLogsDB(t *testing.T) {
	t.Parallel()

	path := buildCodexSQLiteFixture(t, []string{`event.name="codex.sse_event" event.kind=response.completed conversation.id=conv-1 response.id=resp-1`})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

func buildCodexSQLiteFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, line := range lines {
		if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, line); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	return path
}

type attributionFixture struct {
	WorkspaceRoot string
	HomeDir       string
}

func buildAttributionFixture(t *testing.T) attributionFixture {
	t.Helper()

	root := fixtureRepoRoot(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexSessions := filepath.Join(home, ".codex", "sessions", "2026", "05", "13")
	if err := os.MkdirAll(codexSessions, 0o700); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	codexJSONL := filepath.Join(codexSessions, "sess-1.jsonl")
	codexBody := `{"type":"session_meta","payload":{"id":"sess-1","cwd":"` + root + `"}}
{"type":"event_msg","payload":{"type":"token_count","response_id":"resp-1","info":{"last_token_usage":{"input_tokens":12,"cached_input_tokens":4,"output_tokens":5,"reasoning_output_tokens":2,"total_tokens":23}}}}`
	if err := os.WriteFile(codexJSONL, []byte(codexBody), 0o600); err != nil {
		t.Fatalf("write codex jsonl: %v", err)
	}

	return attributionFixture{
		WorkspaceRoot: root,
		HomeDir:       home,
	}
}

func buildSQLiteOnlyAttributionFixture(t *testing.T) attributionFixture {
	t.Helper()

	root := fixtureRepoRoot(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	codexSessions := filepath.Join(home, ".codex", "sessions", "2026", "05", "13")
	if err := os.MkdirAll(codexSessions, 0o700); err != nil {
		t.Fatalf("mkdir codex sessions: %v", err)
	}
	codexJSONL := filepath.Join(codexSessions, "sess-1.jsonl")
	codexBody := `{"type":"session_meta","payload":{"id":"conv-1","cwd":"` + root + `"}}`
	if err := os.WriteFile(codexJSONL, []byte(codexBody), 0o600); err != nil {
		t.Fatalf("write codex session metadata: %v", err)
	}
	dbPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create logs table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, `event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-05-13T10:00:00Z conversation.id=conv-1 response.id=resp-1`); err != nil {
		t.Fatalf("insert logs row: %v", err)
	}

	return attributionFixture{
		WorkspaceRoot: root,
		HomeDir:       home,
	}
}

func buildKiroCLISQLiteFixture(t *testing.T, workspaceRoot string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "Library", "Application Support", "kiro-cli")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir kiro-cli dir: %v", err)
	}
	path := filepath.Join(base, "data.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations_v2 (key TEXT NOT NULL, conversation_id TEXT NOT NULL, value TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (key, conversation_id))`); err != nil {
		t.Fatalf("create conversations_v2: %v", err)
	}
	value := fmt.Sprintf(`{"conversation_id":"conv-1","history":[{"user":{"timestamp":"2026-05-20T21:55:09.029671+08:00","content":{"Prompt":{"prompt":"test"}}},"assistant":{"Response":{"message_id":"msg-1","content":"reply"}},"request_metadata":{"request_id":"req-1","context_usage_percentage":3.2832,"message_id":"msg-1","request_start_timestamp_ms":1779285309036,"stream_end_timestamp_ms":1779285314178}}],"model_info":{"model_id":"auto","rate_unit":"Credit"},"user_turn_metadata":{"requests":[],"usage_info":[{"value":0.10903188126036485,"unit":"credit","unit_plural":"credits"}]}}`)
	if _, err := db.Exec(`INSERT INTO conversations_v2 (key, conversation_id, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, workspaceRoot, "conv-1", value, 1779285314178, 1779285314178); err != nil {
		t.Fatalf("insert conversations_v2: %v", err)
	}
	return path
}

func buildKiroCLISQLiteAttributionFixture(t *testing.T) attributionFixture {
	t.Helper()

	root := fixtureRepoRoot(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Library", "Application Support", "kiro-cli"), 0o700); err != nil {
		t.Fatalf("mkdir kiro-cli home: %v", err)
	}
	src := buildKiroCLISQLiteFixture(t, root)
	dst := filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read kiro-cli fixture: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write kiro-cli fixture: %v", err)
	}

	return attributionFixture{
		WorkspaceRoot: root,
		HomeDir:       home,
	}
}

func buildKiroIDEExecutionFixture(t *testing.T, workspaceRoot, sessionID string) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "kiro-ide", "8794d1d6b05461c486ae3c70a25dbd02", "414d1636299d2b9e4ce7e17fb11f63e9")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir kiro ide root: %v", err)
	}
	path := filepath.Join(root, "71d22ce2a62c4cdc077c824e07bd8650")
	body := fmt.Sprintf(`{
  "executionId": "exec-1",
  "workflowType": "chat-agent",
  "status": "succeed",
  "startTime": 1779288013500,
  "chatSessionId": %q,
  "endTime": 1779288019211,
  "usageSummary": [
    {"usage": 0.007750866932006633, "unit": "credit", "unitPlural": "credits"},
    {"usage": 0.05991760597014926, "unit": "credit", "unitPlural": "credits"}
  ],
  "contextUsagePercentage": 23.259000778198242,
  "workspaceContext": {"cwd": %q}
}`, sessionID, workspaceRoot)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write kiro ide execution: %v", err)
	}
	return path
}

func buildKiroIDEAttributionFixture(t *testing.T) attributionFixture {
	t.Helper()

	root := fixtureRepoRoot(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(root))
	sessionDir := filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("mkdir kiro ide session dir: %v", err)
	}
	sessionIndexPath := filepath.Join(sessionDir, "sessions.json")
	sessionIndex := fmt.Sprintf(`[{"sessionId":"chat-sess-1","title":"hi","dateCreated":"1779284885045","workspaceDirectory":%q}]`, root)
	if err := os.WriteFile(sessionIndexPath, []byte(sessionIndex), 0o600); err != nil {
		t.Fatalf("write kiro ide session index: %v", err)
	}

	execPath := filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "8794d1d6b05461c486ae3c70a25dbd02", "414d1636299d2b9e4ce7e17fb11f63e9", "71d22ce2a62c4cdc077c824e07bd8650")
	if err := os.MkdirAll(filepath.Dir(execPath), 0o700); err != nil {
		t.Fatalf("mkdir kiro ide exec dir: %v", err)
	}
	body := fmt.Sprintf(`{
  "executionId": "exec-1",
  "workflowType": "chat-agent",
  "status": "succeed",
  "startTime": 1779288013500,
  "chatSessionId": "chat-sess-1",
  "endTime": 1779288019211,
  "usageSummary": [
    {"usage": 0.007750866932006633, "unit": "credit", "unitPlural": "credits"},
    {"usage": 0.05991760597014926, "unit": "credit", "unitPlural": "credits"}
  ],
  "contextUsagePercentage": 23.259000778198242,
  "workspaceContext": {"cwd": %q}
}`, root)
	if err := os.WriteFile(execPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write kiro ide execution: %v", err)
	}

	return attributionFixture{
		WorkspaceRoot: root,
		HomeDir:       home,
	}
}

type syncEngineFixture struct {
	Engine *SyncEngine
	Client *syncBackendClientStub
}

func setupSyncEngineWithSpool(t *testing.T) syncEngineFixture {
	t.Helper()

	clientStub := &syncBackendClientStub{}
	engine := &SyncEngine{
		Client: clientStub,
	}
	spoolPath := filepath.Join(t.TempDir(), "spool.json")
	payload := []LocalToolUsageEvent{{
		Tool:            "codex",
		WorkspaceID:     "ws-1",
		ToolSessionID:   "conv-1",
		ToolEventID:     "resp-1",
		DedupeKey:       "spooled-dedupe-key",
		UsageUnit:       UsageUnitToken,
		RequestCount:    1,
		ObservedStartAt: jsonTime("2026-05-13T10:00:00Z"),
		ObservedEndAt:   jsonTime("2026-05-13T10:00:01Z"),
	}}
	if err := SaveJSON(spoolPath, payload); err != nil {
		t.Fatalf("SaveJSON(spool): %v", err)
	}
	engine.spoolPath = spoolPath
	return syncEngineFixture{Engine: engine, Client: clientStub}
}

type syncBackendClientStub struct {
	uploads  []string
	requests []client.ToolUsageEventRequest
	failOn   string
}

func (s *syncBackendClientStub) SendToolUsageEvent(_ context.Context, req client.ToolUsageEventRequest) error {
	if s.failOn != "" && req.DedupeKey == s.failOn {
		return fmt.Errorf("upload failed for %s", req.DedupeKey)
	}
	s.uploads = append(s.uploads, req.DedupeKey)
	s.requests = append(s.requests, req)
	return nil
}

func (s *syncBackendClientStub) SawUpload(dedupeKey string) bool {
	for _, item := range s.uploads {
		if item == dedupeKey {
			return true
		}
	}
	return false
}

func fixtureRepoRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return root
}

func jsonTime(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, raw)
	return parsed.UTC()
}
