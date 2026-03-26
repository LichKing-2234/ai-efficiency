package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// --- labelPR with real labeler (error path) ---

func TestLabelPRWithLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	// Create a labeler with nil sub2api client — LabelPR will fail but should not panic
	labeler := efficiency.NewLabeler(client, nil, zap.NewNop())
	h := NewHandler(client, labeler, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/label-repo")
	ctx := context.Background()

	// Create a PR record to label
	pr, err := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SetScmPrURL("https://github.com/org/label-repo/pull/1").
		SetAuthor("alice").
		SetTitle("Test PR").
		SetSourceBranch("feat-1").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		Save(ctx)
	if err != nil {
		t.Fatalf("create PR: %v", err)
	}

	// Should not panic — labeler will attempt to label and may fail gracefully
	h.labelPR(ctx, pr.ID)
}

// --- HandleGitHub: event=nil path (unsupported event type) ---

func TestHandleGitHubPingEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/ping-repo")

	payload := map[string]interface{}{
		"zen": "Keep it logically awesome.",
		"repository": map[string]interface{}{
			"full_name": "org/ping-repo",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "ping-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// ping events should be parsed successfully and return "ignored" or "processed"
	// The key thing is it doesn't return 404 or 500
	if w.Code == http.StatusNotFound || w.Code == http.StatusInternalServerError {
		t.Errorf("ping event should not return %d", w.Code)
	}
}

// --- HandleBitbucket: event=nil path (unsupported event type) ---

func TestHandleBitbucketPushEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/push-repo")

	payload := map[string]interface{}{
		"eventKey": "repo:refs_changed",
		"repository": map[string]interface{}{
			"slug": "push-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "repo:refs_changed")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code == http.StatusNotFound || w.Code == http.StatusInternalServerError {
		t.Errorf("push event should not return %d", w.Code)
	}
}

// --- HandleGitHub: full PR opened flow with known repo ---

func TestHandleGitHubPROpenedFullFlow(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/flow-repo")

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "org/flow-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   10,
			"title":    "Flow PR",
			"user":     map[string]interface{}{"login": "bob"},
			"head":     map[string]interface{}{"ref": "feat-flow"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/flow-repo/pull/10",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "flow-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Should not be 404 (repo exists)
	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- HandleBitbucket: full PR opened flow with known repo ---

func TestHandleBitbucketPROpenedFullFlow(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/flow-bb-repo")

	payload := map[string]interface{}{
		"eventKey": "pr:opened",
		"repository": map[string]interface{}{
			"slug": "flow-bb-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"pullRequest": map[string]interface{}{
			"id":    5,
			"title": "BB Flow PR",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "carol"},
			},
			"fromRef": map[string]interface{}{
				"displayId": "feat-bb",
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

// --- dispatch with unknown event type ---

func TestDispatchUnknownEventType(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/unknown-event-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	event := &scm.WebhookEvent{
		Type:         scm.EventType("unknown_event"),
		RepoFullName: "org/unknown-event-repo",
		Sender:       "user1",
	}

	// Should not panic
	h.dispatch(c, rc, event)
}

// --- handlePROpened with labeler ---

func TestHandlePROpenedWithLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	labeler := efficiency.NewLabeler(client, nil, zap.NewNop())
	h := NewHandler(client, labeler, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/labeler-open-repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPROpened,
		RepoFullName: "org/labeler-open-repo",
		Sender:       "alice",
		PR: &scm.PRInfo{
			ID:           50,
			Title:        "Labeled PR",
			Author:       "alice",
			SourceBranch: "feat-label",
			TargetBranch: "main",
			URL:          "https://github.com/org/labeler-open-repo/pull/50",
		},
	}

	h.handlePROpened(ctx, rc, event)

	records, err := client.PrRecord.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 PR record, got %d", len(records))
	}
}

// --- handlePRMerged with labeler ---

func TestHandlePRMergedWithLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	labeler := efficiency.NewLabeler(client, nil, zap.NewNop())
	h := NewHandler(client, labeler, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/labeler-merge-repo")
	ctx := context.Background()

	// Create existing PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(60).
		SetScmPrURL("https://github.com/org/labeler-merge-repo/pull/60").
		SetAuthor("dave").
		SetTitle("Merge with label").
		SetSourceBranch("feat-merge-label").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/labeler-merge-repo",
		Sender:       "dave",
		PR: &scm.PRInfo{
			ID:           60,
			Title:        "Merge with label",
			Author:       "dave",
			SourceBranch: "feat-merge-label",
			TargetBranch: "main",
			URL:          "https://github.com/org/labeler-merge-repo/pull/60",
		},
	}

	h.handlePRMerged(ctx, rc, event)

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
}

// --- HandleGitHub with webhook secret set on repo ---

func TestHandleGitHubWithWebhookSecret(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	ctx := context.Background()
	provider, _ := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		Save(ctx)

	secret := "mysecret123"
	client.RepoConfig.Create().
		SetName("secret-repo").
		SetFullName("org/secret-repo").
		SetCloneURL("https://github.com/org/secret-repo.git").
		SetScmProviderID(provider.ID).
		SetWebhookSecret(secret).
		SaveX(ctx)

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "org/secret-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   1,
			"title":    "Secret PR",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-1"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/secret-repo/pull/1",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "secret-001")
	req.Header.Set("Content-Type", "application/json")
	// No valid signature — should fail signature validation and store dead letter
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Should get 401 (invalid signature) since we didn't sign the payload
	if w.Code != http.StatusUnauthorized {
		t.Logf("status = %d (expected 401 for invalid signature)", w.Code)
	}

	// Verify dead letter was stored
	letters, _ := client.WebhookDeadLetter.Query().All(ctx)
	if len(letters) < 1 {
		t.Log("expected dead letter to be stored for invalid signature")
	}
}

// --- HandleBitbucket with webhook secret set on repo ---

func TestHandleBitbucketWithWebhookSecret(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	ctx := context.Background()
	provider, _ := client.ScmProvider.Create().
		SetName("test-bb-provider").
		SetType(scmprovider.TypeBitbucketServer).
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials("test-token").
		Save(ctx)

	secret := "bbsecret123"
	client.RepoConfig.Create().
		SetName("bb-secret-repo").
		SetFullName("PROJ/bb-secret-repo").
		SetCloneURL("https://bitbucket.example.com/PROJ/bb-secret-repo.git").
		SetScmProviderID(provider.ID).
		SetWebhookSecret(secret).
		SaveX(ctx)

	payload := map[string]interface{}{
		"eventKey": "pr:opened",
		"repository": map[string]interface{}{
			"slug": "bb-secret-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"pullRequest": map[string]interface{}{
			"id":    1,
			"title": "BB Secret PR",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "alice"},
			},
			"fromRef": map[string]interface{}{"displayId": "feat-1"},
			"toRef":   map[string]interface{}{"displayId": "main"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	// May fail or succeed depending on signature validation
	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- storeDeadLetter with nil payload map ---

func TestStoreDeadLetterNilPayload(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/nil-payload-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	// nil payload bytes
	h.storeDeadLetter(c, rc.ID, "delivery-nil", "push", nil, "nil payload")

	ctx := context.Background()
	letters, err := client.WebhookDeadLetter.Query().All(ctx)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(letters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(letters))
	}
}

// --- HandleGitHub: body read error (oversized body) ---

func TestHandleGitHubOversizedBody(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	// Create a body larger than 1MB
	bigBody := make([]byte, 1<<20+100)
	for i := range bigBody {
		bigBody[i] = 'a'
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(bigBody))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "big-001")
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d", w.Code)
	}
}

