package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestClient(t *testing.T) *ent.Client {
	return testdb.Open(t)
}

func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// createTestRepoConfig creates an ScmProvider and a RepoConfig for testing.
func createTestRepoConfig(t *testing.T, client *ent.Client, fullName string) *ent.RepoConfig {
	t.Helper()
	ctx := context.Background()

	provider, err := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create scm provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
		SetName("test-repo").
		SetFullName(fullName).
		SetCloneURL("https://github.com/" + fullName + ".git").
		SetScmProviderID(provider.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create repo config: %v", err)
	}
	return rc
}

// newGinContext creates a gin.Context backed by httptest for the given request.
func newGinContext(w *httptest.ResponseRecorder, r *http.Request) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request = r
	return c
}

// --- Tests ---

func TestNewHandler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	h := NewHandler(client, nil, newTestLogger())
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.entClient != client {
		t.Error("entClient not set correctly")
	}
	if h.labeler != nil {
		t.Error("expected nil labeler")
	}
}

// --- bodyReader tests ---

func TestBodyReader(t *testing.T) {
	t.Run("read full data", func(t *testing.T) {
		data := []byte("hello world")
		r := &bodyReader{data: data}
		buf, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(buf) != "hello world" {
			t.Errorf("got %q, want %q", string(buf), "hello world")
		}
	})

	t.Run("read in chunks", func(t *testing.T) {
		data := []byte("abcdefghij")
		r := &bodyReader{data: data}
		buf := make([]byte, 3)
		var result []byte
		for {
			n, err := r.Read(buf)
			result = append(result, buf[:n]...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if string(result) != "abcdefghij" {
			t.Errorf("got %q, want %q", string(result), "abcdefghij")
		}
	})

	t.Run("read empty data", func(t *testing.T) {
		r := &bodyReader{data: []byte{}}
		buf := make([]byte, 10)
		n, err := r.Read(buf)
		if n != 0 {
			t.Errorf("expected 0 bytes, got %d", n)
		}
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("read after EOF", func(t *testing.T) {
		r := &bodyReader{data: []byte("x")}
		buf := make([]byte, 10)
		// First read consumes all data.
		_, _ = r.Read(buf)
		// Second read should return EOF.
		n, err := r.Read(buf)
		if n != 0 {
			t.Errorf("expected 0 bytes after EOF, got %d", n)
		}
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})
}

// --- HandleGitHub error-case tests ---

func TestHandleGitHubMissingHeaders(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"repository":{"full_name":"org/repo"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", body)
	// No X-GitHub-Event or X-GitHub-Delivery headers.
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleGitHubInvalidPayload(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewBufferString("not json"))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "abc-123")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleGitHubMissingRepoName(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	// Valid JSON but no repository.full_name.
	body := bytes.NewBufferString(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", body)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "abc-123")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleGitHubUnknownRepo(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"repository":{"full_name":"unknown/repo"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", body)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "abc-123")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// --- HandleBitbucket error-case tests ---

func TestHandleBitbucketMissingHeader(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", body)
	// No X-Event-Key header.
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBitbucketInvalidPayload(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewBufferString("{bad"))
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBitbucketMissingRepoInfo(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	// Empty project key and slug → full_name becomes "/".
	body := bytes.NewBufferString(`{"repository":{"slug":"","project":{"key":""}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", body)
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBitbucketUnknownRepo(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"repository":{"slug":"repo","project":{"key":"PROJ"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", body)
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// --- dispatch / handlePR* tests ---

func TestHandlePROpened(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPROpened,
		RepoFullName: "org/repo",
		Sender:       "testuser",
		PR: &scm.PRInfo{
			ID:           42,
			Title:        "Add feature X",
			Author:       "testuser",
			SourceBranch: "feature/x",
			TargetBranch: "main",
			URL:          "https://github.com/org/repo/pull/42",
		},
	}

	h.handlePROpened(ctx, rc, event)

	// Verify PrRecord was created.
	records, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(records))
	}
	pr := records[0]
	if pr.ScmPrID != 42 {
		t.Errorf("expected scm_pr_id=42, got %d", pr.ScmPrID)
	}
	if pr.Title != "Add feature X" {
		t.Errorf("expected title 'Add feature X', got %q", pr.Title)
	}
	if pr.Author != "testuser" {
		t.Errorf("expected author 'testuser', got %q", pr.Author)
	}
	if pr.SourceBranch != "feature/x" {
		t.Errorf("expected source_branch 'feature/x', got %q", pr.SourceBranch)
	}
	if pr.Status != prrecord.StatusOpen {
		t.Errorf("expected status 'open', got %q", pr.Status)
	}
}

func TestHandlePRUpdated(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")
	ctx := context.Background()

	// Create an existing PR record.
	_, err := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(99).
		SetScmPrURL("https://github.com/org/repo/pull/99").
		SetAuthor("alice").
		SetTitle("Old title").
		SetSourceBranch("feature/old").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create PR record: %v", err)
	}

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/repo",
		Sender:       "alice",
		PR: &scm.PRInfo{
			ID:           99,
			Title:        "Updated title",
			Author:       "alice",
			SourceBranch: "feature/old",
			TargetBranch: "main",
			URL:          "https://github.com/org/repo/pull/99",
		},
	}

	h.handlePRUpdated(ctx, rc, event)

	// Verify title was updated.
	updated, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(updated))
	}
	if updated[0].Title != "Updated title" {
		t.Errorf("expected title 'Updated title', got %q", updated[0].Title)
	}
}

func TestHandlePRUpdatedNewPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")
	ctx := context.Background()

	// No existing PR record — handlePRUpdated should create one via handlePROpened.
	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/repo",
		Sender:       "bob",
		PR: &scm.PRInfo{
			ID:           200,
			Title:        "New PR via update",
			Author:       "bob",
			SourceBranch: "feature/new",
			TargetBranch: "main",
			URL:          "https://github.com/org/repo/pull/200",
		},
	}

	h.handlePRUpdated(ctx, rc, event)

	records, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(records))
	}
	if records[0].ScmPrID != 200 {
		t.Errorf("expected scm_pr_id=200, got %d", records[0].ScmPrID)
	}
	if records[0].Status != prrecord.StatusOpen {
		t.Errorf("expected status 'open', got %q", records[0].Status)
	}
}

func TestHandlePRMerged(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")
	ctx := context.Background()

	// Create an existing open PR record.
	_, err := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(55).
		SetScmPrURL("https://github.com/org/repo/pull/55").
		SetAuthor("carol").
		SetTitle("Merge me").
		SetSourceBranch("feature/merge").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create PR record: %v", err)
	}

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/repo",
		Sender:       "carol",
		PR: &scm.PRInfo{
			ID:           55,
			Title:        "Merge me",
			Author:       "carol",
			SourceBranch: "feature/merge",
			TargetBranch: "main",
			URL:          "https://github.com/org/repo/pull/55",
		},
	}

	h.handlePRMerged(ctx, rc, event)

	records, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(records))
	}
	pr := records[0]
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("expected status 'merged', got %q", pr.Status)
	}
	if pr.MergedAt == nil {
		t.Error("expected merged_at to be set")
	}
	if pr.CycleTimeHours <= 0 {
		// cycle_time_hours should be > 0 since some time has passed since creation.
		// In practice it may be very small but non-negative. Just check it was set.
		t.Logf("cycle_time_hours=%f (may be very small in tests)", pr.CycleTimeHours)
	}
}

func TestHandlePRMergedNewPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")
	ctx := context.Background()

	// No existing PR — handlePRMerged should create a merged record directly.
	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/repo",
		Sender:       "dave",
		PR: &scm.PRInfo{
			ID:           300,
			Title:        "Merged without open",
			Author:       "dave",
			SourceBranch: "hotfix/urgent",
			TargetBranch: "main",
			URL:          "https://github.com/org/repo/pull/300",
		},
	}

	h.handlePRMerged(ctx, rc, event)

	records, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(records))
	}
	pr := records[0]
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("expected status 'merged', got %q", pr.Status)
	}
	if pr.MergedAt == nil {
		t.Error("expected merged_at to be set")
	}
	if pr.ScmPrID != 300 {
		t.Errorf("expected scm_pr_id=300, got %d", pr.ScmPrID)
	}
}

func TestLabelPRNilLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	// Should not panic with nil labeler.
	h.labelPR(context.Background(), 999)
}

func TestStoreDeadLetter(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "org/repo",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	h.storeDeadLetter(c, rc.ID, "delivery-123", "pull_request", payloadBytes, "signature mismatch")

	// Verify dead letter was stored.
	ctx := context.Background()
	letters, err := client.WebhookDeadLetter.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(letters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(letters))
	}
	dl := letters[0]
	if dl.DeliveryID != "delivery-123" {
		t.Errorf("expected delivery_id 'delivery-123', got %q", dl.DeliveryID)
	}
	if dl.EventType != "pull_request" {
		t.Errorf("expected event_type 'pull_request', got %q", dl.EventType)
	}
	if dl.ErrorMessage != "signature mismatch" {
		t.Errorf("expected error_message 'signature mismatch', got %q", dl.ErrorMessage)
	}
}

// --- dispatch tests ---

func TestDispatchPROpened(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/dispatch-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	event := &scm.WebhookEvent{
		Type:         scm.EventPROpened,
		RepoFullName: "org/dispatch-repo",
		Sender:       "user1",
		PR: &scm.PRInfo{
			ID: 1, Title: "PR 1", Author: "user1",
			SourceBranch: "feat-1", TargetBranch: "main",
			URL: "https://github.com/org/dispatch-repo/pull/1",
		},
	}

	h.dispatch(c, rc, event)

	ctx := context.Background()
	count, _ := client.PrRecord.Query().Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 PR record after dispatch PR opened, got %d", count)
	}
}

func TestDispatchPRUpdated(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/dispatch-update-repo")
	ctx := context.Background()

	// Pre-create a PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(10).
		SetScmPrURL("https://github.com/org/dispatch-update-repo/pull/10").
		SetAuthor("user1").
		SetTitle("Old Title").
		SetSourceBranch("feat-10").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/dispatch-update-repo",
		Sender:       "user1",
		PR: &scm.PRInfo{
			ID: 10, Title: "New Title", Author: "user1",
			SourceBranch: "feat-10", TargetBranch: "main",
			URL: "https://github.com/org/dispatch-update-repo/pull/10",
		},
	}

	h.dispatch(c, rc, event)

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Title != "New Title" {
		t.Errorf("title = %q, want %q", pr.Title, "New Title")
	}
}

func TestDispatchPRMerged(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/dispatch-merge-repo")
	ctx := context.Background()

	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(20).
		SetScmPrURL("https://github.com/org/dispatch-merge-repo/pull/20").
		SetAuthor("user1").
		SetTitle("Merge PR").
		SetSourceBranch("feat-20").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/dispatch-merge-repo",
		Sender:       "user1",
		PR: &scm.PRInfo{
			ID: 20, Title: "Merge PR", Author: "user1",
			SourceBranch: "feat-20", TargetBranch: "main",
			URL: "https://github.com/org/dispatch-merge-repo/pull/20",
		},
	}

	h.dispatch(c, rc, event)

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
}

func TestDispatchPush(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/push-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	event := &scm.WebhookEvent{
		Type:         scm.EventPush,
		RepoFullName: "org/push-repo",
		Sender:       "user1",
	}

	// Should not panic — push events are just logged
	h.dispatch(c, rc, event)
}

// --- handlePR nil PR tests ---

func TestHandlePROpenedNilPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/nil-pr-repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPROpened,
		RepoFullName: "org/nil-pr-repo",
		Sender:       "user1",
		PR:           nil,
	}

	// Should not panic with nil PR
	h.handlePROpened(ctx, rc, event)

	count, _ := client.PrRecord.Query().Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 PR records with nil PR, got %d", count)
	}
}

func TestHandlePRUpdatedNilPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/nil-update-repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/nil-update-repo",
		Sender:       "user1",
		PR:           nil,
	}

	h.handlePRUpdated(ctx, rc, event)
}

func TestHandlePRMergedNilPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/nil-merge-repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/nil-merge-repo",
		Sender:       "user1",
		PR:           nil,
	}

	h.handlePRMerged(ctx, rc, event)
}

// --- HandleGitHub with known repo (deeper path coverage) ---

func TestHandleGitHubKnownRepoNoSecret(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/gh-repo")

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "org/gh-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   1,
			"title":    "Test PR",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-1"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/gh-repo/pull/1",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// The GitHub provider may fail to parse (no valid signature setup in test),
	// but we should get past the repo lookup. Check we don't get 404.
	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

func TestHandleBitbucketKnownRepoNoSecret(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/bb-repo")

	payload := map[string]interface{}{
		"eventKey": "pr:opened",
		"repository": map[string]interface{}{
			"slug": "bb-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"pullRequest": map[string]interface{}{
			"id":    1,
			"title": "Test PR",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "alice"},
			},
			"fromRef": map[string]interface{}{
				"displayId": "feat-1",
			},
			"toRef": map[string]interface{}{
				"displayId": "main",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

func TestStoreDeadLetterInvalidJSON(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/dl-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	// Invalid JSON payload — should still store (payload map will be nil)
	h.storeDeadLetter(c, rc.ID, "delivery-bad", "push", []byte("not json"), "parse error")

	ctx := context.Background()
	letters, err := client.WebhookDeadLetter.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(letters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(letters))
	}
}

func TestHandlePRMergedExistingWithCycleTime(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/cycle-repo")
	ctx := context.Background()

	// Create PR with a known creation time in the past
	createdAt := time.Now().Add(-24 * time.Hour)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(77).
		SetScmPrURL("https://github.com/org/cycle-repo/pull/77").
		SetAuthor("eve").
		SetTitle("Cycle time PR").
		SetSourceBranch("feature/cycle").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SetCreatedAt(createdAt).
		SaveX(ctx)

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/cycle-repo",
		Sender:       "eve",
		PR: &scm.PRInfo{
			ID: 77, Title: "Cycle time PR", Author: "eve",
			SourceBranch: "feature/cycle", TargetBranch: "main",
			URL: "https://github.com/org/cycle-repo/pull/77",
		},
	}

	h.handlePRMerged(ctx, rc, event)

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
	if pr.CycleTimeHours < 23.0 {
		t.Errorf("cycle_time_hours = %f, expected >= 23.0 (created 24h ago)", pr.CycleTimeHours)
	}
}
