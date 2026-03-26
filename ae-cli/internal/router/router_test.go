package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	r := New("http://example.com/api", "key", "model-1", []string{"claude", "codex"})
	if r == nil {
		t.Fatal("expected non-nil router")
	}
	if r.apiURL != "http://example.com/api/v1/chat/completions" {
		t.Errorf("apiURL = %q, want %q", r.apiURL, "http://example.com/api/v1/chat/completions")
	}
	if r.apiKey != "key" {
		t.Errorf("apiKey = %q, want %q", r.apiKey, "key")
	}
	if r.model != "model-1" {
		t.Errorf("model = %q, want %q", r.model, "model-1")
	}
	if len(r.tools) != 2 {
		t.Errorf("tools count = %d, want 2", len(r.tools))
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	r := New("http://example.com/api/", "key", "model-1", []string{"claude"})
	if r.apiURL != "http://example.com/api/v1/chat/completions" {
		t.Errorf("apiURL = %q, want trailing slash trimmed", r.apiURL)
	}
}

func TestRouteMatchingTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}

		// Verify request body
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "test-model" {
			t.Errorf("model = %q, want %q", req.Model, "test-model")
		}
		if len(req.Messages) != 2 {
			t.Errorf("messages count = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("first message role = %q, want system", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "user" {
			t.Errorf("second message role = %q, want user", req.Messages[1].Role)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "claude"}},
			},
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "test-key", "test-model", []string{"claude", "codex"})
	// Override apiURL to point to test server
	r.apiURL = srv.URL

	tool, err := r.Route("help me debug this code")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if tool != "claude" {
		t.Errorf("tool = %q, want %q", tool, "claude")
	}
}

func TestRouteFallbackToFirstTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "unknown-tool-name"}},
			},
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude", "codex"})
	r.apiURL = srv.URL

	tool, err := r.Route("some message")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Should fallback to first tool
	if tool != "claude" {
		t.Errorf("tool = %q, want %q (fallback)", tool, "claude")
	}
}

func TestRouteTrimsAndLowercases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "  CODEX  \n"}},
			},
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude", "codex"})
	r.apiURL = srv.URL

	tool, err := r.Route("run a command")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if tool != "codex" {
		t.Errorf("tool = %q, want %q", tool, "codex")
	}
}

func TestRouteServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude"})
	r.apiURL = srv.URL

	_, err := r.Route("test")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "LLM returned status 500") {
		t.Errorf("error = %q, want it to contain 'LLM returned status 500'", err.Error())
	}
}

func TestRouteNetworkError(t *testing.T) {
	r := New("http://127.0.0.1:1", "key", "model", []string{"claude"})
	r.apiURL = "http://127.0.0.1:1"

	_, err := r.Route("test")
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "calling LLM") {
		t.Errorf("error = %q, want it to contain 'calling LLM'", err.Error())
	}
}

func TestRouteEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: nil,
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude"})
	r.apiURL = srv.URL

	_, err := r.Route("test")
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error = %q, want it to contain 'no choices'", err.Error())
	}
}

func TestRouteBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude"})
	r.apiURL = srv.URL

	_, err := r.Route("test")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error = %q, want it to contain 'decoding response'", err.Error())
	}
}

func TestRouteNoToolsConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "anything"}},
			},
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{})
	r.apiURL = srv.URL

	_, err := r.Route("test")
	if err == nil {
		t.Fatal("expected error for no tools configured, got nil")
	}
	if !strings.Contains(err.Error(), "no tools configured") {
		t.Errorf("error = %q, want it to contain 'no tools configured'", err.Error())
	}
}

func TestRouteSystemPromptContainsTools(t *testing.T) {
	var receivedReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "claude"}},
			},
		})
	}))
	defer srv.Close()

	r := New(srv.URL, "key", "model", []string{"claude", "codex", "kiro"})
	r.apiURL = srv.URL

	r.Route("test message")

	// Verify system prompt contains tool names
	systemMsg := receivedReq.Messages[0].Content
	if !strings.Contains(systemMsg, "claude") {
		t.Error("system prompt should contain 'claude'")
	}
	if !strings.Contains(systemMsg, "codex") {
		t.Error("system prompt should contain 'codex'")
	}
	if !strings.Contains(systemMsg, "kiro") {
		t.Error("system prompt should contain 'kiro'")
	}

	// Verify user message is passed through
	userMsg := receivedReq.Messages[1].Content
	if userMsg != "test message" {
		t.Errorf("user message = %q, want %q", userMsg, "test message")
	}
}
