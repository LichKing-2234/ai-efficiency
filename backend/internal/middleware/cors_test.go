package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

func TestCORSWithNilOrigins(t *testing.T) {
	handler := CORS(nil)
	if handler == nil {
		t.Fatal("CORS(nil) returned nil handler")
	}
}

func TestCORSWithEmptyOrigins(t *testing.T) {
	handler := CORS([]string{})
	if handler == nil {
		t.Fatal("CORS([]string{}) returned nil handler")
	}
}

func TestCORSWithBlankOriginsFallsBackToDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{""}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q, want default origin", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSWithCustomOrigins(t *testing.T) {
	handler := CORS([]string{"https://example.com", "https://app.example.com"})
	if handler == nil {
		t.Fatal("CORS with custom origins returned nil handler")
	}
}

func TestCORSMiddlewareHandlesOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://example.com"}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header to be set on OPTIONS request")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", w.Header().Get("Access-Control-Allow-Origin"), "https://example.com")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !headerListContains(got, telemetry.HeaderRequestID) {
		t.Errorf("Access-Control-Allow-Headers = %q, want %s", got, telemetry.HeaderRequestID)
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); !headerListContains(got, telemetry.HeaderRequestID) {
		t.Errorf("Access-Control-Expose-Headers = %q, want %s", got, telemetry.HeaderRequestID)
	}
}

func TestCORSMiddlewareAllowsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://example.com"}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok")
	}
}

func TestCORSMiddlewareDoesNotRejectSimpleRequestFromDisallowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://example.com"}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddlewareRejectsDisallowedPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://example.com"}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestCORSMiddlewareAllowsLoopbackVariantForConfiguredFrontend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"http://localhost:19084"}))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://127.0.0.1:19084")
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:19084" {
		t.Errorf("Access-Control-Allow-Origin = %q, want loopback variant", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func headerListContains(value, want string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), want) {
			return true
		}
	}
	return false
}
