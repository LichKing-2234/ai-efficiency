package relay_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
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
	var seenPaths []string

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
		seenPaths = append(seenPaths, r.URL.RequestURI())
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
		seenPaths = append(seenPaths, r.URL.RequestURI())
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
		seenPaths = append(seenPaths, r.URL.RequestURI())
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

func TestSub2APITeamUsageTrendForUsersFansOutByUserID(t *testing.T) {
	var mu sync.Mutex
	var requested []map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, map[string]string{
			"user_id":     r.URL.Query().Get("user_id"),
			"start_date":  r.URL.Query().Get("start_date"),
			"end_date":    r.URL.Query().Get("end_date"),
			"granularity": r.URL.Query().Get("granularity"),
			"timezone":    r.URL.Query().Get("timezone"),
		})
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-06-26", "actual_cost": 1.25}},
		})
	})
	p := newTestProvider(t, mux)
	trender := p.(relay.TeamMemberTrendProvider)
	got, err := trender.GetUsageTrendForUsers(context.Background(), []int64{1001, 1002}, relay.TeamMemberTrendParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-26",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Slice(requested, func(i, j int) bool {
		return requested[i]["user_id"] < requested[j]["user_id"]
	})
	if diff := cmp.Diff([]map[string]string{
		{
			"user_id":     "1001",
			"start_date":  "2026-06-01",
			"end_date":    "2026-06-26",
			"granularity": "day",
			"timezone":    "Asia/Shanghai",
		},
		{
			"user_id":     "1002",
			"start_date":  "2026-06-01",
			"end_date":    "2026-06-26",
			"granularity": "day",
			"timezone":    "Asia/Shanghai",
		},
	}, requested); diff != "" {
		t.Fatalf("requested query mismatch (-want +got):\n%s", diff)
	}
	if len(got[1001]) != 1 || got[1001][0].ActualCost != 1.25 {
		t.Fatalf("trend[1001] = %#v, want one actual_cost point", got[1001])
	}
}

func TestSub2APITeamUsageTrendForUsersFetchesConcurrently(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	allArrived := make(chan struct{})
	closed := false

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		mu.Lock()
		seen[userID] = true
		if len(seen) == 2 && !closed {
			close(allArrived)
			closed = true
		}
		mu.Unlock()

		select {
		case <-allArrived:
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			http.Error(w, "requests were not concurrent", http.StatusGatewayTimeout)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-06-26", "actual_cost": 1.25}},
		})
	})
	p := newTestProvider(t, mux)
	trender := p.(relay.TeamMemberTrendProvider)

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	got, err := trender.GetUsageTrendForUsers(ctx, []int64{1001, 1002}, relay.TeamMemberTrendParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-26",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("trend map size = %d, want 2", len(got))
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
						"user_id":           1001,
						"today_actual_cost": 1.0,
						"total_actual_cost": 10.0,
						"total_tokens":      1234,
					},
				},
			},
		})
	})
	p := newTestProvider(t, mux)
	summary := p.(relay.TeamUsageSummaryProvider)
	got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001}, relay.TeamUsageSummaryParams{Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("GetBatchUserUsageStats() error = %v", err)
	}
	if diff := cmp.Diff([]any{float64(1001)}, body["user_ids"]); diff != "" {
		t.Fatalf("user_ids mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("Asia/Shanghai", body["timezone"]); diff != "" {
		t.Fatalf("timezone mismatch (-want +got):\n%s", diff)
	}
	if got[1001].TotalActualCost != 10.0 || got[1001].TotalTokens == nil || *got[1001].TotalTokens != 1234 {
		t.Fatalf("batch stats = %#v, want user 1001 total_actual_cost 10.0 total_tokens 1234", got)
	}
}
