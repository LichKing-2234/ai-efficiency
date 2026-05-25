package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newAuthHandlerTestService(t *testing.T) (*auth.Service, *ent.Client, *AuthHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
	svc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	svc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))
	return svc, client, NewAuthHandler(svc, client)
}

func newHandlerTestRefreshSessionStore(t *testing.T) auth.RefreshSessionStore {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return auth.NewRedisRefreshSessionStore(redisClient)
}

func createAuthHandlerTestUser(t *testing.T, client *ent.Client, username string) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@test.local").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestLogoutRevokesRefreshTokenAndReturnsOK(t *testing.T) {
	svc, client, h := newAuthHandlerTestService(t)
	u := createAuthHandlerTestUser(t, client, "alice")

	pair, err := svc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	})
	if err != nil {
		t.Fatalf("GenerateTokenPairForUser: %v", err)
	}

	r := gin.New()
	r.POST("/api/v1/auth/logout", h.Logout)
	body := bytes.NewBufferString(`{"refresh_token":"` + pair.RefreshToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, _, err := svc.RefreshToken(req.Context(), pair.RefreshToken); err == nil {
		t.Fatal("revoked refresh token should no longer refresh")
	}
}

func TestLogoutAllRevokesAuthenticatedUserSessions(t *testing.T) {
	svc, client, h := newAuthHandlerTestService(t)
	u := createAuthHandlerTestUser(t, client, "bob")

	pair, err := svc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	})
	if err != nil {
		t.Fatalf("GenerateTokenPairForUser: %v", err)
	}

	r := gin.New()
	r.POST("/api/v1/auth/logout-all", auth.RequireAuth(svc), h.LogoutAll)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, _, err := svc.RefreshToken(req.Context(), pair.RefreshToken); err == nil {
		t.Fatal("logout-all should revoke the user's refresh token")
	}
}
