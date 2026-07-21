package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
)

func newTestProvider(t *testing.T, handler http.Handler) relay.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := relay.NewSub2apiProvider(srv.Client(), srv.URL+"/v1", "test-llm-key", "test-model", zap.NewNop())
	if setter, ok := p.(interface{ SetAdminAPIKey(string) }); ok {
		setter.SetAdminAPIKey("test-admin-key")
	}
	return p
}

func paddedJSONBody(t *testing.T, raw string, size int) []byte {
	t.Helper()
	if len(raw) > size {
		t.Fatalf("JSON fixture size = %d, exceeds requested size %d", len(raw), size)
	}
	body := make([]byte, size)
	copy(body, raw)
	for index := len(raw); index < len(body); index++ {
		body[index] = ' '
	}
	return body
}

func TestName(t *testing.T) {
	p := newTestProvider(t, http.NewServeMux())
	if p.Name() != "sub2api" {
		t.Fatalf("expected name 'sub2api', got %q", p.Name())
	}
}

func TestNewSub2apiProviderNormalizesInferenceBaseURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{"content": "pong"},
				},
			},
			"usage": map[string]any{"total_tokens": 5},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := relay.NewSub2apiProvider(srv.Client(), srv.URL, "test-llm-key", "gpt-5.4", zap.NewNop())
	resp, err := p.ChatCompletion(context.Background(), relay.ChatCompletionRequest{
		Messages: []relay.ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() unexpected error: %v", err)
	}
	if resp.Content != "pong" || resp.TokensUsed != 5 {
		t.Fatalf("unexpected response: %+v", resp)
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

func TestPingDrainsBoundedBodyAndReusesConnection(t *testing.T) {
	var connectionMu sync.Mutex
	newConnections := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("h", 128))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connectionMu.Lock()
			newConnections++
			connectionMu.Unlock()
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	client := server.Client()
	t.Cleanup(client.CloseIdleConnections)
	provider := relay.NewSub2apiProvider(client, server.URL, "test-key", "test-model", zap.NewNop())
	for index := 0; index < 2; index++ {
		if err := provider.Ping(context.Background()); err != nil {
			t.Fatalf("Ping() call %d error = %v", index+1, err)
		}
	}

	connectionMu.Lock()
	gotConnections := newConnections
	connectionMu.Unlock()
	if gotConnections != 1 {
		t.Fatalf("new connections = %d, want 1 across two sequential pings", gotConnections)
	}
}

func TestPingUnreachable(t *testing.T) {
	// Create a server and immediately close it so the URL is unreachable.
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	client := srv.Client()
	srv.Close()

	p := relay.NewSub2apiProvider(client, url+"/v1", "key", "model", zap.NewNop())
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
		if r.Header.Get("X-API-Key") != "test-admin-key" {
			t.Errorf("expected admin API key in X-API-Key header")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected Authorization header to be empty, got %q", got)
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

func TestGetUserReturnsNilWhenNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "not found",
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.GetUser(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetUser() unexpected error: %v", err)
	}
	if user != nil {
		t.Fatalf("GetUser() user = %+v, want nil", user)
	}
}

func TestGetUserIncludesSubscribedGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "admin",
				"subscriptions": []any{
					map[string]any{
						"id":       101,
						"user_id":  1,
						"group_id": 6,
						"status":   "active",
						"group":    map[string]any{"id": 6, "name": "Group Alpha", "platform": "openai"},
					},
					map[string]any{
						"id":       102,
						"user_id":  1,
						"group_id": 10,
						"status":   "active",
						"group":    map[string]any{"id": 10, "name": "Group Delta", "platform": "gemini"},
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.GetUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
		{ID: 10, Name: "Group Delta", Platform: "gemini"},
	}, user.AllowedGroups); diff != "" {
		t.Fatalf("subscribed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestListAllowedGroupsForUserUsesActiveSubscriptions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "admin",
				"subscriptions": []any{
					map[string]any{
						"id":       201,
						"user_id":  1,
						"group_id": 5,
						"status":   "active",
						"group":    map[string]any{"id": 5, "name": "Group Gamma", "platform": "anthropic"},
					},
					map[string]any{
						"id":       202,
						"user_id":  1,
						"group_id": 6,
						"status":   "active",
						"group":    map[string]any{"id": 6, "name": "Group Alpha", "platform": "openai"},
					},
					map[string]any{
						"id":       203,
						"user_id":  1,
						"group_id": 7,
						"status":   "inactive",
						"group":    map[string]any{"id": 7, "name": "Inactive", "platform": "openai"},
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	lister, ok := p.(interface {
		ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error)
	})
	if !ok {
		t.Fatal("provider does not implement ListAllowedGroupsForUser")
	}
	groups, err := lister.ListAllowedGroupsForUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAllowedGroupsForUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 5, Name: "Group Gamma", Platform: "anthropic"},
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
	}, groups); diff != "" {
		t.Fatalf("subscribed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestListAllowedGroupsForUserFallsBackToAdminListWhenDetailOmitsSubscriptions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.org",
				"username": "",
				"role":     "admin",
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       2,
						"email":    "other@example.org",
						"username": "other",
						"role":     "user",
					},
					map[string]any{
						"id":       1,
						"email":    "alice@example.org",
						"username": "",
						"role":     "admin",
						"subscriptions": []any{
							map[string]any{
								"id":       301,
								"user_id":  1,
								"group_id": 5,
								"status":   "active",
								"group": map[string]any{
									"id":                5,
									"name":              "Group Gamma",
									"platform":          "anthropic",
									"subscription_type": "subscription",
								},
							},
						},
					},
				},
				"page":  1,
				"pages": 1,
			},
		})
	})

	p := newTestProvider(t, mux)
	groups, err := p.ListAllowedGroupsForUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAllowedGroupsForUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 5, Name: "Group Gamma", Platform: "anthropic", SubscriptionType: "subscription"},
	}, groups); diff != "" {
		t.Fatalf("allowed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestListAllowedGroupsForUserUsesAllowedGroupObjectsWithoutGroupList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice",
				"role":     "user",
				"allowed_groups": []any{
					map[string]any{
						"id":       6,
						"name":     "Group Alpha",
						"platform": "openai",
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	groups, err := p.ListAllowedGroupsForUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAllowedGroupsForUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
	}, groups); diff != "" {
		t.Fatalf("allowed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestListAllowedGroupsForUserUsesUserFactsAndGroupDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":             1,
				"email":          "alice@example.com",
				"username":       "alice",
				"role":           "user",
				"allowed_groups": []int64{6},
				"subscriptions": []any{
					map[string]any{
						"id":       201,
						"user_id":  1,
						"group_id": 5,
						"status":   "active",
						"group": map[string]any{
							"id":                5,
							"name":              "Group Gamma",
							"platform":          "anthropic",
							"subscription_type": "subscription",
						},
					},
					map[string]any{
						"id":       202,
						"user_id":  1,
						"group_id": 8,
						"status":   "inactive",
						"group":    map[string]any{"id": 8, "name": "Inactive", "platform": "openai"},
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":                6,
						"name":              "Group Alpha",
						"platform":          "openai",
						"status":            "active",
						"is_exclusive":      true,
						"subscription_type": "standard",
					},
					map[string]any{
						"id":                9,
						"name":              "Other",
						"platform":          "gemini",
						"status":            "active",
						"subscription_type": "standard",
					},
				},
				"page":  1,
				"pages": 1,
			},
		})
	})

	p := newTestProvider(t, mux)
	groups, err := p.ListAllowedGroupsForUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAllowedGroupsForUser() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 5, Name: "Group Gamma", Platform: "anthropic", SubscriptionType: "subscription"},
		{ID: 6, Name: "Group Alpha", Platform: "openai", IsExclusive: true, SubscriptionType: "standard"},
	}, groups); diff != "" {
		t.Fatalf("allowed groups mismatch (-want +got):\n%s", diff)
	}
}

func TestFindUserByEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		email := r.URL.Query().Get("search")
		if got := r.URL.Query().Get("email"); got != "" {
			t.Errorf("expected email query to be empty, got %q", got)
		}
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

func TestFindUserByEmailSuccessFalse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "permission denied",
			"data":    []any{},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.FindUserByEmail(context.Background(), "any@example.com")
	if err == nil {
		t.Fatal("FindUserByEmail() expected error for success=false, got nil")
	}
}

func TestFindUserByUsername(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-admin-key" {
			t.Errorf("expected admin API key in X-API-Key header")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected Authorization header to be empty, got %q", got)
		}

		username := r.URL.Query().Get("search")
		if got := r.URL.Query().Get("username"); got != "" {
			t.Errorf("expected username query to be empty, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if username == "missing" {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{
					"id":       11,
					"email":    "u@example.com",
					"username": username,
					"role":     "user",
				},
			},
		})
	})

	p := newTestProvider(t, mux)

	user, err := p.FindUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user == nil || user.ID != 11 || user.Username != "alice" {
		t.Fatalf("FindUserByUsername() unexpected user: %+v", user)
	}

	user, err = p.FindUserByUsername(context.Background(), "missing")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user != nil {
		t.Fatalf("FindUserByUsername() expected nil for not found, got %+v", user)
	}
}

func TestFindUserByUsernameSuccessFalse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "permission denied",
			"data":    []any{},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.FindUserByUsername(context.Background(), "alice")
	if err == nil {
		t.Fatal("FindUserByUsername() expected error for success=false, got nil")
	}
}

func TestListUsersFetchesAllAdminPages(t *testing.T) {
	mux := http.NewServeMux()
	var pages []string
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "200" {
			t.Fatalf("page_size = %q, want 200", r.URL.Query().Get("page_size"))
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{"id": 11, "email": "alice@example.com", "username": "alice", "role": "user"},
					},
					"page":      1,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{"id": 12, "email": "bob@example.org", "username": "bob", "role": "admin"},
					},
					"page":      2,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		default:
			t.Fatalf("unexpected page %q", page)
		}
	})

	p := newTestProvider(t, mux)
	lister, ok := p.(relay.UserDirectoryProvider)
	if !ok {
		t.Fatal("provider does not implement UserDirectoryProvider")
	}
	users, err := lister.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() unexpected error: %v", err)
	}
	if got, want := pages, []string{"1", "2"}; !cmp.Equal(got, want) {
		t.Fatalf("pages = %#v, want %#v", got, want)
	}
	if got, want := []int64{users[0].ID, users[1].ID}, []int64{11, 12}; !cmp.Equal(got, want) {
		t.Fatalf("user ids = %#v, want %#v", got, want)
	}
	if users[1].Role != "admin" {
		t.Fatalf("second user role = %q, want admin", users[1].Role)
	}
}