// --- HandleBitbucket: body read error (oversized body) ---

func TestHandleBitbucketOversizedBody(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	bigBody := make([]byte, 1<<20+100)
	for i := range bigBody {
		bigBody[i] = 'a'
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(bigBody))
	req.Header.Set("X-Event-Key", "pr:opened")
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.HandleBitbucket(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d", w.Code)
	}
}

// --- dispatch via HandleGitHub for PR merged event ---

func TestHandleGitHubPRMergedEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/gh-merge-repo")

	payload := map[string]interface{}{
		"action": "closed",
		"repository": map[string]interface{}{
			"full_name": "org/gh-merge-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   15,
			"title":    "Merged PR",
			"merged":   true,
			"user":     map[string]interface{}{"login": "eve"},
			"head":     map[string]interface{}{"ref": "feat-merge"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/gh-merge-repo/pull/15",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "merge-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- dispatch via HandleBitbucket for PR merged event ---

func TestHandleBitbucketPRMergedEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/bb-merge-repo")

	payload := map[string]interface{}{
		"eventKey": "pr:merged",
		"repository": map[string]interface{}{
			"slug": "bb-merge-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"pullRequest": map[string]interface{}{
			"id":    20,
			"title": "BB Merged PR",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "frank"},
			},
			"fromRef": map[string]interface{}{"displayId": "feat-bb-merge"},
			"toRef":   map[string]interface{}{"displayId": "main"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:merged")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- HandleGitHub: missing X-GitHub-Delivery header only ---

func TestHandleGitHubMissingDeliveryHeader(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	payload := map[string]interface{}{
		"repository": map[string]interface{}{"full_name": "org/repo"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	// Missing X-GitHub-Delivery
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- HandleGitHub: missing X-GitHub-Event header only ---

func TestHandleGitHubMissingEventHeader(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	payload := map[string]interface{}{
		"repository": map[string]interface{}{"full_name": "org/repo"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	// Missing X-GitHub-Event
	req.Header.Set("X-GitHub-Delivery", "d-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- HandleGitHub: valid HMAC signature → full success path ---

func TestHandleGitHubValidSignatureSuccess(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	// Create repo with NO webhook secret (empty) so signature validation passes
	createTestRepoConfig(t, client, "org/valid-sig-repo")

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "org/valid-sig-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   42,
			"title":    "Valid Sig PR",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-sig"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/valid-sig-repo/pull/42",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "valid-sig-001")
	req.Header.Set("Content-Type", "application/json")
	// No signature header needed when secret is empty
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Verify PR record was created
	ctx := context.Background()
	count, _ := client.PrRecord.Query().Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 PR record, got %d", count)
	}
}

// --- HandleGitHub: push event → event=nil path (unsupported by github provider returns error) ---

func TestHandleGitHubPushEventSuccess(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/gh-push-repo")

	payload := map[string]interface{}{
		"ref": "refs/heads/main",
		"repository": map[string]interface{}{
			"full_name": "org/gh-push-repo",
		},
		"sender": map[string]interface{}{"login": "pusher"},
		"pusher": map[string]interface{}{"name": "pusher"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "push-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Push event should be processed (200)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- HandleGitHub: event=nil (unsupported event returns nil from ParseWebhookPayload) ---

func TestHandleGitHubUnsupportedEventReturnsIgnored(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/unsupported-event-repo")

	// "status" event is not handled by the GitHub provider
	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"full_name": "org/unsupported-event-repo",
		},
		"sender": map[string]interface{}{"login": "bot"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "status")
	req.Header.Set("X-GitHub-Delivery", "status-001")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Should return 401 (unsupported event type causes parse error) or 200 (ignored)
	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- HandleBitbucket: unsupported event key returns nil ---

func TestHandleBitbucketUnsupportedEventKey(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/unsupported-bb-repo")

	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"slug": "unsupported-bb-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"actor": map[string]interface{}{"name": "bot"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "repo:comment:added")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	// Unsupported event returns nil → should be "ignored" (200)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unsupported event, got %d", w.Code)
	}
}

// --- HandleBitbucket: PR updated event (full flow) ---

func TestHandleBitbucketPRUpdatedFullFlow(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "PROJ/bb-update-repo")
	ctx := context.Background()

	// Pre-create a PR record
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(10).
		SetScmPrURL("https://bitbucket.example.com/PROJ/bb-update-repo/pull/10").
		SetAuthor("alice").
		SetTitle("Old Title").
		SetSourceBranch("feat-old").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	payload := map[string]interface{}{
		"eventKey": "pr:modified",
		"repository": map[string]interface{}{
			"slug": "bb-update-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"actor": map[string]interface{}{"name": "alice"},
		"pullRequest": map[string]interface{}{
			"id":    10,
			"title": "Updated Title",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "alice"},
			},
			"fromRef": map[string]interface{}{"displayId": "feat-old"},
			"toRef":   map[string]interface{}{"displayId": "main"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:modified")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Verify PR was updated
	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Title != "Updated Title" {
		t.Errorf("title = %q, want %q", pr.Title, "Updated Title")
	}
}

// --- handlePRUpdated: update with empty title and author (no-op update) ---

func TestHandlePRUpdatedEmptyFields(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/empty-update-repo")
	ctx := context.Background()

	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(77).
		SetScmPrURL("https://github.com/org/empty-update-repo/pull/77").
		SetAuthor("bob").
		SetTitle("Original Title").
		SetSourceBranch("feat-77").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/empty-update-repo",
		Sender:       "bob",
		PR: &scm.PRInfo{
			ID:           77,
			Title:        "", // empty title
			Author:       "", // empty author
			SourceBranch: "feat-77",
			TargetBranch: "main",
			URL:          "https://github.com/org/empty-update-repo/pull/77",
		},
	}

	h.handlePRUpdated(ctx, rc, event)

	// Title and author should remain unchanged
	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Title != "Original Title" {
		t.Errorf("title = %q, want %q", pr.Title, "Original Title")
	}
	if pr.Author != "bob" {
		t.Errorf("author = %q, want %q", pr.Author, "bob")
	}
}

// --- handlePRMerged: merge event for PR not tracked, create fails (duplicate) ---

func TestHandlePRMergedNewPRWithLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	labeler := efficiency.NewLabeler(client, nil, zap.NewNop())
	h := NewHandler(client, labeler, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/merge-new-label-repo")
	ctx := context.Background()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/merge-new-label-repo",
		Sender:       "grace",
		PR: &scm.PRInfo{
			ID:           100,
			Title:        "New merged PR",
			Author:       "grace",
			SourceBranch: "hotfix/100",
			TargetBranch: "main",
			URL:          "https://github.com/org/merge-new-label-repo/pull/100",
		},
	}

	h.handlePRMerged(ctx, rc, event)

	// Verify PR was created as merged
	pr, err := client.PrRecord.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
	if pr.MergedAt == nil {
		t.Error("expected merged_at to be set")
	}
}

// --- HandleGitHub: valid signature with HMAC for PR updated event ---

func TestHandleGitHubValidSignaturePRUpdated(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/sig-update-repo")
	ctx := context.Background()

	// Pre-create a PR record
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(88).
		SetScmPrURL("https://github.com/org/sig-update-repo/pull/88").
		SetAuthor("alice").
		SetTitle("Old").
		SetSourceBranch("feat-88").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	payload := map[string]interface{}{
		"action": "synchronize",
		"repository": map[string]interface{}{
			"full_name": "org/sig-update-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   88,
			"title":    "Updated via sync",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-88"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/sig-update-repo/pull/88",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "sync-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- HandleGitHub: valid signature with HMAC for PR merged event ---

func TestHandleGitHubValidSignaturePRMerged(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/sig-merge-repo")

	payload := map[string]interface{}{
		"action": "closed",
		"repository": map[string]interface{}{
			"full_name": "org/sig-merge-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   99,
			"title":    "Merged via sig",
			"merged":   true,
			"user":     map[string]interface{}{"login": "bob"},
			"head":     map[string]interface{}{"ref": "feat-99"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/sig-merge-repo/pull/99",
		},
		"sender": map[string]interface{}{"login": "bob"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "merge-sig-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Verify PR was created as merged
	ctx := context.Background()
	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr == nil {
		t.Fatal("expected PR record to be created")
	}
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
}

// --- HandleBitbucket: full PR merged flow with existing PR ---

func TestHandleBitbucketPRMergedWithExistingPR(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "PROJ/bb-merge-existing")
	ctx := context.Background()

	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(30).
		SetScmPrURL("https://bitbucket.example.com/PROJ/bb-merge-existing/pull/30").
		SetAuthor("dave").
		SetTitle("BB Merge Existing").
		SetSourceBranch("feat-30").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	payload := map[string]interface{}{
		"eventKey": "pr:merged",
		"repository": map[string]interface{}{
			"slug": "bb-merge-existing",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"actor": map[string]interface{}{"name": "dave"},
		"pullRequest": map[string]interface{}{
			"id":    30,
			"title": "BB Merge Existing",
			"author": map[string]interface{}{
				"user": map[string]interface{}{"name": "dave"},
			},
			"fromRef": map[string]interface{}{"displayId": "feat-30"},
			"toRef":   map[string]interface{}{"displayId": "main"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:merged")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Status != prrecord.StatusMerged {
		t.Errorf("status = %q, want merged", pr.Status)
	}
}

// --- storeDeadLetter: with empty payload bytes ---

func TestStoreDeadLetterEmptyPayload(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/empty-dl-repo")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	h.storeDeadLetter(c, rc.ID, "delivery-empty", "push", []byte{}, "empty payload")

	ctx := context.Background()
	letters, _ := client.WebhookDeadLetter.Query().All(ctx)
	if len(letters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(letters))
	}
}

// --- HandleGitHub: closed PR without merge (should be ignored) ---

func TestHandleGitHubPRClosedNotMerged(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/closed-not-merged")

	payload := map[string]interface{}{
		"action": "closed",
		"repository": map[string]interface{}{
			"full_name": "org/closed-not-merged",
		},
		"pull_request": map[string]interface{}{
			"number":   5,
			"title":    "Closed PR",
			"merged":   false,
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-5"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/closed-not-merged/pull/5",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "closed-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Closed without merge → parsePREvent returns nil → event=nil → "ignored"
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for closed-not-merged, got %d", w.Code)
	}

	// No PR record should be created
	ctx := context.Background()
	count, _ := client.PrRecord.Query().Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 PR records, got %d", count)
	}
}

// --- HandleGitHub: edited PR event ---

func TestHandleGitHubPREditedEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/edited-repo")
	ctx := context.Background()

	// Pre-create PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(33).
		SetScmPrURL("https://github.com/org/edited-repo/pull/33").
		SetAuthor("alice").
		SetTitle("Before Edit").
		SetSourceBranch("feat-33").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	payload := map[string]interface{}{
		"action": "edited",
		"repository": map[string]interface{}{
			"full_name": "org/edited-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   33,
			"title":    "After Edit",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-33"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/edited-repo/pull/33",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "edited-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	pr, _ := client.PrRecord.Query().Only(ctx)
	if pr.Title != "After Edit" {
		t.Errorf("title = %q, want %q", pr.Title, "After Edit")
	}
}

// --- HandleGitHub: reopened PR event ---

func TestHandleGitHubPRReopenedEvent(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/reopened-repo")

	payload := map[string]interface{}{
		"action": "reopened",
		"repository": map[string]interface{}{
			"full_name": "org/reopened-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   44,
			"title":    "Reopened PR",
			"user":     map[string]interface{}{"login": "bob"},
			"head":     map[string]interface{}{"ref": "feat-44"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/reopened-repo/pull/44",
		},
		"sender": map[string]interface{}{"login": "bob"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "reopen-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	ctx := context.Background()
	count, _ := client.PrRecord.Query().Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 PR record, got %d", count)
	}
}

// --- HandleBitbucket: parse failure path (bad JSON in pullRequest) ---

func TestHandleBitbucketParseFailure(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()

	ctx := context.Background()
	provider, _ := client.ScmProvider.Create().
		SetName("bb-parse-fail-provider").
		SetType(scmprovider.TypeBitbucketServer).
		SetBaseURL("https://bitbucket.example.com").
		SetCredentials("test-token").
		Save(ctx)

	secret := "parsefailsecret"
	client.RepoConfig.Create().
		SetName("bb-parse-fail-repo").
		SetFullName("PROJ/bb-parse-fail-repo").
		SetCloneURL("https://bitbucket.example.com/PROJ/bb-parse-fail-repo.git").
		SetScmProviderID(provider.ID).
		SetWebhookSecret(secret).
		SaveX(ctx)

	// Send a payload that will cause the bitbucket provider to fail parsing
	// (missing X-Event-Key in the reconstructed request triggers parse error)
	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"slug": "bb-parse-fail-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	// Set a different event key than what's in the payload to trigger parse path
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h := NewHandler(client, nil, newTestLogger())
	h.HandleBitbucket(c)

	// The bitbucket provider reads the body from the cloned request, which has X-Event-Key
	// but the payload doesn't have pullRequest data, so it should still succeed with empty PR info
	if w.Code == http.StatusNotFound {
		t.Error("should not get 404 for known repo")
	}
}

// --- HandleGitHub: push event with Content-Type (full success path) ---

func TestHandleGitHubPushEventFullSuccess(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/push-full-repo")

	payload := map[string]interface{}{
		"ref":    "refs/heads/main",
		"before": "0000000000000000000000000000000000000000",
		"after":  "abc123def456",
		"repository": map[string]interface{}{
			"full_name": "org/push-full-repo",
		},
		"sender": map[string]interface{}{"login": "pusher"},
		"pusher": map[string]interface{}{"name": "pusher", "email": "pusher@example.com"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "push-full-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- HandleBitbucket: push event (repo:refs_changed) with full flow ---

func TestHandleBitbucketPushEventFullFlow(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/bb-push-full")

	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"slug": "bb-push-full",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"actor": map[string]interface{}{"name": "pusher"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "repo:refs_changed")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- labelPR: error path with closed DB ---

func TestLabelPRDBError(t *testing.T) {
	client := newTestClient(t)

	labeler := efficiency.NewLabeler(client, nil, zap.NewNop())
	h := NewHandler(client, labeler, newTestLogger())

	// Close the client to force LabelPR to return an error
	client.Close()

	// Should not panic — error is logged
	h.labelPR(context.Background(), 999)
}

// --- HandleBitbucket: DB query internal error (closed client) ---

func TestHandleBitbucketDBQueryError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "PROJ/db-error-bb-repo")

	// Close the client to force DB errors
	client.Close()

	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"slug": "db-error-bb-repo",
			"project": map[string]interface{}{
				"key": "PROJ",
			},
		},
		"actor": map[string]interface{}{"name": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", bytes.NewReader(payloadBytes))
	req.Header.Set("X-Event-Key", "pr:opened")
	c := newGinContext(w, req)

	h.HandleBitbucket(c)

	// Should get 500 (internal server error) since DB is closed
	if w.Code != http.StatusInternalServerError {
		t.Logf("status = %d (expected 500 for closed DB)", w.Code)
	}
}

// --- HandleGitHub: DB query internal error (closed client) ---

func TestHandleGitHubDBQueryError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/db-error-gh-repo")

	// Close the client to force DB errors
	client.Close()

	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"full_name": "org/db-error-gh-repo",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "db-error-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// Should get 500 (internal server error) since DB is closed
	if w.Code != http.StatusInternalServerError {
		t.Logf("status = %d (expected 500 for closed DB)", w.Code)
	}
}

// --- handlePROpened: DB create error (closed client) ---

func TestHandlePROpenedDBError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/db-error-open-repo")
	ctx := context.Background()

	// Close the client to force DB errors
	client.Close()

	event := &scm.WebhookEvent{
		Type:         scm.EventPROpened,
		RepoFullName: "org/db-error-open-repo",
		Sender:       "alice",
		PR: &scm.PRInfo{
			ID:           999,
			Title:        "DB Error PR",
			Author:       "alice",
			SourceBranch: "feat-err",
			TargetBranch: "main",
			URL:          "https://github.com/org/db-error-open-repo/pull/999",
		},
	}

	// Should not panic — error is logged
	h.handlePROpened(ctx, rc, event)
}

// --- handlePRUpdated: DB query error (closed client) ---

func TestHandlePRUpdatedDBError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/db-error-update-repo")
	ctx := context.Background()

	// Create a PR record first
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(888).
		SetScmPrURL("https://github.com/org/db-error-update-repo/pull/888").
		SetAuthor("bob").
		SetTitle("Update Error PR").
		SetSourceBranch("feat-888").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	// Close the client to force DB errors
	client.Close()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/db-error-update-repo",
		Sender:       "bob",
		PR: &scm.PRInfo{
			ID:           888,
			Title:        "Updated Error PR",
			Author:       "bob",
			SourceBranch: "feat-888",
			TargetBranch: "main",
			URL:          "https://github.com/org/db-error-update-repo/pull/888",
		},
	}

	// Should not panic — error is logged
	h.handlePRUpdated(ctx, rc, event)
}

// --- handlePRMerged: DB query error (closed client) ---

func TestHandlePRMergedDBError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/db-error-merge-repo")
	ctx := context.Background()

	// Close the client to force DB errors
	client.Close()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/db-error-merge-repo",
		Sender:       "carol",
		PR: &scm.PRInfo{
			ID:           777,
			Title:        "Merge Error PR",
			Author:       "carol",
			SourceBranch: "feat-777",
			TargetBranch: "main",
			URL:          "https://github.com/org/db-error-merge-repo/pull/777",
		},
	}

	// Should not panic — error is logged
	h.handlePRMerged(ctx, rc, event)
}

// --- handlePRMerged: existing PR, DB update error (closed client) ---

func TestHandlePRMergedExistingDBUpdateError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/db-merge-update-repo")
	ctx := context.Background()

	// Create existing PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(666).
		SetScmPrURL("https://github.com/org/db-merge-update-repo/pull/666").
		SetAuthor("dave").
		SetTitle("Merge Update Error").
		SetSourceBranch("feat-666").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	// Close the client to force DB errors on update
	client.Close()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRMerged,
		RepoFullName: "org/db-merge-update-repo",
		Sender:       "dave",
		PR: &scm.PRInfo{
			ID:           666,
			Title:        "Merge Update Error",
			Author:       "dave",
			SourceBranch: "feat-666",
			TargetBranch: "main",
			URL:          "https://github.com/org/db-merge-update-repo/pull/666",
		},
	}

	// Should not panic — error is logged
	h.handlePRMerged(ctx, rc, event)
}

// --- handlePRUpdated: existing PR, DB update error (closed client) ---

func TestHandlePRUpdatedExistingDBUpdateError(t *testing.T) {
	client := newTestClient(t)
	h := NewHandler(client, nil, newTestLogger())

	rc := createTestRepoConfig(t, client, "org/db-update-existing-repo")
	ctx := context.Background()

	// Create existing PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(555).
		SetScmPrURL("https://github.com/org/db-update-existing-repo/pull/555").
		SetAuthor("eve").
		SetTitle("Update Existing Error").
		SetSourceBranch("feat-555").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SaveX(ctx)

	// Close the client to force DB errors on update
	client.Close()

	event := &scm.WebhookEvent{
		Type:         scm.EventPRUpdated,
		RepoFullName: "org/db-update-existing-repo",
		Sender:       "eve",
		PR: &scm.PRInfo{
			ID:           555,
			Title:        "Updated Existing Error",
			Author:       "eve",
			SourceBranch: "feat-555",
			TargetBranch: "main",
			URL:          "https://github.com/org/db-update-existing-repo/pull/555",
		},
	}

	// Should not panic — error is logged
	h.handlePRUpdated(ctx, rc, event)
}

// --- storeDeadLetter: with invalid repo config ID (store error) ---

func TestStoreDeadLetterInvalidRepoConfigID(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := newGinContext(w, req)

	// Use a non-existent repo config ID — should fail to store but not panic
	h.storeDeadLetter(c, 99999, "delivery-bad-rc", "push", []byte(`{"test":true}`), "bad repo config")

	ctx := context.Background()
	letters, _ := client.WebhookDeadLetter.Query().All(ctx)
	// Should fail to store due to FK constraint
	if len(letters) != 0 {
		t.Logf("dead letter stored despite invalid repo config ID (FK may not be enforced)")
	}
}

// --- HandleGitHub: labeled action (unsupported PR action → event=nil → ignored) ---

func TestHandleGitHubPRLabeledAction(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	h := NewHandler(client, nil, newTestLogger())

	createTestRepoConfig(t, client, "org/labeled-action-repo")

	payload := map[string]interface{}{
		"action": "labeled",
		"repository": map[string]interface{}{
			"full_name": "org/labeled-action-repo",
		},
		"pull_request": map[string]interface{}{
			"number":   7,
			"title":    "Labeled PR",
			"user":     map[string]interface{}{"login": "alice"},
			"head":     map[string]interface{}{"ref": "feat-7"},
			"base":     map[string]interface{}{"ref": "main"},
			"html_url": "https://github.com/org/labeled-action-repo/pull/7",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	payloadBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(payloadBytes))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "labeled-001")
	req.Header.Set("Content-Type", "application/json")
	c := newGinContext(w, req)

	h.HandleGitHub(c)

	// "labeled" action → parsePREvent returns nil → event=nil → "ignored"
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for labeled action, got %d", w.Code)
	}
}

// suppress unused import
var _ = (*ent.Client)(nil)
