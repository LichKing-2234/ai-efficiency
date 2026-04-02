package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNewClientEmptyToken(t *testing.T) {
	c := New("http://example.com", "")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.token != "" {
		t.Errorf("token = %q, want empty", c.token)
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

func TestSetHeadersWithoutToken(t *testing.T) {
	c := New("http://localhost", "")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	c.setHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", req.Header.Get("Content-Type"), "application/json")
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty, got %q", req.Header.Get("Authorization"))
	}
}

func TestCreateSession(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions" {
			t.Errorf("path = %s, want /api/v1/sessions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-token")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var req CreateSessionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if req.RepoFullName != "org/repo" {
			t.Errorf("repo_full_name = %q, want %q", req.RepoFullName, "org/repo")
		}
		if req.Branch != "main" {
			t.Errorf("branch = %q, want %q", req.Branch, "main")
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": Session{
				ID:        req.ID,
				Status:    "active",
				StartedAt: now,
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	sess, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-1",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID != "sess-1" {
		t.Errorf("session ID = %q, want %q", sess.ID, "sess-1")
	}
	if sess.Status != "active" {
		t.Errorf("session status = %q, want %q", sess.Status, "active")
	}
}

func TestCreateSessionWithOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": Session{
				ID:     "sess-ok",
				Status: "active",
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	sess, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-ok",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID != "sess-ok" {
		t.Errorf("session ID = %q, want %q", sess.ID, "sess-ok")
	}
}

func TestCreateSessionServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	_, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-err",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("error = %q, want it to contain 'unexpected status 500'", err.Error())
	}
}

func TestCreateSessionBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	_, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-bad",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err == nil {
		t.Fatal("expected error for bad JSON response, got nil")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error = %q, want it to contain 'decoding response'", err.Error())
	}
}

func TestCreateSessionNetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "tok") // port 1 should refuse connections
	_, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-net",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "sending request") {
		t.Errorf("error = %q, want it to contain 'sending request'", err.Error())
	}
}

func TestCreateSessionCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": Session{ID: "x"},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := New(srv.URL, "tok")
	_, err := c.CreateSession(ctx, CreateSessionRequest{
		ID:           "sess-cancel",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestHeartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess-42" {
			t.Errorf("path = %s, want /api/v1/sessions/sess-42", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.Heartbeat(context.Background(), "sess-42"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func TestHeartbeatNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.Heartbeat(context.Background(), "sess-nc"); err != nil {
		t.Fatalf("Heartbeat with 204: %v", err)
	}
}

func TestHeartbeatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server down"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.Heartbeat(context.Background(), "sess-err")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("error = %q, want it to contain 'unexpected status 500'", err.Error())
	}
}

func TestHeartbeatNetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "tok")
	err := c.Heartbeat(context.Background(), "sess-net")
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "sending request") {
		t.Errorf("error = %q, want it to contain 'sending request'", err.Error())
	}
}

func TestHeartbeatCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(srv.URL, "tok")
	err := c.Heartbeat(ctx, "sess-cancel")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestStopSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess-42/stop" {
			t.Errorf("path = %s, want /api/v1/sessions/sess-42/stop", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.StopSession(context.Background(), "sess-42"); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
}

func TestStopSessionOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if err := c.StopSession(context.Background(), "sess-ok"); err != nil {
		t.Fatalf("StopSession with 200: %v", err)
	}
}

func TestStopSessionServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.StopSession(context.Background(), "sess-err")
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 400") {
		t.Errorf("error = %q, want it to contain 'unexpected status 400'", err.Error())
	}
}

func TestStopSessionNetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "tok")
	err := c.StopSession(context.Background(), "sess-net")
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "sending request") {
		t.Errorf("error = %q, want it to contain 'sending request'", err.Error())
	}
}

func TestStopSessionCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(srv.URL, "tok")
	err := c.StopSession(ctx, "sess-cancel")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestAddInvocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess-42/invocations" {
			t.Errorf("path = %s, want /api/v1/sessions/sess-42/invocations", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var inv Invocation
		if err := json.Unmarshal(body, &inv); err != nil {
			t.Fatalf("unmarshal invocation: %v", err)
		}
		if inv.Tool != "claude" {
			t.Errorf("tool = %q, want %q", inv.Tool, "claude")
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	inv := Invocation{
		Tool:  "claude",
		Start: time.Now().Add(-5 * time.Second),
		End:   time.Now(),
	}
	if err := c.AddInvocation(context.Background(), "sess-42", inv); err != nil {
		t.Fatalf("AddInvocation: %v", err)
	}
}

func TestAddInvocationOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	inv := Invocation{Tool: "codex", Start: time.Now(), End: time.Now()}
	if err := c.AddInvocation(context.Background(), "sess-ok", inv); err != nil {
		t.Fatalf("AddInvocation with 200: %v", err)
	}
}

func TestAddInvocationServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	inv := Invocation{Tool: "claude", Start: time.Now(), End: time.Now()}
	err := c.AddInvocation(context.Background(), "sess-err", inv)
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 403") {
		t.Errorf("error = %q, want it to contain 'unexpected status 403'", err.Error())
	}
}

func TestAddInvocationNetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "tok")
	inv := Invocation{Tool: "claude", Start: time.Now(), End: time.Now()}
	err := c.AddInvocation(context.Background(), "sess-net", inv)
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "sending request") {
		t.Errorf("error = %q, want it to contain 'sending request'", err.Error())
	}
}

func TestAddInvocationCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(srv.URL, "tok")
	inv := Invocation{Tool: "claude", Start: time.Now(), End: time.Now()}
	err := c.AddInvocation(ctx, "sess-cancel", inv)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestCreateSessionRequestJSON(t *testing.T) {
	req := CreateSessionRequest{
		ID:           "test-id",
		RepoFullName: "org/repo",
		Branch:       "main",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]string
	json.Unmarshal(data, &decoded)
	if decoded["id"] != "test-id" {
		t.Errorf("id = %q, want %q", decoded["id"], "test-id")
	}
	if decoded["repo_full_name"] != "org/repo" {
		t.Errorf("repo_full_name = %q, want %q", decoded["repo_full_name"], "org/repo")
	}
}

func TestCreateSessionRequestJSONOmitEmpty(t *testing.T) {
	req := CreateSessionRequest{
		ID:           "test-id",
		RepoFullName: "org/repo",
		Branch:       "main",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "sub2api_api_key") {
		t.Errorf("expected sub2api_api_key to be omitted, got %s", string(data))
	}
}

func TestInvocationJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	inv := Invocation{
		Tool:  "claude",
		Start: now,
		End:   now.Add(10 * time.Second),
	}
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Invocation
	json.Unmarshal(data, &decoded)
	if decoded.Tool != "claude" {
		t.Errorf("tool = %q, want %q", decoded.Tool, "claude")
	}
	if !decoded.Start.Equal(now) {
		t.Errorf("start = %v, want %v", decoded.Start, now)
	}
	if !decoded.End.Equal(now.Add(10 * time.Second)) {
		t.Errorf("end = %v, want %v", decoded.End, now.Add(10*time.Second))
	}
}

func TestSessionJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	sess := Session{
		ID:        "sess-json",
		Status:    "active",
		StartedAt: now,
	}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Session
	json.Unmarshal(data, &decoded)
	if decoded.ID != "sess-json" {
		t.Errorf("id = %q, want %q", decoded.ID, "sess-json")
	}
	if decoded.Status != "active" {
		t.Errorf("status = %q, want %q", decoded.Status, "active")
	}
	if !decoded.StartedAt.Equal(now) {
		t.Errorf("started_at = %v, want %v", decoded.StartedAt, now)
	}
}

func TestHeartbeatNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("session not found"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.Heartbeat(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Errorf("error = %q, want it to contain 'unexpected status 404'", err.Error())
	}
}

func TestStopSessionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.StopSession(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %q, want ErrNotFound", err.Error())
	}
}

func TestAddInvocationNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("session not found"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	inv := Invocation{Tool: "claude", Start: time.Now(), End: time.Now()}
	err := c.AddInvocation(context.Background(), "nonexistent", inv)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Errorf("error = %q, want it to contain 'unexpected status 404'", err.Error())
	}
}

func TestCreateSessionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	_, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-nf",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Errorf("error = %q, want it to contain 'unexpected status 404'", err.Error())
	}
}

func TestNewClientHTTPClientTimeout(t *testing.T) {
	c := New("http://localhost:8080", "tok")
	if c.httpClient == nil {
		t.Fatal("httpClient should not be nil")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
}

func TestAddInvocationNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	inv := Invocation{Tool: "claude", Start: time.Now(), End: time.Now()}
	err := c.AddInvocation(context.Background(), "sess-nc", inv)
	// 204 is not in the accepted list (201, 200), so this should error
	if err == nil {
		t.Fatal("expected error for 204 response on AddInvocation")
	}
}

func TestCreateSessionEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	sess, err := c.CreateSession(context.Background(), CreateSessionRequest{
		ID:           "sess-empty",
		RepoFullName: "org/repo",
		Branch:       "main",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Data field will be zero-value
	if sess.ID != "" {
		t.Errorf("expected empty ID from empty envelope, got %q", sess.ID)
	}
}

func TestBootstrapSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/bootstrap" {
			t.Errorf("path = %s, want /api/v1/sessions/bootstrap", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-token")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var req BootstrapSessionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if req.RepoFullName != "org/repo" {
			t.Errorf("repo_full_name = %q, want %q", req.RepoFullName, "org/repo")
		}
		if req.BranchSnapshot != "main" {
			t.Errorf("branch_snapshot = %q, want %q", req.BranchSnapshot, "main")
		}
		if req.HeadSHA != "abc123" {
			t.Errorf("head_sha = %q, want %q", req.HeadSHA, "abc123")
		}
		if req.WorkspaceRoot != "/ws" {
			t.Errorf("workspace_root = %q, want %q", req.WorkspaceRoot, "/ws")
		}
		if req.GitDir != "/ws/.git" {
			t.Errorf("git_dir = %q, want %q", req.GitDir, "/ws/.git")
		}
		if req.GitCommonDir != "/ws/.git" {
			t.Errorf("git_common_dir = %q, want %q", req.GitCommonDir, "/ws/.git")
		}
		if req.WorkspaceID != "wsid-1" {
			t.Errorf("workspace_id = %q, want %q", req.WorkspaceID, "wsid-1")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": BootstrapSessionResponse{
				SessionID:     "00000000-0000-0000-0000-000000000001",
				StartedAt:     now,
				RelayUserID:   10,
				RelayAPIKeyID: 20,
				ProviderName:  "sub2api",
				GroupID:       "g-default",
				RuntimeRef:    "rt-1",
				EnvBundle: map[string]string{
					"AE_SESSION_ID":  "00000000-0000-0000-0000-000000000001",
					"AE_RUNTIME_REF": "rt-1",
				},
				KeyExpiresAt: now.Add(2 * time.Hour),
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	resp, err := c.BootstrapSession(context.Background(), BootstrapSessionRequest{
		RepoFullName:   "org/repo",
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/ws",
		GitDir:         "/ws/.git",
		GitCommonDir:   "/ws/.git",
		WorkspaceID:    "wsid-1",
	})
	if err != nil {
		t.Fatalf("BootstrapSession: %v", err)
	}
	if resp.SessionID != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "00000000-0000-0000-0000-000000000001")
	}
	if resp.ProviderName != "sub2api" {
		t.Errorf("provider_name = %q, want %q", resp.ProviderName, "sub2api")
	}
	if got := resp.EnvBundle["AE_SESSION_ID"]; got == "" {
		t.Errorf("env_bundle[AE_SESSION_ID] empty, want non-empty")
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
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-token")
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
		_, _ = w.Write([]byte(`{"code":201,"data":{"event_id":"cp-1"}}`))
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
		_, _ = w.Write([]byte(`{"code":201,"data":{"event_id":"rw-1"}}`))
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

func TestSendSessionEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/session-events" {
			t.Errorf("path = %s, want /api/v1/session-events", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-token")
		}

		var req SessionEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.EventID != "evt-1" || req.SessionID != "sess-1" || req.EventType != "post_commit" {
			t.Fatalf("unexpected session event request: %+v", req)
		}
		if !req.CapturedAt.Equal(now) {
			t.Fatalf("captured_at = %v, want %v", req.CapturedAt, now)
		}
		if req.RawPayload["commit_sha"] != "abc123" {
			t.Fatalf("raw_payload.commit_sha = %v, want %q", req.RawPayload["commit_sha"], "abc123")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if err := c.SendSessionEvent(context.Background(), SessionEventRequest{
		EventID:     "evt-1",
		SessionID:   "sess-1",
		WorkspaceID: "ws-1",
		EventType:   "post_commit",
		Source:      "proxy",
		CapturedAt:  now,
		RawPayload:  map[string]any{"commit_sha": "abc123"},
	}); err != nil {
		t.Fatalf("SendSessionEvent: %v", err)
	}
}
