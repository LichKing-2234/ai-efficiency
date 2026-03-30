package relay_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

func newTestProvider(t *testing.T, handler http.Handler) relay.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return relay.NewSub2apiProvider(srv.Client(), srv.URL+"/v1", srv.URL, "test-admin-key", "test-model", zap.NewNop())
}

func TestName(t *testing.T) {
	p := newTestProvider(t, http.NewServeMux())
	if p.Name() != "sub2api" {
		t.Fatalf("expected name 'sub2api', got %q", p.Name())
	}
}

func TestPing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})
	p := newTestProvider(t, mux)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}
}

func TestPingUnreachable(t *testing.T) {
	// Create a server and immediately close it so the URL is unreachable.
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	client := srv.Client()
	srv.Close()

	p := relay.NewSub2apiProvider(client, url+"/v1", url, "key", "model", zap.NewNop())
	if err := p.Ping(context.Background()); err == nil {
		t.Fatal("Ping() expected error for unreachable server, got nil")
	}
}

func TestPingNon200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	p := newTestProvider(t, mux)
	if err := p.Ping(context.Background()); err == nil {
		t.Fatal("Ping() expected error for non-200 status, got nil")
	}
}

func TestAuthenticate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode login body: %v", err)
		}
		if body.Email != "alice@example.com" || body.Password != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "invalid credentials"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "session-token-123"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer session-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice",
				"role":     "user",
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.Authenticate(context.Background(), "alice@example.com", "secret")
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if user.ID != 1 || user.Email != "alice@example.com" || user.Username != "alice" || user.Role != "user" {
		t.Fatalf("Authenticate() unexpected user: %+v", user)
	}
}

func TestAuthenticateEmptyUsername(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "tok-123"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       2,
				"email":    "bob@example.com",
				"username": "",
				"role":     "user",
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.Authenticate(context.Background(), "bob@example.com", "pass")
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if user.Username != "bob@example.com" {
		t.Fatalf("expected username fallback to email 'bob@example.com', got %q", user.Username)
	}
}

func TestAuthenticateInvalidCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "invalid credentials"})
	})

	p := newTestProvider(t, mux)
	_, err := p.Authenticate(context.Background(), "bad", "creds")
	if err != relay.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticateExtraVerification(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"requires_2fa": true},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.Authenticate(context.Background(), "user", "pass")
	if err != relay.ErrExtraVerificationRequired {
		t.Fatalf("expected ErrExtraVerificationRequired, got %v", err)
	}
}

func TestAuthenticateTurnstile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"turnstile": "required"},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.Authenticate(context.Background(), "user", "pass")
	if err != relay.ErrExtraVerificationRequired {
		t.Fatalf("expected ErrExtraVerificationRequired, got %v", err)
	}
}

func TestGetUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Errorf("expected admin API key in Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":       42,
				"email":    "bob@example.com",
				"username": "bob",
				"role":     "admin",
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.GetUser(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetUser() unexpected error: %v", err)
	}
	if user.ID != 42 || user.Email != "bob@example.com" || user.Username != "bob" || user.Role != "admin" {
		t.Fatalf("GetUser() unexpected user: %+v", user)
	}
}

func TestFindUserByEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		email := r.URL.Query().Get("email")
		if email == "notfound@example.com" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    []any{},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{
					"id":       10,
					"email":    email,
					"username": "found",
					"role":     "user",
				},
			},
		})
	})

	p := newTestProvider(t, mux)

	// Found case
	user, err := p.FindUserByEmail(context.Background(), "found@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() unexpected error: %v", err)
	}
	if user == nil || user.ID != 10 {
		t.Fatalf("FindUserByEmail() expected user with ID 10, got %+v", user)
	}

	// Not found case
	user, err = p.FindUserByEmail(context.Background(), "notfound@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() unexpected error: %v", err)
	}
	if user != nil {
		t.Fatalf("FindUserByEmail() expected nil for not found, got %+v", user)
	}
}

func TestChatCompletion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Errorf("expected API key in Authorization header")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test-model" {
			t.Errorf("expected model 'test-model', got %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "Hello from LLM!",
					},
				},
			},
			"usage": map[string]any{
				"total_tokens": 42,
			},
		})
	})

	p := newTestProvider(t, mux)
	resp, err := p.ChatCompletion(context.Background(), relay.ChatCompletionRequest{
		Messages: []relay.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() unexpected error: %v", err)
	}
	if resp.Content != "Hello from LLM!" {
		t.Fatalf("expected content 'Hello from LLM!', got %q", resp.Content)
	}
	if resp.TokensUsed != 42 {
		t.Fatalf("expected 42 tokens, got %d", resp.TokensUsed)
	}
}

func TestChatCompletionWithTools(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		tools, ok := body["tools"]
		if !ok {
			t.Error("expected tools in request body")
		}
		toolSlice, ok := tools.([]any)
		if !ok || len(toolSlice) == 0 {
			t.Error("expected non-empty tools array")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "",
						"tool_calls": []any{
							map[string]any{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"London"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]any{
				"total_tokens": 55,
			},
		})
	})

	p := newTestProvider(t, mux)
	resp, err := p.ChatCompletionWithTools(context.Background(),
		relay.ChatCompletionRequest{
			Messages: []relay.ChatMessage{{Role: "user", Content: "What's the weather?"}},
		},
		[]relay.ToolDef{{
			Type: "function",
			Function: relay.ToolFuncDef{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}},
	)
	if err != nil {
		t.Fatalf("ChatCompletionWithTools() unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("expected tool call 'get_weather', got %q", resp.ToolCalls[0].Function.Name)
	}
	if resp.TokensUsed != 55 {
		t.Fatalf("expected 55 tokens, got %d", resp.TokensUsed)
	}
}

func TestGetUsageStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/5/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if from == "" || to == "" {
			t.Error("expected from and to query params")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"total_tokens": 10000,
				"total_cost":   1.23,
			},
		})
	})

	p := newTestProvider(t, mux)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	stats, err := p.GetUsageStats(context.Background(), 5, from, to)
	if err != nil {
		t.Fatalf("GetUsageStats() unexpected error: %v", err)
	}
	if stats.TotalTokens != 10000 {
		t.Fatalf("expected 10000 tokens, got %d", stats.TotalTokens)
	}
	if stats.TotalCost != 1.23 {
		t.Fatalf("expected cost 1.23, got %f", stats.TotalCost)
	}
}

func TestListUserAPIKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{"id": 1, "user_id": 7, "name": "key-1", "status": "active"},
				map[string]any{"id": 2, "user_id": 7, "name": "key-2", "status": "disabled"},
			},
		})
	})

	p := newTestProvider(t, mux)
	keys, err := p.ListUserAPIKeys(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListUserAPIKeys() unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].Name != "key-1" || keys[1].Name != "key-2" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestCreateUserAPIKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Errorf("expected admin API key in Authorization header")
		}
		var body struct {
			UserID int64  `json:"user_id"`
			Name   string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.UserID != 3 || body.Name != "my-key" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":      99,
				"user_id": 3,
				"name":    "my-key",
				"status":  "active",
				"secret":  "sk-abc123",
			},
		})
	})

	p := newTestProvider(t, mux)
	key, err := p.CreateUserAPIKey(context.Background(), 3, relay.APIKeyCreateRequest{Name: "my-key"})
	if err != nil {
		t.Fatalf("CreateUserAPIKey() unexpected error: %v", err)
	}
	if key.ID != 99 || key.Secret != "sk-abc123" || key.Name != "my-key" {
		t.Fatalf("unexpected key: %+v", key)
	}
}

func TestAdminRequestError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "internal error",
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.GetUser(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// Ensure the provider uses the configured model, not the one from the request.
func TestChatCompletionUsesConfiguredModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test-model" {
			t.Errorf("expected model 'test-model', got %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"total_tokens":1}}`)
	})

	p := newTestProvider(t, mux)
	_, err := p.ChatCompletion(context.Background(), relay.ChatCompletionRequest{
		Model:    "should-be-overridden",
		Messages: []relay.ChatMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateUserAPIKeyWithExpiryAndGroup(t *testing.T) {
	exp := time.Date(2026, 3, 31, 1, 2, 3, 0, time.UTC)
	groupID := "team-ai"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body struct {
			UserID    int64  `json:"user_id"`
			Name      string `json:"name"`
			ExpiresAt string `json:"expires_at"`
			GroupID   string `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.UserID != 3 || body.Name != "my-key" || body.ExpiresAt != exp.Format(time.RFC3339) || body.GroupID != groupID {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":      99,
				"user_id": 3,
				"name":    "my-key",
				"status":  "active",
				"secret":  "sk-abc123",
			},
		})
	})

	p := newTestProvider(t, mux)
	key, err := p.CreateUserAPIKey(context.Background(), 3, relay.APIKeyCreateRequest{
		Name:      "my-key",
		ExpiresAt: &exp,
		GroupID:   groupID,
	})
	if err != nil {
		t.Fatalf("CreateUserAPIKey() unexpected error: %v", err)
	}
	if key.ID != 99 || key.Secret != "sk-abc123" || key.Name != "my-key" {
		t.Fatalf("unexpected key: %+v", key)
	}
}

func TestListUsageLogsByAPIKeyExact(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/usage_logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Query().Get("api_key_id"); got != "99" {
			t.Errorf("api_key_id=%q, want %q", got, "99")
		}
		if got := r.URL.Query().Get("from"); got != from.Format(time.RFC3339) {
			t.Errorf("from=%q, want %q", got, from.Format(time.RFC3339))
		}
		if got := r.URL.Query().Get("to"); got != to.Format(time.RFC3339) {
			t.Errorf("to=%q, want %q", got, to.Format(time.RFC3339))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{
					"id":            1,
					"request_id":    "req-1",
					"created_at":    "2026-03-01T00:00:01Z",
					"api_key_id":    99,
					"user_id":       3,
					"account_id":    "acct-1",
					"group_id":      "team-ai",
					"model":         "gpt-5.1",
					"input_tokens":  10,
					"output_tokens": 20,
					"cache_tokens":  3,
					"total_tokens":  33,
					"total_cost":    0.12,
					"actual_cost":   0.10,
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	logs, err := p.ListUsageLogsByAPIKeyExact(context.Background(), 99, from, to)
	if err != nil {
		t.Fatalf("ListUsageLogsByAPIKeyExact() unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].RequestID != "req-1" {
		t.Fatalf("expected RequestID=req-1, got %q", logs[0].RequestID)
	}
	if logs[0].APIKeyID != 99 || logs[0].UserID != 3 || logs[0].AccountID != "acct-1" || logs[0].GroupID != "team-ai" {
		t.Fatalf("unexpected ids: %+v", logs[0])
	}
	if logs[0].Model != "gpt-5.1" || logs[0].InputTokens != 10 || logs[0].OutputTokens != 20 || logs[0].CacheTokens != 3 {
		t.Fatalf("unexpected token/model fields: %+v", logs[0])
	}
	if logs[0].TotalTokens != 33 {
		t.Fatalf("unexpected log: %+v", logs[0])
	}
}
