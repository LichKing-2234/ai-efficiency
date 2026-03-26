package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequireAuthMissingHeader(t *testing.T) {
	svc := newTestService()

	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthInvalidToken(t *testing.T) {
	svc := newTestService()

	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthValidToken(t *testing.T) {
	svc := newTestService()

	info := &UserInfo{ID: 1, Username: "testuser", Role: "admin"}
	pair, _ := svc.generateTokenPair(info)

	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		uc := GetUserContext(c)
		if uc == nil {
			t.Error("UserContext should not be nil")
			c.JSON(500, nil)
			return
		}
		c.JSON(200, gin.H{
			"user_id":  uc.UserID,
			"username": uc.Username,
			"role":     uc.Role,
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireAdminAllowed(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 1, Username: "admin", Role: "admin"}
	pair, _ := svc.generateTokenPair(info)

	r := gin.New()
	r.GET("/admin", RequireAuth(svc), RequireAdmin(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireAdminDenied(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 2, Username: "user", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	r := gin.New()
	r.GET("/admin", RequireAuth(svc), RequireAdmin(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestExtractTokenFormats(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer abc123", "abc123"},
		{"lowercase bearer", "bearer abc123", "abc123"},
		{"no header", "", ""},
		{"no bearer prefix", "abc123", ""},
		{"basic auth", "Basic abc123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request, _ = http.NewRequest("GET", "/", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}
			got := extractToken(c)
			if got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetUserContextMissing(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	uc := GetUserContext(c)
	if uc != nil {
		t.Error("GetUserContext should return nil when not set")
	}
}