func TestProviderWideDirectoryContractUsesFixedQueryAndAuthoritativePages(t *testing.T) {
	pageBodies := map[string][]byte{
		"1": []byte(`{"success":true,"data":{"items":[{"id":11},{"id":12}],"page":1,"page_size":1000,"pages":2,"total":3}}`),
		"2": []byte(`{"success":true,"data":{"items":[{"id":13}],"page":2,"page_size":1000,"pages":2,"total":3}}`),
	}
	var pages []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if diff := cmp.Diff(map[string][]string{
			"page":                  {page},
			"page_size":             {"1000"},
			"include_subscriptions": {"false"},
			"sort_by":               {"id"},
			"sort_order":            {"asc"},
		}, map[string][]string(r.URL.Query())); diff != "" {
			t.Fatalf("directory query mismatch (-want +got):\n%s", diff)
		}
		body, ok := pageBodies[page]
		if !ok {
			t.Fatalf("unexpected page %q", page)
		}
		_, _ = w.Write(body)
	})

	provider := newTestProvider(t, mux)
	directory, ok := provider.(relay.ProviderWideTeamUsageProvider)
	if !ok {
		t.Fatal("provider does not implement ProviderWideTeamUsageProvider")
	}
	got, err := directory.GetProviderUserIDs(context.Background())
	if err != nil {
		t.Fatalf("GetProviderUserIDs() error = %v", err)
	}
	if diff := cmp.Diff([]int64{11, 12, 13}, got.UserIDs); diff != "" {
		t.Fatalf("provider user IDs mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"1", "2"}, pages); diff != "" {
		t.Fatalf("requested pages mismatch (-want +got):\n%s", diff)
	}
	if got.PageCount != 2 {
		t.Fatalf("page count = %d, want 2", got.PageCount)
	}
	wantBytes := int64(len(pageBodies["1"]) + len(pageBodies["2"]))
	if got.ResponseBytes != wantBytes {
		t.Fatalf("response bytes = %d, want %d", got.ResponseBytes, wantBytes)
	}
}

func TestProviderWideDirectoryContractRejectsInvalidPaginationAndIDs(t *testing.T) {
	tests := []struct {
		name  string
		pages map[string]string
	}{
		{
			name: "cross-page order",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":1,"page_size":1000,"pages":2,"total":2}}`,
				"2": `{"success":true,"data":{"items":[{"id":10}],"page":2,"page_size":1000,"pages":2,"total":2}}`,
			},
		},
		{
			name: "duplicate ID",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11},{"id":11}],"page":1,"page_size":1000,"pages":1,"total":2}}`,
			},
		},
		{
			name: "non-positive ID",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":0}],"page":1,"page_size":1000,"pages":1,"total":1}}`,
			},
		},
		{
			name: "response page mismatch",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":2,"page_size":1000,"pages":1,"total":1}}`,
			},
		},
		{
			name: "page size mismatch",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":1,"page_size":200,"pages":1,"total":1}}`,
			},
		},
		{
			name: "missing total pages",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":1,"page_size":1000,"total":1}}`,
			},
		},
		{
			name: "changing total",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":1,"page_size":1000,"pages":2,"total":2}}`,
				"2": `{"success":true,"data":{"items":[{"id":12}],"page":2,"page_size":1000,"pages":2,"total":3}}`,
			},
		},
		{
			name: "empty nonterminal page",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[],"page":1,"page_size":1000,"pages":2,"total":1}}`,
			},
		},
		{
			name: "final count mismatch",
			pages: map[string]string{
				"1": `{"success":true,"data":{"items":[{"id":11}],"page":1,"page_size":1000,"pages":1,"total":2}}`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
				body, ok := test.pages[r.URL.Query().Get("page")]
				if !ok {
					t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
				}
				_, _ = io.WriteString(w, body)
			})
			provider := newTestProvider(t, mux)
			directory := provider.(relay.ProviderWideTeamUsageProvider)
			if _, err := directory.GetProviderUserIDs(context.Background()); err == nil {
				t.Fatalf("GetProviderUserIDs() error = nil, want %s rejection", test.name)
			}
		})
	}
}

func TestProviderWideDirectoryContractRejectsExactUserAndBodyBounds(t *testing.T) {
	t.Run("5000 users", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
			page, err := strconv.Atoi(r.URL.Query().Get("page"))
			if err != nil || page < 1 || page > 5 {
				t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
			}
			items := make([]map[string]int64, 1000)
			for index := range items {
				items[index] = map[string]int64{"id": int64((page-1)*1000 + index + 1)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
				"items": items, "page": page, "page_size": 1000, "pages": 5, "total": 5000,
			}})
		})
		provider := newTestProvider(t, mux)
		directory := provider.(relay.ProviderWideTeamUsageProvider)
		if _, err := directory.GetProviderUserIDs(context.Background()); err == nil {
			t.Fatal("GetProviderUserIDs() error = nil, want exact-5000 rejection")
		}
	})

	const directoryBodyLimit = 16 << 20
	const validEmptyDirectory = `{"success":true,"data":{"items":[],"page":1,"page_size":1000,"pages":1,"total":0}}`
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "below 16 MiB", size: directoryBodyLimit - 1},
		{name: "exactly 16 MiB", size: directoryBodyLimit, wantErr: true},
		{name: "over 16 MiB", size: directoryBodyLimit + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := paddedJSONBody(t, validEmptyDirectory, test.size)
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			})
			provider := newTestProvider(t, mux)
			directory := provider.(relay.ProviderWideTeamUsageProvider)
			got, err := directory.GetProviderUserIDs(context.Background())
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "16777216-byte limit") {
					t.Fatalf("GetProviderUserIDs() error = %v, want pre-decode body limit rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetProviderUserIDs() error = %v, want below-limit acceptance", err)
			}
			if got.ResponseBytes != int64(test.size) || got.PageCount != 1 || len(got.UserIDs) != 0 {
				t.Fatalf("directory result = %#v, want accepted %d-byte empty page", got, test.size)
			}
		})
	}
}

func TestProviderWideCurrentStatsContractUsesOneBoundedExactChunk(t *testing.T) {
	response := []byte(`{"success":true,"data":{"stats":{"101":{"user_id":101,"today_actual_cost":1.25,"total_actual_cost":10.5,"total_tokens":123},"102":{"user_id":102,"today_actual_cost":0,"total_actual_cost":0}}}}`)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload) != 1 {
			t.Fatalf("request fields = %v, want only user_ids", payload)
		}
		var ids []int64
		if err := json.Unmarshal(payload["user_ids"], &ids); err != nil {
			t.Fatalf("decode user_ids: %v", err)
		}
		if diff := cmp.Diff([]int64{101, 102}, ids); diff != "" {
			t.Fatalf("user_ids mismatch (-want +got):\n%s", diff)
		}
		_, _ = w.Write(response)
	})

	provider := newTestProvider(t, mux)
	statsProvider := provider.(relay.ProviderWideTeamUsageProvider)
	got, err := statsProvider.GetProviderCurrentUsageStats(context.Background(), []int64{101, 102})
	if err != nil {
		t.Fatalf("GetProviderCurrentUsageStats() error = %v", err)
	}
	if got.ResponseBytes != int64(len(response)) {
		t.Fatalf("response bytes = %d, want %d", got.ResponseBytes, len(response))
	}
	if len(got.Stats) != 2 || got.Stats[101].TotalActualCost != 10.5 || got.Stats[102].UserID != 102 {
		t.Fatalf("stats = %#v, want exact records for 101 and 102", got.Stats)
	}
}

func TestProviderWideCurrentStatsContractAcceptsExactly500RequestedIDs(t *testing.T) {
	requested := make([]int64, 500)
	stats := make(map[string]any, len(requested))
	for index := range requested {
		userID := int64(index + 1)
		requested[index] = userID
		stats[strconv.FormatInt(userID, 10)] = map[string]any{
			"user_id": userID, "today_actual_cost": float64(index), "total_actual_cost": float64(index + 1),
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			UserIDs []int64 `json:"user_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if diff := cmp.Diff(requested, payload.UserIDs); diff != "" {
			t.Fatalf("500-ID request mismatch (-want +got):\n%s", diff)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"stats": stats}})
	})
	provider := newTestProvider(t, mux)
	wide := provider.(relay.ProviderWideTeamUsageProvider)
	got, err := wide.GetProviderCurrentUsageStats(context.Background(), requested)
	if err != nil {
		t.Fatalf("GetProviderCurrentUsageStats() error = %v", err)
	}
	if len(got.Stats) != 500 {
		t.Fatalf("stats count = %d, want 500", len(got.Stats))
	}
}

func TestProviderWideCurrentStatsContractRejectsRequestAndCoverageViolations(t *testing.T) {
	validRecord := `{"user_id":101,"today_actual_cost":1,"total_actual_cost":2}`
	tests := []struct {
		name string
		ids  []int64
		body string
	}{
		{name: "empty request", ids: nil, body: `{}`},
		{name: "non-positive request", ids: []int64{0}, body: `{}`},
		{name: "duplicate request", ids: []int64{101, 101}, body: `{}`},
		{name: "missing record", ids: []int64{101, 102}, body: `{"success":true,"data":{"stats":{"101":` + validRecord + `}}}`},
		{name: "extra record", ids: []int64{101}, body: `{"success":true,"data":{"stats":{"101":` + validRecord + `,"102":{"user_id":102}}}}`},
		{name: "embedded ID mismatch", ids: []int64{101}, body: `{"success":true,"data":{"stats":{"101":{"user_id":102}}}}`},
		{name: "duplicate JSON key", ids: []int64{101}, body: `{"success":true,"data":{"stats":{"101":` + validRecord + `,"101":` + validRecord + `}}}`},
		{name: "negative cost", ids: []int64{101}, body: `{"success":true,"data":{"stats":{"101":{"user_id":101,"today_actual_cost":-1,"total_actual_cost":2}}}}`},
		{name: "negative tokens", ids: []int64{101}, body: `{"success":true,"data":{"stats":{"101":{"user_id":101,"today_actual_cost":1,"total_actual_cost":2,"total_tokens":-1}}}}`},
	}
	tooMany := make([]int64, 501)
	for index := range tooMany {
		tooMany[index] = int64(index + 1)
	}
	tests = append(tests, struct {
		name string
		ids  []int64
		body string
	}{name: "501 requested IDs", ids: tooMany, body: `{}`})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_, _ = io.WriteString(w, test.body)
			})
			provider := newTestProvider(t, mux)
			statsProvider := provider.(relay.ProviderWideTeamUsageProvider)
			if _, err := statsProvider.GetProviderCurrentUsageStats(context.Background(), test.ids); err == nil {
				t.Fatalf("GetProviderCurrentUsageStats() error = nil, want %s rejection", test.name)
			}
			if (test.name == "empty request" || test.name == "non-positive request" || test.name == "duplicate request" || test.name == "501 requested IDs") && calls != 0 {
				t.Fatalf("HTTP calls = %d, want validation before request", calls)
			}
		})
	}
}

