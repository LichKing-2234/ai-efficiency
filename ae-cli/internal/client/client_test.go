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

func TestEnsureRepoFromRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/ensure-remote" {
			t.Errorf("path = %s, want /api/v1/repos/ensure-remote", r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req["remote_url"] != "https://github.com/acme/platform.git" || req["branch"] != "main" {
			t.Fatalf("unexpected ensure repo request: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":              17,
				"repo_key":        "github.com/acme/platform",
				"full_name":       "github.com/acme/platform",
				"clone_url":       "https://github.com/acme/platform.git",
				"default_branch":  "main",
				"binding_state":   "unbound",
				"scm_provider_id": nil,
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	resp, err := c.EnsureRepoFromRemote(context.Background(), "https://github.com/acme/platform.git", "main")
	if err != nil {
		t.Fatalf("EnsureRepoFromRemote: %v", err)
	}
	if resp.RepoKey != "github.com/acme/platform" {
		t.Fatalf("repo_key = %q, want %q", resp.RepoKey, "github.com/acme/platform")
	}
}

func TestResolveRepoFromRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req["remote_url"] != "git@repo-host.example.com:org/repo.git" || req["client_cache_version"] != "repo-eligibility-v1" {
			t.Fatalf("request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"eligible":       true,
				"repo_config_id": 123,
				"repo_key":       "repo-host.example.com/org/repo",
				"full_name":      "org/repo",
				"clone_url":      "git@repo-host.example.com:org/repo.git",
				"status":         "active",
				"binding_state":  "unbound",
			},
		})
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ResolveRepoFromRemote(context.Background(), ResolveRepoRequest{
		RemoteURL:          "git@repo-host.example.com:org/repo.git",
		ClientCacheVersion: "repo-eligibility-v1",
	})
	if err != nil {
		t.Fatalf("ResolveRepoFromRemote: %v", err)
	}
	if !resp.Eligible || resp.RepoConfigID != 123 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestBatchHookEligible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/hook-eligible" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"version": "repo-eligibility-v1",
				"repos": []map[string]any{{
					"eligible":       true,
					"repo_config_id": 123,
					"repo_key":       "repo-host.example.com/org/repo",
				}},
				"ineligible": []map[string]any{{
					"eligible": false,
					"repo_key": "repo-host.example.com/org/missing",
					"reason":   "not_found",
				}},
			},
		})
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").BatchHookEligible(context.Background(), []HookEligibleRepoRequest{
		{RepoKey: "repo-host.example.com/org/repo", RemoteURL: "https://repo-host.example.com/org/repo.git"},
	})
	if err != nil {
		t.Fatalf("BatchHookEligible: %v", err)
	}
	if resp.Version != "repo-eligibility-v1" || len(resp.Repos) != 1 || len(resp.Ineligible) != 1 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestManagedPayloadsIncludeRepoConfigID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload["repo_config_id"].(float64) != 123 {
			t.Fatalf("payload missing repo_config_id: %#v", payload)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.SendCommitCheckpoint(context.Background(), CommitCheckpointRequest{
		EventID:       "cp",
		RepoConfigID:  123,
		WorkspaceID:   "ws",
		CommitSHA:     "abc",
		BindingSource: "unbound",
	}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}

func TestListProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/user/providers" {
			t.Errorf("path = %s, want /api/v1/user/providers", r.URL.Path)
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
						"default_model": "gpt-5.3-codex",
						"is_primary":    true,
						"groups": []map[string]any{
							{
								"group_id":   "42",
								"group_name": "OpenAI",
								"platform":   "openai",
								"credential": map[string]any{
									"api_key_id": 123,
									"key":        "sk-test",
									"status":     "active",
								},
							},
							{
								"group_id":   "43",
								"group_name": "Claude",
								"platform":   "anthropic",
								"credential": map[string]any{
									"api_key_id": 124,
									"key":        "sk-claude",
									"status":     "active",
								},
							},
							{
								"group_id":   "44",
								"group_name": "Gemini",
								"platform":   "gemini",
								"credential": map[string]any{
									"api_key_id": 125,
									"key":        "sk-inactive",
									"status":     "inactive",
								},
							},
						},
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
	if len(providers[0].Credentials) != 2 {
		t.Fatalf("credentials len = %d, want 2: %+v", len(providers[0].Credentials), providers[0].Credentials)
	}
	if providers[0].Credentials[0].Platform != "openai" || providers[0].Credentials[0].APIKey != "sk-test" {
		t.Fatalf("openai credential = %+v", providers[0].Credentials[0])
	}
	if providers[0].Credentials[1].Platform != "anthropic" || providers[0].Credentials[1].APIKey != "sk-claude" {
		t.Fatalf("anthropic credential = %+v", providers[0].Credentials[1])
	}
}

func TestListProvidersFallsBackToLegacyEndpoint(t *testing.T) {
	var sawUserEndpoint bool
	var sawLegacyEndpoint bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user/providers":
			sawUserEndpoint = true
			http.NotFound(w, r)
		case "/api/v1/providers":
			sawLegacyEndpoint = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{
					"providers": []map[string]any{
						{
							"name":          "legacy",
							"display_name":  "Legacy",
							"base_url":      "https://legacy.example.com/v1",
							"api_key":       "sk-legacy",
							"api_key_id":    456,
							"default_model": "gpt-5.3-codex",
							"is_primary":    true,
						},
					},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	providers, err := c.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if !sawUserEndpoint || !sawLegacyEndpoint {
		t.Fatalf("endpoint calls: user=%v legacy=%v, want both", sawUserEndpoint, sawLegacyEndpoint)
	}
	if len(providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(providers))
	}
	if providers[0].Name != "legacy" || providers[0].APIKey != "sk-legacy" {
		t.Fatalf("unexpected provider payload: %+v", providers[0])
	}
}
