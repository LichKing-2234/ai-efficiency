package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := New("http://localhost:8080", "tok")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "http://localhost:8080")
	}
	if c.token != "tok" {
		t.Errorf("token = %q, want %q", c.token, "tok")
	}
}

func TestSetHeadersWithToken(t *testing.T) {
	c := New("http://localhost", "my-token")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	c.setHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", req.Header.Get("Content-Type"), "application/json")
	}
	if req.Header.Get("Authorization") != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", req.Header.Get("Authorization"), "Bearer my-token")
	}
}

func TestSendCommitCheckpoint(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/checkpoints/commit" {
			t.Errorf("path = %s, want /api/v1/checkpoints/commit", r.URL.Path)
		}
		var req CommitCheckpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.EventID != "cp-1" || req.RepoFullName != "org/repo" || req.CommitSHA != "abc123" {
			t.Fatalf("unexpected checkpoint request: %+v", req)
		}
		if req.CapturedAt == nil || !req.CapturedAt.Equal(now) {
			t.Fatalf("captured_at = %v, want %v", req.CapturedAt, now)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if err := c.SendCommitCheckpoint(context.Background(), CommitCheckpointRequest{
		EventID:        "cp-1",
		SessionID:      "sess-1",
		RepoFullName:   "org/repo",
		WorkspaceID:    "ws-1",
		CommitSHA:      "abc123",
		ParentSHAs:     []string{"000000"},
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		BindingSource:  "marker",
		CapturedAt:     &now,
	}); err != nil {
		t.Fatalf("SendCommitCheckpoint: %v", err)
	}
}

func TestSendCommitRewrite(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/checkpoints/rewrite" {
			t.Errorf("path = %s, want /api/v1/checkpoints/rewrite", r.URL.Path)
		}
		var req CommitRewriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.EventID != "rw-1" || req.RepoFullName != "org/repo" || req.OldCommitSHA != "old123" || req.NewCommitSHA != "new456" {
			t.Fatalf("unexpected rewrite request: %+v", req)
		}
		if req.CapturedAt == nil || !req.CapturedAt.Equal(now) {
			t.Fatalf("captured_at = %v, want %v", req.CapturedAt, now)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if err := c.SendCommitRewrite(context.Background(), CommitRewriteRequest{
		EventID:       "rw-1",
		SessionID:     "sess-1",
		RepoFullName:  "org/repo",
		WorkspaceID:   "ws-1",
		RewriteType:   "amend",
		OldCommitSHA:  "old123",
		NewCommitSHA:  "new456",
		BindingSource: "marker",
		CapturedAt:    &now,
	}); err != nil {
		t.Fatalf("SendCommitRewrite: %v", err)
	}
}

func TestSendToolUsageEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/tool-usage-events" {
			t.Errorf("path = %s, want /api/v1/tool-usage-events", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.SendToolUsageEvent(context.Background(), ToolUsageEventRequest{
		Tool:            "codex",
		WorkspaceID:     "ws-1",
		ToolSessionID:   "conv-1",
		DedupeKey:       "codex:conv-1:resp-1",
		UsageUnit:       "token",
		ObservedStartAt: time.Now().UTC(),
		ObservedEndAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SendToolUsageEvent: %v", err)
	}
}

func TestListProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/providers" {
			t.Errorf("path = %s, want /api/v1/providers", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"providers": []map[string]any{
					{
						"name":          "primary",
						"display_name":  "Primary",
						"base_url":      "https://relay.example.com/v1",
						"api_key":       "sk-test",
						"api_key_id":    123,
						"default_model": "gpt-5.3-codex",
						"is_primary":    true,
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	providers, err := c.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(providers))
	}
	if providers[0].Name != "primary" || providers[0].APIKey != "sk-test" || !providers[0].IsPrimary {
		t.Fatalf("unexpected provider payload: %+v", providers[0])
	}
}