func TestProviderWideCurrentStatsContractEnforcesBodyLimitBeforeDecode(t *testing.T) {
	const statsBodyLimit = 2 << 20
	const validStats = `{"success":true,"data":{"stats":{"101":{"user_id":101,"today_actual_cost":1,"total_actual_cost":2}}}}`
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "below 2 MiB", size: statsBodyLimit - 1},
		{name: "exactly 2 MiB", size: statsBodyLimit, wantErr: true},
		{name: "over 2 MiB", size: statsBodyLimit + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := paddedJSONBody(t, validStats, test.size)
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			})
			provider := newTestProvider(t, mux)
			statsProvider := provider.(relay.ProviderWideTeamUsageProvider)
			got, err := statsProvider.GetProviderCurrentUsageStats(context.Background(), []int64{101})
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "2097152-byte limit") {
					t.Fatalf("GetProviderCurrentUsageStats() error = %v, want pre-decode body limit rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetProviderCurrentUsageStats() error = %v, want below-limit acceptance", err)
			}
			if got.ResponseBytes != int64(test.size) || len(got.Stats) != 1 {
				t.Fatalf("stats result = %#v, want accepted %d-byte response", got, test.size)
			}
		})
	}
}

func TestProviderWideUsageSourcesDoNotExposeUpstreamIdentityMessages(t *testing.T) {
	const upstreamMessage = "alice@example.com secret response text"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": upstreamMessage})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": upstreamMessage})
	})
	provider := newTestProvider(t, mux)
	wide := provider.(relay.ProviderWideTeamUsageProvider)

	_, directoryErr := wide.GetProviderUserIDs(context.Background())
	_, statsErr := wide.GetProviderCurrentUsageStats(context.Background(), []int64{101})
	for name, err := range map[string]error{"directory": directoryErr, "stats": statsErr} {
		if err == nil {
			t.Fatalf("%s error = nil, want failed envelope rejection", name)
		}
		if strings.Contains(err.Error(), upstreamMessage) || strings.Contains(err.Error(), "alice@example.com") {
			t.Fatalf("%s error exposed upstream identity text: %v", name, err)
		}
	}
}

func TestCreateUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-admin-key" {
			t.Errorf("expected admin API key in X-API-Key header")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected Authorization header to be empty, got %q", got)
		}

		var body relay.CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode create user body: %v", err)
		}
		if body.Username != "newuser" || body.Email != "newuser@example.com" || body.Password != "pw" || body.Concurrency != 5 {
			t.Fatalf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":       123,
				"email":    body.Email,
				"username": body.Username,
				"role":     "user",
			},
		})
	})

	p := newTestProvider(t, mux)
	u, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username:    "newuser",
		Email:       "newuser@example.com",
		Password:    "pw",
		Notes:       "test",
		Concurrency: 5,
	})
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}
	if u == nil || u.ID != 123 || u.Username != "newuser" {
		t.Fatalf("CreateUser() unexpected user: %+v", u)
	}
}

func TestCreateUserAssignsDefaultSubscriptionsFromRelaySettings(t *testing.T) {
	var assignBodies []map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       123,
				"email":    "newuser@example.com",
				"username": "newuser",
				"role":     "user",
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"default_subscriptions": []any{
					map[string]any{"group_id": 5, "validity_days": 30},
					map[string]any{"group_id": 6, "validity_days": 60},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode assign body: %v", err)
		}
		assignBodies = append(assignBodies, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": len(assignBodies), "status": "active"},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username:    "newuser",
		Email:       "newuser@example.com",
		Password:    "pw",
		Notes:       "test",
		Concurrency: 5,
	})
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}
	if len(assignBodies) != 2 {
		t.Fatalf("assigned subscriptions = %d, want 2", len(assignBodies))
	}
	if assignBodies[0]["user_id"] != float64(123) || assignBodies[0]["group_id"] != float64(5) || assignBodies[0]["validity_days"] != float64(30) {
		t.Fatalf("unexpected first assign body: %+v", assignBodies[0])
	}
	if assignBodies[1]["user_id"] != float64(123) || assignBodies[1]["group_id"] != float64(6) || assignBodies[1]["validity_days"] != float64(60) {
		t.Fatalf("unexpected second assign body: %+v", assignBodies[1])
	}
}

func TestCreateUserSkipsDefaultSubscriptionAlreadyAssignedByRelay(t *testing.T) {
	var assignCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       123,
				"email":    "newuser@example.com",
				"username": "newuser",
				"role":     "user",
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"default_subscriptions": []any{
					map[string]any{"group_id": 5, "validity_days": 365},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/users/123/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []any{
				map[string]any{
					"id":       77,
					"user_id":  123,
					"group_id": 5,
					"status":   "active",
					"notes":    "auto assigned by default user subscriptions setting",
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		assignCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusConflict,
			"message": "subscription exists but request conflicts with existing assignment semantics",
			"reason":  "SUBSCRIPTION_ASSIGN_CONFLICT",
			"metadata": map[string]string{
				"conflict_reason": "notes_mismatch",
			},
		})
	})

	p := newTestProvider(t, mux)
	u, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username:    "newuser",
		Email:       "newuser@example.com",
		Password:    "pw",
		Notes:       "test",
		Concurrency: 5,
	})
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}
	if u == nil || u.ID != 123 || u.Username != "newuser" {
		t.Fatalf("CreateUser() unexpected user: %+v", u)
	}
	if assignCalls != 0 {
		t.Fatalf("assign calls = %d, want 0", assignCalls)
	}
}

func TestCreateUserTreatsExistingDefaultSubscriptionConflictAsAssigned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       123,
				"email":    "newuser@example.com",
				"username": "newuser",
				"role":     "user",
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"default_subscriptions": []any{
					map[string]any{"group_id": 5, "validity_days": 30},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusConflict,
			"message": "subscription already exists",
			"reason":  "SUBSCRIPTION_ALREADY_EXISTS",
		})
	})

	p := newTestProvider(t, mux)
	u, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username:    "newuser",
		Email:       "newuser@example.com",
		Password:    "pw",
		Notes:       "test",
		Concurrency: 5,
	})
	if err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}
	if u == nil || u.ID != 123 || u.Username != "newuser" {
		t.Fatalf("CreateUser() unexpected user: %+v", u)
	}
}

func TestCreateUserKeepsUnrelatedDefaultSubscriptionConflictFatal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       123,
				"email":    "newuser@example.com",
				"username": "newuser",
				"role":     "user",
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"default_subscriptions": []any{
					map[string]any{"group_id": 5, "validity_days": 30},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusConflict,
			"message": "subscription exists but group cannot accept assignment",
			"reason":  "GROUP_ASSIGNMENT_CONFLICT",
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username:    "newuser",
		Email:       "newuser@example.com",
		Password:    "pw",
		Notes:       "test",
		Concurrency: 5,
	})
	if err == nil {
		t.Fatal("CreateUser() expected error for unrelated subscription conflict, got nil")
	}
}

func TestCreateUserSuccessFalse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "validation error",
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.CreateUser(context.Background(), relay.CreateUserRequest{
		Username: "newuser",
		Email:    "newuser@example.com",
		Password: "pw",
	})
	if err == nil {
		t.Fatal("CreateUser() expected error for success=false, got nil")
	}
}

func TestAssignSubscriptionForUserPostsSelectedGroup(t *testing.T) {
	var assignBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&assignBody); err != nil {
			t.Fatalf("decode assign body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 77, "status": "active"},
		})
	})

	p := newTestProvider(t, mux)
	assigner, ok := p.(interface {
		AssignSubscriptionForUser(context.Context, int64, int64, int) error
	})
	if !ok {
		t.Fatal("provider does not implement AssignSubscriptionForUser")
	}
	if err := assigner.AssignSubscriptionForUser(context.Background(), 42, 5, 60); err != nil {
		t.Fatalf("AssignSubscriptionForUser() unexpected error: %v", err)
	}
	if assignBody["user_id"] != float64(42) || assignBody["group_id"] != float64(5) || assignBody["validity_days"] != float64(60) {
		t.Fatalf("unexpected assign body: %+v", assignBody)
	}
	if assignBody["notes"] != "assigned by ai-efficiency admin" {
		t.Fatalf("notes = %v, want admin assignment note", assignBody["notes"])
	}
}

func TestAssignSubscriptionForUserKeepsSemanticConflictFatal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusConflict,
			"message": "subscription exists but request conflicts with existing assignment semantics",
			"reason":  "SUBSCRIPTION_ASSIGN_CONFLICT",
			"metadata": map[string]string{
				"conflict_reason": "validity_days_mismatch",
			},
		})
	})

	p := newTestProvider(t, mux)
	assigner, ok := p.(interface {
		AssignSubscriptionForUser(context.Context, int64, int64, int) error
	})
	if !ok {
		t.Fatal("provider does not implement AssignSubscriptionForUser")
	}
	err := assigner.AssignSubscriptionForUser(context.Background(), 42, 5, 60)
	if err == nil {
		t.Fatal("AssignSubscriptionForUser() expected semantic conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "validity_days_mismatch") {
		t.Fatalf("AssignSubscriptionForUser() error = %v, want conflict reason", err)
	}
}

func TestExtendSubscriptionForUserFindsExistingSubscriptionAndPostsDays(t *testing.T) {
	var extendBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{
				{"id": 77, "user_id": 42, "group_id": 5, "status": "active"},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/77/extend", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&extendBody); err != nil {
			t.Fatalf("decode extend body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 77, "status": "active"},
		})
	})

	p := newTestProvider(t, mux)
	manager, ok := p.(interface {
		ExtendSubscriptionForUser(context.Context, int64, int64, int) error
	})
	if !ok {
		t.Fatal("provider does not implement ExtendSubscriptionForUser")
	}
	if err := manager.ExtendSubscriptionForUser(context.Background(), 42, 5, 7); err != nil {
		t.Fatalf("ExtendSubscriptionForUser() unexpected error: %v", err)
	}
	if extendBody["days"] != float64(7) {
		t.Fatalf("unexpected extend body: %+v", extendBody)
	}
}

func TestRemoveSubscriptionForUserFindsExistingSubscriptionAndDeletes(t *testing.T) {
	deleted := false

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{
				{"id": 77, "user_id": 42, "group_id": 5, "status": "active"},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		deleted = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message": "Subscription revoked successfully"},
		})
	})

	p := newTestProvider(t, mux)
	manager, ok := p.(interface {
		RemoveSubscriptionForUser(context.Context, int64, int64) error
	})
	if !ok {
		t.Fatal("provider does not implement RemoveSubscriptionForUser")
	}
	if err := manager.RemoveSubscriptionForUser(context.Background(), 42, 5); err != nil {
		t.Fatalf("RemoveSubscriptionForUser() unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("expected subscription delete request")
	}
}

