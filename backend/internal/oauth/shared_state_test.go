package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
)

func newSharedOAuthStore(t *testing.T) authstate.Store {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return authstate.NewRedisStore(client, "test-ns")
}

func newSharedOAuthHandler(t *testing.T, store authstate.Store) *oauth.Handler {
	t.Helper()
	return oauth.NewHandlerWithStore(oauth.NewServer(), "http://localhost:5173", &mockTokenGen{}, store)
}

func newDeviceRouter(handler *oauth.Handler) *gin.Engine {
	router := gin.New()
	router.POST("/oauth/device/code", handler.DeviceCode)
	router.POST("/oauth/token", handler.Token)
	verify := router.Group("/oauth")
	verify.Use(func(c *gin.Context) {
		c.Set(authpkg.ContextKeyUser, &authpkg.UserContext{UserID: 7, Username: "alice", Role: "member"})
		c.Next()
	})
	verify.POST("/device/verify", handler.VerifyDevice)
	return router
}

func newAuthorizeRouter(handler *oauth.Handler) *gin.Engine {
	router := gin.New()
	router.POST("/oauth/token", handler.Token)
	approve := router.Group("/oauth")
	approve.Use(func(c *gin.Context) {
		c.Set(authpkg.ContextKeyUser, &authpkg.UserContext{UserID: 7, Username: "alice", Role: "member"})
		c.Next()
	})
	approve.POST("/authorize/approve", handler.Approve)
	return router
}

// TestDeviceFlowCompletesOnAnotherInstance proves a device authorization
// started on one process can be approved and exchanged on another process.
func TestDeviceFlowCompletesOnAnotherInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newSharedOAuthStore(t)
	issuing := newDeviceRouter(newSharedOAuthHandler(t, store))
	completing := newDeviceRouter(newSharedOAuthHandler(t, store))

	codeReq := httptest.NewRequest(http.MethodPost, "/oauth/device/code", strings.NewReader("client_id=ae-cli"))
	codeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	codeRec := httptest.NewRecorder()
	issuing.ServeHTTP(codeRec, codeReq)
	if codeRec.Code != http.StatusOK {
		t.Fatalf("device code status=%d body=%s", codeRec.Code, codeRec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(codeRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode device code: %v", err)
	}
	deviceCode := payload["device_code"].(string)
	userCode := payload["user_code"].(string)

	verifyBody := strings.NewReader(`{"user_code":"` + userCode + `","approved":true}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", verifyBody)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyRec := httptest.NewRecorder()
	completing.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyRec.Code, verifyRec.Body.String())
	}

	tokenForm := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRec := httptest.NewRecorder()
	completing.ServeHTTP(tokenRec, tokenReq)
	if !strings.Contains(tokenRec.Body.String(), "test-access-token") {
		t.Fatalf("token body=%s", tokenRec.Body.String())
	}
}

// TestAuthorizationCodeExchangedOnAnotherInstance proves an approved PKCE code
// can be redeemed on a process other than the one that issued it.
func TestAuthorizationCodeExchangedOnAnotherInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newSharedOAuthStore(t)
	issuing := newAuthorizeRouter(newSharedOAuthHandler(t, store))
	redeeming := newAuthorizeRouter(newSharedOAuthHandler(t, store))

	verifier := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	redirectURI := "http://localhost:18234/callback"
	approveBody := `{"client_id":"ae-cli","redirect_uri":"` + redirectURI + `","code_challenge":"` + challenge + `","code_challenge_method":"S256","state":"xyz","approved":true}`
	approveReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize/approve", strings.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveRec := httptest.NewRecorder()
	issuing.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approvePayload map[string]string
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approvePayload); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	code := codeFromRedirect(t, approvePayload["redirect_uri"])

	exchange := func(router *gin.Engine) *httptest.ResponseRecorder {
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"client_id":     {"ae-cli"},
			"code_verifier": {verifier},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := exchange(redeeming); !strings.Contains(rec.Body.String(), "test-access-token") {
		t.Fatalf("cross-instance exchange body=%s", rec.Body.String())
	}
	if rec := exchange(issuing); !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Fatalf("replayed exchange body=%s", rec.Body.String())
	}
}

func codeFromRedirect(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse redirect_uri: %v", err)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatalf("redirect_uri %q has no code", raw)
	}
	return code
}
