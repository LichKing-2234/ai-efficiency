package oauth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type deviceTokenGen struct{}

func (deviceTokenGen) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	return "device-access-token", "device-refresh-token", 7200, nil
}

func setupDeviceRouter(t *testing.T) (*gin.Engine, *Handler, *time.Time) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	handler := NewHandler(NewServer(), "http://localhost:5173", deviceTokenGen{})
	handler.now = func() time.Time { return now }
	handler.deviceCodeExpiry = 15 * time.Minute
	handler.devicePollInterval = 5 * time.Second

	r := gin.New()
	r.POST("/oauth/device/code", handler.DeviceCode)
	r.GET("/oauth/device", handler.DevicePage)
	r.POST("/oauth/token", handler.Token)

	verify := r.Group("/oauth")
	verify.Use(func(c *gin.Context) {
		c.Set(authpkg.ContextKeyUser, &authpkg.UserContext{
			UserID:   7,
			Username: "alice",
			Role:     "member",
		})
		c.Next()
	})
	verify.POST("/device/verify", handler.VerifyDevice)

	return r, handler, &now
}

func issueDeviceCode(t *testing.T, router *gin.Engine) (map[string]any, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/oauth/device/code", strings.NewReader("client_id=ae-cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device code status=%d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode device code: %v", err)
	}

	return payload, payload["device_code"].(string)
}

func TestTokenDeviceGrantPendingApprovedConsumed(t *testing.T) {
	router, _, _ := setupDeviceRouter(t)
	payload, deviceCode := issueDeviceCode(t, router)

	tokenBody := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}

	pendingReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	pendingReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	pendingW := httptest.NewRecorder()
	router.ServeHTTP(pendingW, pendingReq)
	if !strings.Contains(pendingW.Body.String(), "authorization_pending") {
		t.Fatalf("pending body=%s", pendingW.Body.String())
	}

	verifyBody := bytes.NewBufferString(`{"user_code":"` + strings.ToLower(payload["user_code"].(string)) + `","approved":true}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", verifyBody)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	router.ServeHTTP(verifyW, verifyReq)
	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyW.Code, verifyW.Body.String())
	}

	successReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	successReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	successW := httptest.NewRecorder()
	router.ServeHTTP(successW, successReq)
	if !strings.Contains(successW.Body.String(), "device-access-token") {
		t.Fatalf("success body=%s", successW.Body.String())
	}

	consumedReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	consumedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consumedW := httptest.NewRecorder()
	router.ServeHTTP(consumedW, consumedReq)
	if !strings.Contains(consumedW.Body.String(), "invalid_grant") {
		t.Fatalf("consumed body=%s", consumedW.Body.String())
	}
}

func TestTokenDeviceGrantDeniedExpiredAndSlowDown(t *testing.T) {
	router, handler, now := setupDeviceRouter(t)
	payload, deviceCode := issueDeviceCode(t, router)

	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}

	firstReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	firstReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)

	secondReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	secondReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	if !strings.Contains(secondW.Body.String(), "slow_down") {
		t.Fatalf("slow_down body=%s", secondW.Body.String())
	}

	*now = now.Add(6 * time.Second)

	denyBody := bytes.NewBufferString(`{"user_code":"` + payload["user_code"].(string) + `","approved":false}`)
	denyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", denyBody)
	denyReq.Header.Set("Content-Type", "application/json")
	denyW := httptest.NewRecorder()
	router.ServeHTTP(denyW, denyReq)
	if denyW.Code != http.StatusOK {
		t.Fatalf("deny status=%d body=%s", denyW.Code, denyW.Body.String())
	}

	deniedReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	deniedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deniedW := httptest.NewRecorder()
	router.ServeHTTP(deniedW, deniedReq)
	if !strings.Contains(deniedW.Body.String(), "access_denied") {
		t.Fatalf("denied body=%s", deniedW.Body.String())
	}

	_, expiringCode := issueDeviceCode(t, router)
	handler.devices[expiringCode].ExpiresAt = now.Add(-time.Second)

	expiredForm := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {expiringCode},
		"client_id":   {"ae-cli"},
	}
	expiredReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(expiredForm.Encode()))
	expiredReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expiredW := httptest.NewRecorder()
	router.ServeHTTP(expiredW, expiredReq)
	if !strings.Contains(expiredW.Body.String(), "expired_token") {
		t.Fatalf("expired body=%s", expiredW.Body.String())
	}
}

func TestVerifyDeviceAcceptsUserCodeWithoutDash(t *testing.T) {
	router, _, _ := setupDeviceRouter(t)
	payload, _ := issueDeviceCode(t, router)

	userCode := strings.ReplaceAll(payload["user_code"].(string), "-", "")
	verifyBody := bytes.NewBufferString(`{"user_code":"` + userCode + `","approved":true}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", verifyBody)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	router.ServeHTTP(verifyW, verifyReq)

	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyW.Code, verifyW.Body.String())
	}
	if !strings.Contains(verifyW.Body.String(), "approved") {
		t.Fatalf("verify body=%s", verifyW.Body.String())
	}
}

func TestDeviceCodeRegeneratesDuplicateUserCodes(t *testing.T) {
	router, handler, _ := setupDeviceRouter(t)

	oldGenerateCode := generateCodeFunc
	oldGenerateUserCode := generateUserCodeFunc
	t.Cleanup(func() {
		generateCodeFunc = oldGenerateCode
		generateUserCodeFunc = oldGenerateUserCode
	})

	deviceCodes := []string{"device-1", "device-2", "device-3"}
	userCodes := []string{"ABCD-EFGH", "ABCD-EFGH", "WXYZ-1234"}
	generateCodeFunc = func() (string, error) {
		code := deviceCodes[0]
		deviceCodes = deviceCodes[1:]
		return code, nil
	}
	generateUserCodeFunc = func() (string, error) {
		code := userCodes[0]
		userCodes = userCodes[1:]
		return code, nil
	}

	firstPayload, _ := issueDeviceCode(t, router)
	secondPayload, _ := issueDeviceCode(t, router)

	if firstPayload["user_code"] == secondPayload["user_code"] {
		t.Fatalf("expected regenerated unique user_code, got duplicate %q", firstPayload["user_code"])
	}
	if got := handler.devicesByUserCode[normalizeUserCode(secondPayload["user_code"].(string))]; got == nil {
		t.Fatal("expected secondary user_code index to contain second device entry")
	}
}

func TestDeviceCodeReturnsServerErrorWhenRandomGenerationFails(t *testing.T) {
	router, _, _ := setupDeviceRouter(t)

	oldGenerateCode := generateCodeFunc
	t.Cleanup(func() { generateCodeFunc = oldGenerateCode })
	generateCodeFunc = func() (string, error) {
		return "", errRandomRead
	}

	req := httptest.NewRequest(http.MethodPost, "/oauth/device/code", strings.NewReader("client_id=ae-cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "server_error") {
		t.Fatalf("body=%s", w.Body.String())
	}
}