func TestResetSubscriptionQuotaForUserFindsExistingSubscriptionAndPostsAllWindows(t *testing.T) {
	var resetBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{
				{
					"id":                77,
					"user_id":           42,
					"group_id":          5,
					"status":            "active",
					"daily_usage_usd":   12.5,
					"weekly_usage_usd":  40.0,
					"monthly_usage_usd": 88.0,
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/77/reset-quota", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&resetBody); err != nil {
			t.Fatalf("decode reset body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 77, "status": "active"},
		})
	})

	p := newTestProvider(t, mux)
	manager, ok := p.(interface {
		ResetSubscriptionQuotaForUser(context.Context, int64, int64) error
	})
	if !ok {
		t.Fatal("provider does not implement ResetSubscriptionQuotaForUser")
	}
	if err := manager.ResetSubscriptionQuotaForUser(context.Background(), 42, 5); err != nil {
		t.Fatalf("ResetSubscriptionQuotaForUser() unexpected error: %v", err)
	}
	if resetBody["daily"] != true || resetBody["weekly"] != true || resetBody["monthly"] != true {
		t.Fatalf("unexpected reset body: %+v", resetBody)
	}
}

func TestExtendSubscriptionForUserReturnsNotFoundForMissingGroupSubscription(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/42/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{
				{"id": 77, "user_id": 42, "group_id": 6, "status": "active"},
			},
		})
	})

	p := newTestProvider(t, mux)
	manager, ok := p.(interface {
		ExtendSubscriptionForUser(context.Context, int64, int64, int) error
	})
	if !ok {
		t.Fatal("provider does not implement ExtendSubscriptionForUser")
	}
	err := manager.ExtendSubscriptionForUser(context.Background(), 42, 5, 7)
	if err == nil || !strings.Contains(err.Error(), "subscription not found") {
		t.Fatalf("ExtendSubscriptionForUser() error = %v, want subscription not found", err)
	}
}

func TestUpdateUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-admin-key" {
			t.Errorf("expected admin API key in X-API-Key header")
		}
		var body relay.UpdateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode update user body: %v", err)
		}
		if body.Password != "ldap-pass" {
			t.Fatalf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":       7,
				"email":    "alice@example.com",
				"username": "alice",
				"role":     "user",
			},
		})
	})

	p := newTestProvider(t, mux)
	updater, ok := p.(interface {
		UpdateUser(context.Context, int64, relay.UpdateUserRequest) (*relay.User, error)
	})
	if !ok {
		t.Fatal("provider does not support UpdateUser")
	}

	u, err := updater.UpdateUser(context.Background(), 7, relay.UpdateUserRequest{Password: "ldap-pass"})
	if err != nil {
		t.Fatalf("UpdateUser() unexpected error: %v", err)
	}
	if u == nil || u.ID != 7 || u.Username != "alice" {
		t.Fatalf("UpdateUser() unexpected user: %+v", u)
	}
}

func TestUpdateUserIncludesEnvelopeErrorMessage(t *testing.T) {
	tests := []struct {
		name      string
		envelope  map[string]any
		wantError string
	}{
		{
			name: "top-level message",
			envelope: map[string]any{
				"code":    500,
				"message": "cannot disable admin user",
			},
			wantError: "relay: update user: request failed: cannot disable admin user",
		},
		{
			name: "nested error message",
			envelope: map[string]any{
				"code": 500,
				"error": map[string]any{
					"message": "relay account is locked",
				},
			},
			wantError: "relay: update user: request failed: relay account is locked",
		},
		{
			name: "top-level and nested error messages",
			envelope: map[string]any{
				"code":    500,
				"message": "cannot update relay user",
				"error": map[string]any{
					"message": "relay account is locked",
				},
			},
			wantError: "relay: update user: request failed: cannot update relay user: relay account is locked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/users/7", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.envelope)
			})

			p := newTestProvider(t, mux)
			updater, ok := p.(interface {
				UpdateUser(context.Context, int64, relay.UpdateUserRequest) (*relay.User, error)
			})
			if !ok {
				t.Fatal("provider does not support UpdateUser")
			}

			_, err := updater.UpdateUser(context.Background(), 7, relay.UpdateUserRequest{Status: "disabled"})
			if err == nil {
				t.Fatal("UpdateUser() error = nil, want envelope error")
			}
			if got := err.Error(); got != tt.wantError {
				t.Fatalf("UpdateUser() error = %q, want %q", got, tt.wantError)
			}
		})
	}
}

func TestDisableUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"id": 7, "email": "alice@example.com", "username": "alice", "role": "user"},
			})
			return
		}
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-admin-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		var body relay.UpdateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Status != "disabled" {
			t.Fatalf("status = %q, want disabled", body.Status)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 7, "email": "alice@example.com", "username": "alice", "role": "user"},
		})
	})
	p := newTestProvider(t, mux)
	disabler, ok := p.(relay.UserDisabler)
	if !ok {
		t.Fatal("provider does not support UserDisabler")
	}
	if err := disabler.DisableUser(context.Background(), 7); err != nil {
		t.Fatalf("DisableUser() unexpected error: %v", err)
	}
}

func TestDisableUserIncludesUpstreamErrorMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"id": 7, "email": "alice@example.com", "username": "alice", "role": "user"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusInternalServerError,
			"message": "cannot disable admin user",
		})
	})
	p := newTestProvider(t, mux)
	disabler, ok := p.(relay.UserDisabler)
	if !ok {
		t.Fatal("provider does not support UserDisabler")
	}
	err := disabler.DisableUser(context.Background(), 7)
	if err == nil {
		t.Fatal("DisableUser() error = nil, want upstream error")
	}
	if got, want := err.Error(), "relay: disable user: relay: update user: unexpected status 500: cannot disable admin user"; got != want {
		t.Fatalf("DisableUser() error = %q, want %q", got, want)
	}
}

func TestDisableUserRejectsRelayAdminBeforeUpdate(t *testing.T) {
	updateCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			updateCalled = true
			t.Fatal("DisableUser sent update request for admin relay user")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 7, "email": "admin@example.com", "username": "admin", "role": "admin"},
		})
	})
	p := newTestProvider(t, mux)
	disabler, ok := p.(relay.UserDisabler)
	if !ok {
		t.Fatal("provider does not support UserDisabler")
	}
	err := disabler.DisableUser(context.Background(), 7)
	if err == nil {
		t.Fatal("DisableUser() error = nil, want admin rejection")
	}
	if got, want := err.Error(), "relay: disable user: cannot disable admin relay user"; got != want {
		t.Fatalf("DisableUser() error = %q, want %q", got, want)
	}
	if updateCalled {
		t.Fatal("DisableUser sent update request for admin relay user")
	}
}

func TestChatCompletion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Errorf("expected relay key in Authorization header, got %q", r.Header.Get("Authorization"))
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
	if setter, ok := p.(interface{ SetAdminAPIKey(string) }); ok {
		setter.SetAdminAPIKey("test-admin-key")
	} else {
		t.Fatal("provider does not support SetAdminAPIKey")
	}
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

func TestChatCompletionIncludesErrorMessageForNonOKResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid API key",
				"type":    "authentication_error",
			},
		})
	})

	p := newTestProvider(t, mux)
	_, err := p.ChatCompletion(context.Background(), relay.ChatCompletionRequest{
		Messages: []relay.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("ChatCompletion() expected error")
	}
	if got, want := err.Error(), "relay: chat completion: unexpected status 401: invalid API key"; got != want {
		t.Fatalf("ChatCompletion() error = %q, want %q", got, want)
	}
}

func TestChatCompletionWithTools(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Fatalf("expected relay key in Authorization header, got %q", r.Header.Get("Authorization"))
		}
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
	if setter, ok := p.(interface{ SetAdminAPIKey(string) }); ok {
		setter.SetAdminAPIKey("test-admin-key")
	} else {
		t.Fatal("provider does not support SetAdminAPIKey")
	}
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

func TestListUserAPIKeysDecodesGroupPlatformAndLastUsed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{
				map[string]any{
					"id":           1,
					"user_id":      7,
					"key":          "sk-existing-openai",
					"name":         "alice",
					"status":       "active",
					"quota":        100.0,
					"quota_used":   32.4,
					"created_at":   "2026-04-07T10:00:00Z",
					"last_used_at": "2026-04-07T11:00:00Z",
					"group": map[string]any{
						"id":                42,
						"name":              "Group Alpha",
						"platform":          "openai",
						"daily_limit_usd":   10.0,
						"weekly_limit_usd":  50.0,
						"monthly_limit_usd": 100.0,
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	keys, err := p.ListUserAPIKeys(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListUserAPIKeys() unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].Key != "sk-existing-openai" {
		t.Fatalf("key secret = %q, want %q", keys[0].Key, "sk-existing-openai")
	}
	if keys[0].Group == nil || keys[0].Group.Platform != "openai" {
		t.Fatalf("group platform = %+v, want openai", keys[0].Group)
	}
	if keys[0].Quota != 100 || keys[0].QuotaUsed != 32.4 {
		t.Fatalf("quota fields = quota:%v quota_used:%v, want 100 / 32.4", keys[0].Quota, keys[0].QuotaUsed)
	}
	if keys[0].Group.Name != "Group Alpha" {
		t.Fatalf("group name = %q, want Group Alpha", keys[0].Group.Name)
	}
	if keys[0].Group.MonthlyLimitUSD == nil || *keys[0].Group.MonthlyLimitUSD != 100 {
		t.Fatalf("monthly_limit_usd = %+v, want 100", keys[0].Group.MonthlyLimitUSD)
	}
	if keys[0].LastUsedAt == nil || keys[0].LastUsedAt.IsZero() {
		t.Fatalf("expected last_used_at to be decoded")
	}
}

func TestListUserAPIKeysDecodesPaginatedItemsEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":      1,
						"user_id": 7,
						"name":    "alice",
						"status":  "active",
						"group": map[string]any{
							"id":       42,
							"platform": "anthropic",
						},
					},
				},
				"total":     1,
				"page":      1,
				"page_size": 20,
				"pages":     1,
			},
		})
	})

	p := newTestProvider(t, mux)
	keys, err := p.ListUserAPIKeys(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListUserAPIKeys() unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].Name != "alice" {
		t.Fatalf("name = %q, want %q", keys[0].Name, "alice")
	}
	if keys[0].Group == nil || keys[0].Group.Platform != "anthropic" {
		t.Fatalf("group platform = %+v, want anthropic", keys[0].Group)
	}
}

func TestListUserAPIKeysFetchesAllPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":      101,
							"user_id": 7,
							"name":    "newer",
							"status":  "active",
							"group": map[string]any{
								"id":       6,
								"platform": "openai",
							},
						},
					},
					"total":     2,
					"page":      1,
					"page_size": 1,
					"pages":     2,
				},
			})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":      2,
							"user_id": 7,
							"name":    "older",
							"status":  "inactive",
							"group": map[string]any{
								"id":       5,
								"platform": "anthropic",
							},
						},
					},
					"total":     2,
					"page":      2,
					"page_size": 1,
					"pages":     2,
				},
			})
		default:
			t.Fatalf("unexpected page query: %q", page)
		}
	})

	p := newTestProvider(t, mux)
	keys, err := p.ListUserAPIKeys(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListUserAPIKeys() unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
	if keys[0].ID != 101 || keys[1].ID != 2 {
		t.Fatalf("keys = %+v, want ids [101 2]", keys)
	}
}

func TestFindUserByUsernameAcceptsCodeEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": []any{
				map[string]any{
					"id":       10,
					"email":    "alice@example.com",
					"username": "alice",
					"role":     "user",
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user == nil || user.Username != "alice" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByUsernameAcceptsPaginatedEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       12,
						"email":    "alice@example.com",
						"username": "alice",
						"role":     "user",
					},
				},
				"page":      1,
				"page_size": 1,
				"pages":     1,
				"total":     1,
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user == nil || user.Username != "alice" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByEmailAcceptsPaginatedEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       13,
						"email":    "alice@example.com",
						"username": "alice",
						"role":     "user",
					},
				},
				"page":      1,
				"page_size": 1,
				"pages":     1,
				"total":     1,
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() unexpected error: %v", err)
	}
	if user == nil || user.Email != "alice@example.com" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByUsernameRequiresExactMatchInPaginatedEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       15,
						"email":    "bob@example.com",
						"username": "",
						"role":     "user",
					},
					map[string]any{
						"id":       21,
						"email":    "carol@example.com",
						"username": "carol@example.com",
						"role":     "user",
					},
				},
				"page":      1,
				"page_size": 20,
				"pages":     1,
				"total":     2,
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByUsername(context.Background(), "carol@example.com")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user == nil || user.ID != 21 {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByUsernameReturnsNilWhenPaginatedEnvelopeHasNoExactMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       15,
						"email":    "bob@example.com",
						"username": "",
						"role":     "user",
					},
				},
				"page":      1,
				"page_size": 20,
				"pages":     1,
				"total":     1,
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByUsername(context.Background(), "carol@example.com")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user != nil {
		t.Fatalf("expected nil for no exact match, got %+v", user)
	}
}

func TestFindUserByEmailRequiresExactMatchInPaginatedEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"id":       15,
						"email":    "bob@example.com",
						"username": "",
						"role":     "user",
					},
					map[string]any{
						"id":       21,
						"email":    "carol@example.com",
						"username": "",
						"role":     "user",
					},
				},
				"page":      1,
				"page_size": 20,
				"pages":     1,
				"total":     2,
			},
		})
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByEmail(context.Background(), "carol@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() unexpected error: %v", err)
	}
	if user == nil || user.ID != 21 {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByEmailFallsBackToFullAdminListWhenFilteredLookupMissesExactMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		email := r.URL.Query().Get("search")
		page := r.URL.Query().Get("page")

		switch {
		case email == "carol@example.com":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       15,
							"email":    "bob@example.com",
							"username": "bob",
							"role":     "user",
						},
					},
					"page":      1,
					"page_size": 1,
					"pages":     2,
					"total":     2,
				},
			})
		case page == "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       15,
							"email":    "bob@example.com",
							"username": "bob",
							"role":     "user",
						},
					},
					"page":      1,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		case page == "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       21,
							"email":    "carol@example.com",
							"username": "carol",
							"role":     "admin",
						},
					},
					"page":      2,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		default:
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByEmail(context.Background(), "carol@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() unexpected error: %v", err)
	}
	if user == nil || user.ID != 21 || user.Role != "admin" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestFindUserByUsernameFallsBackToFullAdminListWhenFilteredLookupMissesExactMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		username := r.URL.Query().Get("search")
		page := r.URL.Query().Get("page")

		switch {
		case username == "carol":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       15,
							"email":    "bob@example.com",
							"username": "bob",
							"role":     "user",
						},
					},
					"page":      1,
					"page_size": 1,
					"pages":     2,
					"total":     2,
				},
			})
		case page == "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       15,
							"email":    "bob@example.com",
							"username": "bob",
							"role":     "user",
						},
					},
					"page":      1,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		case page == "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":       21,
							"email":    "carol@example.com",
							"username": "carol",
							"role":     "admin",
						},
					},
					"page":      2,
					"page_size": 200,
					"pages":     2,
					"total":     2,
				},
			})
		default:
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
	})

	p := newTestProvider(t, mux)
	user, err := p.FindUserByUsername(context.Background(), "carol")
	if err != nil {
		t.Fatalf("FindUserByUsername() unexpected error: %v", err)
	}
	if user == nil || user.ID != 21 || user.Role != "admin" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestCreateUserAPIKeyRequiresUserCredentials(t *testing.T) {
	p := newTestProvider(t, http.NewServeMux())
	_, err := p.CreateUserAPIKey(context.Background(), 3, relay.APIKeyCreateRequest{Name: "my-key"})
	if err == nil {
		t.Fatal("CreateUserAPIKey() expected error without user credentials")
	}
	if got, want := err.Error(), "relay: create api key: user credentials are required"; got != want {
		t.Fatalf("CreateUserAPIKey() error = %q, want %q", got, want)
	}
}

