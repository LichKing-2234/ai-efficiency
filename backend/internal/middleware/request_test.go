package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestTelemetrySelectsAndReturnsRequestID(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		preserve bool
	}{
		{name: "missing"},
		{name: "valid", incoming: "client_Request-123.valid", preserve: true},
		{name: "maximum length", incoming: strings.Repeat("a", 128), preserve: true},
		{name: "129 characters", incoming: strings.Repeat("a", 129)},
		{name: "whitespace", incoming: "request alpha"},
		{name: "slash", incoming: "request/alpha"},
		{name: "control character", incoming: "request\x1falpha"},
		{name: "unicode", incoming: "request-\u7528\u6237"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(RequestTelemetry(zap.NewNop(), "test-release"))
			var contextRequestID string
			router.GET("/test", func(c *gin.Context) {
				contextRequestID = telemetry.RequestID(c.Request.Context())
				c.String(http.StatusOK, "ok")
			})

			request := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.incoming != "" {
				request.Header.Set(telemetry.HeaderRequestID, tt.incoming)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			selected := response.Header().Get(telemetry.HeaderRequestID)
			if selected == "" {
				t.Fatal("response request ID is empty")
			}
			if contextRequestID != selected {
				t.Fatalf("context request ID = %q, response request ID = %q", contextRequestID, selected)
			}
			if tt.preserve {
				if selected != tt.incoming {
					t.Fatalf("selected request ID = %q, want preserved %q", selected, tt.incoming)
				}
				return
			}
			if _, err := uuid.Parse(selected); err != nil {
				t.Fatalf("selected request ID = %q, want generated UUID: %v", selected, err)
			}
			if !validRequestIDForTest(selected) {
				t.Fatalf("generated request ID = %q, want 1-128 ASCII [A-Za-z0-9._-]", selected)
			}
		})
	}
}

func TestRequestTelemetryReturnsRequestIDOnEveryResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestTelemetry(zap.NewNop(), "test-release"))
	router.Use(CORS([]string{"https://app.example.com"}))
	router.GET("/success", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/error", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusInternalServerError)
	})

	tests := []struct {
		name       string
		method     string
		path       string
		origin     string
		wantStatus int
	}{
		{name: "success", method: http.MethodGet, path: "/success", wantStatus: http.StatusOK},
		{name: "error", method: http.MethodGet, path: "/error", wantStatus: http.StatusInternalServerError},
		{name: "options", method: http.MethodOptions, path: "/success", origin: "https://app.example.com", wantStatus: http.StatusNoContent},
		{name: "not found", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.Header.Set(telemetry.HeaderRequestID, "request-response")
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
				request.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if got := response.Header().Get(telemetry.HeaderRequestID); got != "request-response" {
				t.Fatalf("response %s = %q, want %q", telemetry.HeaderRequestID, got, "request-response")
			}
		})
	}
}

func TestRequestTelemetryUsesRouteTemplatesAndProtectsPrivacy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(RequestTelemetry(zap.New(core), "test-release"))
	router.GET("/users/:id", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	requests := []struct {
		path      string
		requestID string
		secrets   []string
	}{
		{
			path:      "/users/7?email=alice@example.com",
			requestID: "request-alpha",
			secrets:   []string{"/users/7", "alice@example.com", "private-body-alpha", "7"},
		},
		{
			path:      "/users/99?email=bob@example.org",
			requestID: "request-beta",
			secrets:   []string{"/users/99", "bob@example.org", "private-body-beta", "99"},
		},
	}

	for _, tt := range requests {
		request := httptest.NewRequest(http.MethodGet, tt.path, strings.NewReader(tt.secrets[2]))
		request.Header.Set(telemetry.HeaderRequestID, tt.requestID)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tt.path, response.Code)
		}
	}

	entries := observed.All()
	if len(entries) != len(requests) {
		t.Fatalf("request events = %d, want %d", len(entries), len(requests))
	}
	for index, entry := range entries {
		if entry.Message != "http_request" {
			t.Fatalf("event %d message = %q, want %q", index, entry.Message, "http_request")
		}
		fields := entry.ContextMap()
		if len(fields) != 8 {
			t.Fatalf("event %d fields = %#v, want exactly 8 request fields", index, fields)
		}
		assertRequestField(t, fields, "event", "http_request")
		assertRequestField(t, fields, "route", "/users/:id")
		assertRequestField(t, fields, "method", http.MethodGet)
		assertRequestField(t, fields, "status_class", "2xx")
		assertRequestField(t, fields, "release", "test-release")
		assertRequestField(t, fields, "request_id", requests[index].requestID)
		if got, ok := fields["response_bytes"].(int64); !ok || got != int64(len("ok")) {
			t.Fatalf("response_bytes = %#v, want %d", fields["response_bytes"], len("ok"))
		}
		if got, ok := fields["duration_ms"].(int64); !ok || got < 0 {
			t.Fatalf("duration_ms = %#v, want non-negative int64", fields["duration_ms"])
		}
		assertRequestPrivacy(t, entry, fields, requests[index].secrets...)
	}
}

func TestRequestTelemetryUsesFixedUnmatchedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(RequestTelemetry(zap.New(core), "test-release"))

	request := httptest.NewRequest(http.MethodGet, "/private/raw-path?email=alice@example.com", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("request events = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	assertRequestField(t, fields, "route", "unmatched")
	assertRequestPrivacy(t, entries[0], fields, "/private/raw-path", "alice@example.com")
}

func assertRequestField(t *testing.T, fields map[string]interface{}, key, want string) {
	t.Helper()
	if got, ok := fields[key].(string); !ok || got != want {
		t.Fatalf("field %s = %#v, want %q", key, fields[key], want)
	}
}

func assertRequestPrivacy(t *testing.T, entry observer.LoggedEntry, fields map[string]interface{}, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(entry.Message, secret) {
			t.Fatalf("event message contains private value %q", secret)
		}
		for key, value := range fields {
			text, ok := value.(string)
			if ok && strings.Contains(text, secret) {
				t.Fatalf("field %s contains private value %q", key, secret)
			}
		}
	}
}

func validRequestIDForTest(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		char := id[index]
		if (char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}