func TestUpdateUserAPIKeyStatusWithJWT(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "session-token-123"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "",
				"role":     "admin",
			},
		})
	})
	mux.HandleFunc("/api/v1/keys/2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token-123" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer session-token-123")
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Status != "active" {
			t.Fatalf("status = %q, want %q", body.Status, "active")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
		})
	})

	p := newTestProvider(t, mux)
	ctx := relay.WithUserCredentials(context.Background(), "alice@example.com", "secret")
	if err := p.UpdateUserAPIKeyStatus(ctx, 2, "active"); err != nil {
		t.Fatalf("UpdateUserAPIKeyStatus() unexpected error: %v", err)
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

func TestListModelsForPlatformUsesGeminiNativeEndpoint(t *testing.T) {
	var gotAuth string
	var gotGoogleKey string

	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotGoogleKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []any{
				map[string]any{
					"name":        "models/gemini-2.5-flash",
					"displayName": "Gemini 2.5 Flash",
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := relay.NewSub2apiProvider(srv.Client(), srv.URL+"/v1", "sk-user-gemini", "", zap.NewNop())
	lister, ok := p.(relay.PlatformModelLister)
	if !ok {
		t.Fatal("provider does not implement PlatformModelLister")
	}

	models, err := lister.ListModelsForPlatform(context.Background(), "gemini")
	if err != nil {
		t.Fatalf("ListModelsForPlatform() unexpected error: %v", err)
	}
	if gotAuth != "Bearer sk-user-gemini" {
		t.Fatalf("Authorization = %q, want user key", gotAuth)
	}
	if gotGoogleKey != "sk-user-gemini" {
		t.Fatalf("x-goog-api-key = %q, want user key", gotGoogleKey)
	}
	if len(models) != 1 || models[0].ID != "gemini-2.5-flash" || models[0].DisplayName != "Gemini 2.5 Flash" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestListModelsForPlatformUsesV1ModelsForOpenAI(t *testing.T) {
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{
					"id":           "gpt-5.4",
					"display_name": "GPT-5.4",
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := relay.NewSub2apiProvider(srv.Client(), srv.URL, "sk-user-openai", "", zap.NewNop())
	lister, ok := p.(relay.PlatformModelLister)
	if !ok {
		t.Fatal("provider does not implement PlatformModelLister")
	}

	models, err := lister.ListModelsForPlatform(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ListModelsForPlatform() unexpected error: %v", err)
	}
	if gotAuth != "Bearer sk-user-openai" {
		t.Fatalf("Authorization = %q, want user key", gotAuth)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.4" || models[0].DisplayName != "GPT-5.4" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestCreateUserAPIKeyWithExpiryAndGroup(t *testing.T) {
	exp := time.Date(2026, 3, 31, 1, 2, 3, 0, time.UTC)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "user-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       3,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "user",
			},
		})
	})
	mux.HandleFunc("/api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer user-jwt-token" {
			t.Fatalf("expected user jwt token, got %q", r.Header.Get("Authorization"))
		}

		var body struct {
			Name          string `json:"name"`
			ExpiresInDays int    `json:"expires_in_days"`
			GroupID       int64  `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.Name != "my-key" || body.ExpiresInDays != 1 || body.GroupID != 6 {
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
				"key":     "sk-abc123",
			},
		})
	})

	p := newTestProvider(t, mux)
	ctx := relay.WithUserCredentials(context.Background(), "alice@example.com", "test-password")
	key, err := p.CreateUserAPIKey(ctx, 3, relay.APIKeyCreateRequest{
		Name:      "my-key",
		ExpiresAt: &exp,
		GroupID:   "6",
	})
	if err != nil {
		t.Fatalf("CreateUserAPIKey() unexpected error: %v", err)
	}
	if key.ID != 99 || key.Secret != "sk-abc123" || key.Name != "my-key" {
		t.Fatalf("unexpected key: %+v", key)
	}
}

func TestCreateUserAPIKeyWithJWTUserFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body["email"] != "alice@example.com" || body["password"] != "test-password" {
			t.Fatalf("unexpected login body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "user-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-jwt-token" {
			t.Fatalf("expected user jwt token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":       1,
				"email":    "alice@example.com",
				"username": "alice@example.com",
				"role":     "admin",
			},
		})
	})
	mux.HandleFunc("/api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-jwt-token" {
			t.Fatalf("expected user jwt token for api key create, got %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		if body["name"] != "jwt-key" || body["group_id"] != float64(6) {
			t.Fatalf("unexpected create body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"id":      99,
				"user_id": 1,
				"name":    "jwt-key",
				"status":  "active",
				"key":     "sk-user-jwt-key",
			},
		})
	})

	p := newTestProvider(t, mux)
	ctx := relay.WithUserCredentials(context.Background(), "alice@example.com", "test-password")
	key, err := p.CreateUserAPIKey(ctx, 1, relay.APIKeyCreateRequest{
		Name:    "jwt-key",
		GroupID: "6",
	})
	if err != nil {
		t.Fatalf("CreateUserAPIKey() unexpected error: %v", err)
	}
	if key.ID != 99 || key.UserID != 1 || key.Name != "jwt-key" || key.Secret != "sk-user-jwt-key" {
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

func TestResolveDefaultGroupIDUsesLargestActiveAccountCount(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-admin-key" {
			t.Fatalf("expected X-API-Key header, got %q", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{
					map[string]any{"id": 5, "name": "Anthropic", "status": "active", "account_count": 1, "active_account_count": 1},
					map[string]any{"id": 6, "name": "OpenAI", "status": "active", "account_count": 14, "active_account_count": 13},
					map[string]any{"id": 9, "name": "Paused", "status": "inactive", "account_count": 99, "active_account_count": 0},
				},
				"page":      1,
				"page_size": 200,
				"pages":     1,
				"total":     3,
			},
		})
	})

	p := newTestProvider(t, mux)
	resolver, ok := p.(interface {
		ResolveDefaultGroupID(context.Context) (string, error)
	})
	if !ok {
		t.Fatal("provider does not implement ResolveDefaultGroupID")
	}
	groupID, err := resolver.ResolveDefaultGroupID(context.Background())
	if err != nil {
		t.Fatalf("ResolveDefaultGroupID() unexpected error: %v", err)
	}
	if groupID != "6" {
		t.Fatalf("groupID = %q, want %q", groupID, "6")
	}
}

func TestResolveDefaultGroupIDForPlatformFiltersByPlatform(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{
					map[string]any{"id": 5, "platform": "anthropic", "status": "active", "account_count": 4, "active_account_count": 3},
					map[string]any{"id": 6, "platform": "openai", "status": "active", "account_count": 14, "active_account_count": 13},
					map[string]any{"id": 7, "platform": "anthropic", "status": "active", "account_count": 10, "active_account_count": 9},
				},
				"page":  1,
				"pages": 1,
			},
		})
	})

	p := newTestProvider(t, mux)
	resolver, ok := p.(interface {
		ResolveDefaultGroupIDForPlatform(context.Context, string) (string, error)
	})
	if !ok {
		t.Fatal("provider does not implement ResolveDefaultGroupIDForPlatform")
	}
	groupID, err := resolver.ResolveDefaultGroupIDForPlatform(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("ResolveDefaultGroupIDForPlatform() unexpected error: %v", err)
	}
	if groupID != "7" {
		t.Fatalf("groupID = %q, want %q", groupID, "7")
	}
}

func TestListPlatformGroupsReturnsActivePlatformSummaries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{
					map[string]any{"id": 5, "name": "Group Gamma", "platform": "anthropic", "status": "active", "account_count": 4, "active_account_count": 3},
					map[string]any{"id": 6, "name": "Group Alpha", "platform": "openai", "status": "active", "account_count": 14, "active_account_count": 13},
					map[string]any{"id": 7, "name": "Disabled", "platform": "gemini", "status": "disabled", "account_count": 1, "active_account_count": 1},
				},
				"page":  1,
				"pages": 1,
			},
		})
	})

	p := newTestProvider(t, mux)
	lister, ok := p.(interface {
		ListPlatformGroups(context.Context) ([]relay.Group, error)
	})
	if !ok {
		t.Fatal("provider does not implement ListPlatformGroups")
	}
	groups, err := lister.ListPlatformGroups(context.Background())
	if err != nil {
		t.Fatalf("ListPlatformGroups() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]relay.Group{
		{ID: 5, Name: "Group Gamma", Platform: "anthropic"},
		{ID: 6, Name: "Group Alpha", Platform: "openai"},
	}, groups); diff != "" {
		t.Fatalf("groups mismatch (-want +got):\n%s", diff)
	}
}

func TestGetUserUsageDashboard(t *testing.T) {
	var loginCount int
	var meCount int
	var seenPathsMu sync.Mutex
	var seenPaths []string
	recordPath := func(r *http.Request) {
		seenPathsMu.Lock()
		seenPaths = append(seenPaths, r.URL.RequestURI())
		seenPathsMu.Unlock()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		loginCount++
		if r.Method != http.MethodPost {
			t.Errorf("login method = %s, want POST", r.Method)
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body.Email != "alice@example.com" || body.Password != "test-password" {
			t.Fatalf("login body = %+v, want alice credentials", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "test-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		meCount++
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("/me Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 1, "username": "alice", "email": "alice@example.com"},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		recordPath(r)
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("stats Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"total_api_keys":              99,
				"active_api_keys":             88,
				"total_requests":              150,
				"total_input_tokens":          50000,
				"total_output_tokens":         30000,
				"total_cache_creation_tokens": 4000,
				"total_cache_read_tokens":     10000,
				"total_tokens":                94000,
				"total_cost":                  2.50,
				"total_actual_cost":           2.00,
				"today_requests":              20,
				"today_input_tokens":          8000,
				"today_output_tokens":         5000,
				"today_cache_creation_tokens": 600,
				"today_cache_read_tokens":     700,
				"today_tokens":                14300,
				"today_cost":                  0.35,
				"today_actual_cost":           0.28,
				"average_duration_ms":         1250.5,
				"rpm":                         3,
				"tpm":                         4200,
				"by_platform":                 []map[string]any{{"platform": "openai", "total_actual_cost": 2.00}},
			},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		recordPath(r)
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("trend Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2026-06-01" || q.Get("end_date") != "2026-06-06" || q.Get("granularity") != "day" || q.Get("timezone") != "Asia/Shanghai" {
			t.Fatalf("trend query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"start_date":  "2026-06-01",
				"end_date":    "2026-06-06",
				"granularity": "day",
				"trend": []map[string]any{
					{
						"date":                  "2026-06-06",
						"requests":              20,
						"input_tokens":          8000,
						"output_tokens":         5000,
						"cache_creation_tokens": 600,
						"cache_read_tokens":     700,
						"total_tokens":          14300,
						"cost":                  0.35,
						"actual_cost":           0.28,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/models", func(w http.ResponseWriter, r *http.Request) {
		recordPath(r)
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("models Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2026-06-01" || q.Get("end_date") != "2026-06-06" || q.Get("timezone") != "Asia/Shanghai" {
			t.Fatalf("models query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"start_date": "2026-06-01",
				"end_date":   "2026-06-06",
				"models": []map[string]any{
					{
						"model":                 "example-model",
						"requests":              12,
						"input_tokens":          10000,
						"output_tokens":         5000,
						"cache_creation_tokens": 200,
						"cache_read_tokens":     300,
						"total_tokens":          15500,
						"cost":                  0.75,
						"actual_cost":           0.60,
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	got, err := p.GetUserUsageDashboard(context.Background(), "alice@example.com", "test-password", relay.UserUsageDashboardParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-06",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUserUsageDashboard() unexpected error: %v", err)
	}
	if loginCount != 1 || meCount != 1 {
		t.Fatalf("loginCount=%d meCount=%d, want one login and one /me", loginCount, meCount)
	}
	wantPaths := []string{
		"/api/v1/usage/dashboard/stats",
		"/api/v1/usage/dashboard/trend?end_date=2026-06-06&granularity=day&start_date=2026-06-01&timezone=Asia%2FShanghai",
		"/api/v1/usage/dashboard/models?end_date=2026-06-06&start_date=2026-06-01&timezone=Asia%2FShanghai",
	}
	seenPathsMu.Lock()
	sort.Strings(seenPaths)
	seenPathsMu.Unlock()
	sort.Strings(wantPaths)
	if diff := cmp.Diff(wantPaths, seenPaths); diff != "" {
		t.Fatalf("paths mismatch (-want +got):\n%s", diff)
	}
	if !got.Configured {
		t.Fatal("Configured = false, want true")
	}
	if got.Stats.TotalRequests != 150 || got.Stats.TotalCacheCreationTokens != 4000 || got.Stats.Rpm != 3 || got.Stats.Tpm != 4200 {
		t.Fatalf("unexpected stats: %+v", got.Stats)
	}
	if got.Stats.AverageDurationMs != 1250.5 {
		t.Fatalf("AverageDurationMs = %v, want 1250.5", got.Stats.AverageDurationMs)
	}
	if len(got.Trend) != 1 || got.Trend[0].CacheReadTokens != 700 {
		t.Fatalf("unexpected trend: %+v", got.Trend)
	}
	if len(got.Models) != 1 || got.Models[0].ActualCost != 0.60 {
		t.Fatalf("unexpected models: %+v", got.Models)
	}
	if got.Range.StartDate != "2026-06-01" || got.Range.EndDate != "2026-06-06" || got.Range.Granularity != "day" || got.Range.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected range: %+v", got.Range)
	}
}

func TestGetUserUsageDashboardFailsFastOnSub2APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "test-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 1, "username": "alice", "email": "alice@example.com"},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 502, "message": "upstream failed"})
	})

	p := newTestProvider(t, mux)
	_, err := p.GetUserUsageDashboard(context.Background(), "alice@example.com", "test-password", relay.UserUsageDashboardParams{})
	if err == nil {
		t.Fatal("GetUserUsageDashboard() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "usage dashboard stats") {
		t.Fatalf("error = %v, want stats context", err)
	}
}

func TestSub2APIListGroupRateMultipliersDecodesRateAndRPM(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/42/rate-multipliers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"user_id": 1001, "rate_multiplier": 2.0, "rpm_override": 120}},
		})
	})
	p := newTestProvider(t, mux)
	manager := p.(relay.GroupRateMultiplierManager)
	got, err := manager.ListGroupRateMultipliers(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListGroupRateMultipliers() error = %v", err)
	}
	if got[0].RateMultiplier == nil || *got[0].RateMultiplier != 2.0 {
		t.Fatalf("rate multiplier = %#v, want 2.0", got[0].RateMultiplier)
	}
	if got[0].RPMOverride == nil || *got[0].RPMOverride != 120 {
		t.Fatalf("rpm override = %#v, want 120", got[0].RPMOverride)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsDeduplicatesRequests(t *testing.T) {
	var mu sync.Mutex
	requestCountByGroup := make(map[int64]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		mu.Lock()
		requestCountByGroup[groupID]++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"user_id": groupID * 100, "rate_multiplier": float64(groupID)}},
		})
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	results := reader.GroupRateMultipliersForGroups(context.Background(), []int64{42, 7, 42, 9, 7})
	byGroup := rateMultiplierResultsByGroup(t, results)

	if diff := cmp.Diff([]int64{7, 9, 42}, sortedRateMultiplierResultGroupIDs(byGroup)); diff != "" {
		t.Fatalf("result group IDs mismatch (-want +got):\n%s", diff)
	}
	for _, groupID := range []int64{7, 9, 42} {
		result := byGroup[groupID]
		if result.Err != nil {
			t.Fatalf("group %d result error = %v", groupID, result.Err)
		}
		if len(result.Entries) != 1 || result.Entries[0].UserID != groupID*100 {
			t.Fatalf("group %d entries = %#v", groupID, result.Entries)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff(map[int64]int{7: 1, 9: 1, 42: 1}, requestCountByGroup); diff != "" {
		t.Fatalf("request counts mismatch (-want +got):\n%s", diff)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsLimitsConcurrencyToFour(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	requestCount := 0
	started := make(chan struct{}, 8)
	release := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		mu.Lock()
		active++
		requestCount++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()

		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"user_id": groupID * 100}},
		})
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	done := make(chan []relay.GroupRateMultiplierReadResult, 1)
	go func() {
		done <- reader.GroupRateMultipliersForGroups(context.Background(), []int64{1, 2, 3, 4, 5, 6, 7, 8})
	}()

	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("only %d requests started before timeout, want four", i)
		}
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	startedBeforeRelease := requestCount
	maxBeforeRelease := maxActive
	mu.Unlock()
	close(release)

	var results []relay.GroupRateMultiplierReadResult
	select {
	case results = <-done:
	case <-time.After(time.Second):
		t.Fatal("batch read did not finish after releasing requests")
	}
	if len(results) != 8 {
		t.Fatalf("result count = %d, want 8", len(results))
	}
	mu.Lock()
	defer mu.Unlock()
	if startedBeforeRelease != 4 {
		t.Fatalf("requests started before release = %d, want 4", startedBeforeRelease)
	}
	if maxBeforeRelease != 4 || maxActive != 4 {
		t.Fatalf("max active requests = %d before release and %d overall, want 4", maxBeforeRelease, maxActive)
	}
	if requestCount != 8 {
		t.Fatalf("request count = %d, want 8", requestCount)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsFinishesNearSlowestBranch(t *testing.T) {
	delays := map[int64]time.Duration{
		1: 80 * time.Millisecond,
		2: 120 * time.Millisecond,
		3: 160 * time.Millisecond,
		4: 200 * time.Millisecond,
	}
	var mu sync.Mutex
	arrived := 0
	allArrived := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		mu.Lock()
		arrived++
		if arrived == len(delays) {
			close(allArrived)
		}
		mu.Unlock()
		select {
		case <-allArrived:
		case <-r.Context().Done():
			return
		}
		select {
		case <-time.After(delays[groupID]):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{}})
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	startedAt := time.Now()
	results := reader.GroupRateMultipliersForGroups(context.Background(), []int64{1, 2, 3, 4})
	elapsed := time.Since(startedAt)

	if len(results) != 4 {
		t.Fatalf("result count = %d, want 4", len(results))
	}
	if elapsed < 180*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed = %v, want near the 200ms slowest branch", elapsed)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsRecordsPartialFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		if groupID == 11 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "group metadata unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"user_id": groupID * 100}},
		})
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	byGroup := rateMultiplierResultsByGroup(t, reader.GroupRateMultipliersForGroups(context.Background(), []int64{10, 11, 12}))

	if byGroup[10].Err != nil || len(byGroup[10].Entries) != 1 {
		t.Fatalf("group 10 result = %#v, want success", byGroup[10])
	}
	if byGroup[12].Err != nil || len(byGroup[12].Entries) != 1 {
		t.Fatalf("group 12 result = %#v, want success", byGroup[12])
	}
	if byGroup[11].Err == nil || !strings.Contains(byGroup[11].Err.Error(), "unexpected status 503") {
		t.Fatalf("group 11 error = %v, want status 503 failure", byGroup[11].Err)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsCancelsRequestAtDeadline(t *testing.T) {
	requestCanceled := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/42/rate-multipliers", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		requestCanceled <- struct{}{}
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	startedAt := time.Now()
	results := reader.GroupRateMultipliersForGroups(context.Background(), []int64{42})
	elapsed := time.Since(startedAt)

	if len(results) != 1 || results[0].GroupID != 42 {
		t.Fatalf("results = %#v, want one result for group 42", results)
	}
	if !errors.Is(results[0].Err, context.DeadlineExceeded) {
		t.Fatalf("result error = %v, want context deadline exceeded", results[0].Err)
	}
	if elapsed < 1800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatalf("elapsed = %v, want the two-second per-request deadline", elapsed)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("server request context was not canceled")
	}
}

func TestSub2APIGroupRateMultipliersForGroupsShorterCallerDeadlineWins(t *testing.T) {
	started := make(chan int64, 6)
	canceled := make(chan int64, 6)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		started <- groupID
		<-r.Context().Done()
		canceled <- groupID
	})

	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	results := reader.GroupRateMultipliersForGroups(ctx, []int64{1, 2, 3, 4, 5, 6})
	elapsed := time.Since(startedAt)

	if elapsed < 500*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed = %v, want the 750ms caller deadline to win", elapsed)
	}
	byGroup := rateMultiplierResultsByGroup(t, results)
	if len(byGroup) != 6 {
		t.Fatalf("result count = %d, want 6", len(byGroup))
	}
	for groupID := int64(1); groupID <= 6; groupID++ {
		result := byGroup[groupID]
		if !errors.Is(result.Err, context.DeadlineExceeded) {
			t.Fatalf("group %d error = %v, want caller deadline exceeded", groupID, result.Err)
		}
		if len(result.Entries) != 0 {
			t.Fatalf("group %d entries = %#v, want none after deadline", groupID, result.Entries)
		}
	}

	startedGroupIDs := drainInt64Channel(started)
	sort.Slice(startedGroupIDs, func(i, j int) bool { return startedGroupIDs[i] < startedGroupIDs[j] })
	if diff := cmp.Diff([]int64{1, 2, 3, 4}, startedGroupIDs); diff != "" {
		t.Fatalf("started HTTP groups mismatch (-want +got):\n%s", diff)
	}
	canceledGroupIDs := receiveInt64Values(t, canceled, 4)
	sort.Slice(canceledGroupIDs, func(i, j int) bool { return canceledGroupIDs[i] < canceledGroupIDs[j] })
	if diff := cmp.Diff([]int64{1, 2, 3, 4}, canceledGroupIDs); diff != "" {
		t.Fatalf("canceled HTTP groups mismatch (-want +got):\n%s", diff)
	}
}

func TestSub2APIGroupRateMultipliersForGroupsHonorsOverallBatchBudget(t *testing.T) {
	var mu sync.Mutex
	requested := make(map[int64]struct{})
	lifecycles := make(chan rateMultiplierRequestLifecycle, 16)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/", func(w http.ResponseWriter, r *http.Request) {
		groupID := rateMultiplierGroupIDFromPath(t, r.URL.Path)
		requestStartedAt := time.Now()
		mu.Lock()
		requested[groupID] = struct{}{}
		mu.Unlock()
		<-r.Context().Done()
		lifecycles <- rateMultiplierRequestLifecycle{GroupID: groupID, Duration: time.Since(requestStartedAt)}
	})

	groupIDs := make([]int64, 16)
	for i := range groupIDs {
		groupIDs[i] = int64(i + 1)
	}
	p := newTestProvider(t, mux)
	reader := p.(relay.GroupRateMultiplierBatchReader)
	startedAt := time.Now()
	results := reader.GroupRateMultipliersForGroups(context.Background(), groupIDs)
	elapsed := time.Since(startedAt)

	if elapsed < 4500*time.Millisecond || elapsed > 6500*time.Millisecond {
		t.Fatalf("elapsed = %v, want the five-second overall batch budget", elapsed)
	}
	byGroup := rateMultiplierResultsByGroup(t, results)
	if len(byGroup) != len(groupIDs) {
		t.Fatalf("result count = %d, want %d", len(byGroup), len(groupIDs))
	}
	for _, groupID := range groupIDs {
		result := byGroup[groupID]
		if !errors.Is(result.Err, context.DeadlineExceeded) {
			t.Fatalf("group %d error = %v, want deadline exceeded", groupID, result.Err)
		}
		if len(result.Entries) != 0 {
			t.Fatalf("group %d entries = %#v, want none after deadline", groupID, result.Entries)
		}
	}

	mu.Lock()
	requestedGroupIDs := make([]int64, 0, len(requested))
	for groupID := range requested {
		requestedGroupIDs = append(requestedGroupIDs, groupID)
	}
	mu.Unlock()
	sort.Slice(requestedGroupIDs, func(i, j int) bool { return requestedGroupIDs[i] < requestedGroupIDs[j] })
	if diff := cmp.Diff([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, requestedGroupIDs); diff != "" {
		t.Fatalf("requested HTTP groups mismatch (-want +got):\n%s", diff)
	}

	for _, lifecycle := range receiveRequestLifecycles(t, lifecycles, 12) {
		switch {
		case lifecycle.GroupID <= 8:
			if lifecycle.Duration < 1700*time.Millisecond || lifecycle.Duration > 3*time.Second {
				t.Fatalf("group %d request duration = %v, want the two-second request deadline", lifecycle.GroupID, lifecycle.Duration)
			}
		case lifecycle.GroupID <= 12:
			if lifecycle.Duration < 500*time.Millisecond || lifecycle.Duration > 1800*time.Millisecond {
				t.Fatalf("group %d request duration = %v, want cancellation by the batch deadline", lifecycle.GroupID, lifecycle.Duration)
			}
		default:
			t.Fatalf("unexpected HTTP request for group %d", lifecycle.GroupID)
		}
	}
}

func drainInt64Channel(values <-chan int64) []int64 {
	drained := make([]int64, 0, len(values))
	for len(values) > 0 {
		drained = append(drained, <-values)
	}
	return drained
}

func receiveInt64Values(t *testing.T, values <-chan int64, count int) []int64 {
	t.Helper()
	received := make([]int64, 0, count)
	for len(received) < count {
		select {
		case value := <-values:
			received = append(received, value)
		case <-time.After(time.Second):
			t.Fatalf("received %d values, want %d", len(received), count)
		}
	}
	return received
}

type rateMultiplierRequestLifecycle struct {
	GroupID  int64
	Duration time.Duration
}

func receiveRequestLifecycles(t *testing.T, values <-chan rateMultiplierRequestLifecycle, count int) []rateMultiplierRequestLifecycle {
	t.Helper()
	received := make([]rateMultiplierRequestLifecycle, 0, count)
	for len(received) < count {
		select {
		case value := <-values:
			received = append(received, value)
		case <-time.After(time.Second):
			t.Fatalf("received %d request lifecycles, want %d", len(received), count)
		}
	}
	return received
}

func rateMultiplierGroupIDFromPath(t *testing.T, path string) int64 {
	t.Helper()
	const prefix = "/api/v1/admin/groups/"
	const suffix = "/rate-multipliers"
	groupID, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix), 10, 64)
	if err != nil {
		t.Errorf("parse group ID from path %q: %v", path, err)
		return 0
	}
	return groupID
}

func rateMultiplierResultsByGroup(t *testing.T, results []relay.GroupRateMultiplierReadResult) map[int64]relay.GroupRateMultiplierReadResult {
	t.Helper()
	byGroup := make(map[int64]relay.GroupRateMultiplierReadResult, len(results))
	for _, result := range results {
		if _, exists := byGroup[result.GroupID]; exists {
			t.Fatalf("duplicate result for group %d", result.GroupID)
		}
		byGroup[result.GroupID] = result
	}
	return byGroup
}

func sortedRateMultiplierResultGroupIDs(results map[int64]relay.GroupRateMultiplierReadResult) []int64 {
	groupIDs := make([]int64, 0, len(results))
	for groupID := range results {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	return groupIDs
}

func TestSub2APIReplaceGroupRateMultipliersPreservesRPMPayloadShape(t *testing.T) {
	var body struct {
		Entries []relay.GroupRateMultiplierInput `json:"entries"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/42/rate-multipliers", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Entries) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "entries are required"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
	p := newTestProvider(t, mux)
	manager := p.(relay.GroupRateMultiplierManager)
	rpm := 120
	multiplier := 2.0
	if err := manager.ReplaceGroupRateMultipliers(context.Background(), 42, []relay.GroupRateMultiplierInput{{
		UserID:         1001,
		RateMultiplier: &multiplier,
		RPMOverride:    &rpm,
	}}); err != nil {
		t.Fatalf("ReplaceGroupRateMultipliers() error = %v", err)
	}
	if body.Entries[0].RPMOverride == nil || *body.Entries[0].RPMOverride != 120 {
		t.Fatalf("request body = %#v, want rpm_override preserved", body)
	}
}

func TestSub2APIGetUsageDashboardForUserUsesAdminFilteredEndpoints(t *testing.T) {
	seen := map[string]map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/usage/stats", func(w http.ResponseWriter, r *http.Request) {
		seen["stats"] = map[string]string{
			"user_id":     r.URL.Query().Get("user_id"),
			"start_date":  r.URL.Query().Get("start_date"),
			"end_date":    r.URL.Query().Get("end_date"),
			"granularity": r.URL.Query().Get("granularity"),
			"timezone":    r.URL.Query().Get("timezone"),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"total_tokens": 12}})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		seen["trend"] = map[string]string{
			"user_id":     r.URL.Query().Get("user_id"),
			"start_date":  r.URL.Query().Get("start_date"),
			"end_date":    r.URL.Query().Get("end_date"),
			"granularity": r.URL.Query().Get("granularity"),
			"timezone":    r.URL.Query().Get("timezone"),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/models", func(w http.ResponseWriter, r *http.Request) {
		seen["models"] = map[string]string{
			"user_id":     r.URL.Query().Get("user_id"),
			"start_date":  r.URL.Query().Get("start_date"),
			"end_date":    r.URL.Query().Get("end_date"),
			"granularity": r.URL.Query().Get("granularity"),
			"timezone":    r.URL.Query().Get("timezone"),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	})
	p := newTestProvider(t, mux)
	dashboard := p.(relay.SubjectUsageDashboardProvider)
	if _, err := dashboard.GetUsageDashboardForUser(context.Background(), 1001, relay.UserUsageDashboardParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-26",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	}); err != nil {
		t.Fatalf("GetUsageDashboardForUser() error = %v", err)
	}
	if diff := cmp.Diff(map[string]map[string]string{
		"stats": {
			"user_id":     "1001",
			"start_date":  "",
			"end_date":    "",
			"granularity": "",
			"timezone":    "",
		},
		"trend": {
			"user_id":     "1001",
			"start_date":  "2026-06-01",
			"end_date":    "2026-06-26",
			"granularity": "day",
			"timezone":    "Asia/Shanghai",
		},
		"models": {
			"user_id":     "1001",
			"start_date":  "2026-06-01",
			"end_date":    "2026-06-26",
			"granularity": "",
			"timezone":    "Asia/Shanghai",
		},
	}, seen); diff != "" {
		t.Fatalf("admin filtered endpoints mismatch (-want +got):\n%s", diff)
	}
}

func TestSub2APIGetBatchUserUsageStatsPostsUserIDs(t *testing.T) {
	var body map[string]any
	var trendRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"stats": map[string]any{
					"1001": map[string]any{
						"user_id":            1001,
						"today_actual_cost":  1.0,
						"total_actual_cost":  10.0,
						"total_tokens":       1234,
						"range_actual_cost":  7.5,
						"range_total_tokens": 987,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		trendRequests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	})
	p := newTestProvider(t, mux)
	summary := p.(relay.TeamUsageSummaryProvider)
	got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001}, relay.TeamUsageSummaryParams{
		StartDate: "2026-07-01", EndDate: "2026-07-07",
		Granularity: "day", Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetBatchUserUsageStats() error = %v", err)
	}
	if diff := cmp.Diff([]any{float64(1001)}, body["user_ids"]); diff != "" {
		t.Fatalf("user_ids mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]any{
		"start_date":  "2026-07-01",
		"end_date":    "2026-07-07",
		"granularity": "day",
		"timezone":    "Asia/Shanghai",
	}, map[string]any{
		"start_date":  body["start_date"],
		"end_date":    body["end_date"],
		"granularity": body["granularity"],
		"timezone":    body["timezone"],
	}); diff != "" {
		t.Fatalf("normalized range mismatch (-want +got):\n%s", diff)
	}
	if got[1001].TotalActualCost != 10.0 || got[1001].TotalTokens == nil || *got[1001].TotalTokens != 1234 {
		t.Fatalf("batch stats = %#v, want user 1001 total_actual_cost 10.0 total_tokens 1234", got)
	}
	if got[1001].RangeActualCost == nil || *got[1001].RangeActualCost != 7.5 ||
		got[1001].RangeTotalTokens == nil || *got[1001].RangeTotalTokens != 987 {
		t.Fatalf("batch stats = %#v, want independent range_actual_cost 7.5 range_total_tokens 987", got)
	}
	if trendRequests.Load() != 0 {
		t.Fatalf("trend requests = %d, want 0 for complete batch fields", trendRequests.Load())
	}
}

func TestSub2APIGetBatchUserUsageStatsLeavesRangeCompletionToTeamUsage(t *testing.T) {
	var trendCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"stats": map[string]any{
				"1001": map[string]any{"user_id": 1001, "today_actual_cost": 1.5, "total_actual_cost": 12.5},
			}},
		})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, _ *http.Request) {
		trendCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	})
	p := newTestProvider(t, mux)
	summary := p.(relay.TeamUsageSummaryProvider)

	got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001}, relay.TeamUsageSummaryParams{
		StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "UTC",
		RequireCompleteRange: true,
	})
	if err != nil {
		t.Fatalf("GetBatchUserUsageStats() error = %v", err)
	}
	if trendCalls.Load() != 0 {
		t.Fatalf("trend fallback calls = %d, want 0", trendCalls.Load())
	}
	if got[1001].RangeActualCost != nil || got[1001].RangeTotalTokens != nil {
		t.Fatalf("range fields = %#v/%#v, want incomplete stats without fallback", got[1001].RangeActualCost, got[1001].RangeTotalTokens)
	}
}

func TestSub2APIGetBatchUserUsageStatsDoesNotBackfillMissingRange(t *testing.T) {
	var requestedTrend []map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"stats": map[string]any{
					"1001": map[string]any{
						"user_id": 1001, "today_actual_cost": 1.0, "total_actual_cost": 10.0,
						"range_actual_cost": 7.5, "range_total_tokens": 987,
					},
					"1002": map[string]any{
						"user_id": 1002, "today_actual_cost": 2.0, "total_actual_cost": 20.0,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		requestedTrend = append(requestedTrend, map[string]string{
			"user_id":     r.URL.Query().Get("user_id"),
			"start_date":  r.URL.Query().Get("start_date"),
			"end_date":    r.URL.Query().Get("end_date"),
			"granularity": r.URL.Query().Get("granularity"),
			"timezone":    r.URL.Query().Get("timezone"),
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{
				{"date": "2026-07-01", "actual_cost": 1.25, "total_tokens": 100},
				{"date": "2026-07-02", "actual_cost": 2.50, "total_tokens": 200},
			},
		})
	})

	p := newTestProvider(t, mux)
	summary := p.(relay.TeamUsageSummaryProvider)
	got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001, 1002}, relay.TeamUsageSummaryParams{
		StartDate: "2026-07-01", EndDate: "2026-07-07",
		Granularity: "day", Timezone: "Asia/Shanghai", RequireCompleteRange: true,
	})
	if err != nil {
		t.Fatalf("GetBatchUserUsageStats() error = %v", err)
	}
	if got[1001].RangeActualCost == nil || *got[1001].RangeActualCost != 7.5 ||
		got[1001].RangeTotalTokens == nil || *got[1001].RangeTotalTokens != 987 {
		t.Fatalf("batch stats[1001] = %#v, want direct range totals 7.5/987", got[1001])
	}
	if got[1002].RangeActualCost != nil || got[1002].RangeTotalTokens != nil {
		t.Fatalf("batch stats[1002] = %#v, want range completion deferred to Team Usage", got[1002])
	}
	if diff := cmp.Diff([]map[string]string(nil), requestedTrend); diff != "" {
		t.Fatalf("trend requests mismatch (-want +got):\n%s", diff)
	}
}

func TestSub2APIGetBatchUserUsageStatsNeverStartsRangeTrend(t *testing.T) {
	var trendRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"stats": map[string]any{
					"1001": map[string]any{
						"user_id": 1001, "today_actual_cost": 1.0, "total_actual_cost": 10.0,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		trendRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "upstream failed"})
	})

	p := newTestProvider(t, mux)
	summary := p.(relay.TeamUsageSummaryProvider)
	got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001}, relay.TeamUsageSummaryParams{
		StartDate: "2026-07-01", EndDate: "2026-07-07",
		Granularity: "day", Timezone: "Asia/Shanghai", RequireCompleteRange: true,
	})
	if err != nil {
		t.Fatalf("GetBatchUserUsageStats() fallback error = %v, want nil", err)
	}
	if trendRequests.Load() != 0 {
		t.Fatalf("trend requests = %d, want 0", trendRequests.Load())
	}
	if got[1001].RangeActualCost != nil || got[1001].RangeTotalTokens != nil {
		t.Fatalf("range fields = %#v/%#v, want incomplete", got[1001].RangeActualCost, got[1001].RangeTotalTokens)
	}
	if got[1001].TodayActualCost != 1 || got[1001].TotalActualCost != 10 {
		t.Fatalf("comparison totals changed: %#v", got[1001])
	}
}

func TestSub2APIGetBatchUserUsageStatsIgnoresCompleteRangePolicy(t *testing.T) {
	tests := []struct {
		name  string
		trend []map[string]any
	}{
		{
			name: "missing token point",
			trend: []map[string]any{
				{"date": "2026-07-01", "actual_cost": 1.25, "total_tokens": 100},
				{"date": "2026-07-02", "actual_cost": 2.50},
			},
		},
		{
			name:  "empty trend",
			trend: []map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true,
					"data": map[string]any{
						"stats": map[string]any{
							"1001": map[string]any{"user_id": 1001},
						},
					},
				})
			})
			mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": tt.trend})
			})

			p := newTestProvider(t, mux)
			summary := p.(relay.TeamUsageSummaryProvider)
			got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001}, relay.TeamUsageSummaryParams{RequireCompleteRange: true})
			if err != nil {
				t.Fatalf("GetBatchUserUsageStats() error = %v", err)
			}
			if got[1001].RangeActualCost != nil || got[1001].RangeTotalTokens != nil {
				t.Fatalf("range totals = %#v/%#v, want deferred completion", got[1001].RangeActualCost, got[1001].RangeTotalTokens)
			}
		})
	}
}
