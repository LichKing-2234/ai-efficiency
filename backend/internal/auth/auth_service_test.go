package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/enttest"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Auth Service with Ent DB tests
// ---------------------------------------------------------------------------

func setupAuthEntClient(t *testing.T) *ent.Client {
	t.Helper()
	return enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
}

func newTestServiceWithDB(t *testing.T) (*Service, *ent.Client) {
	t.Helper()
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", 7200, 604800, zap.NewNop())
	return svc, client
}

// ---------------------------------------------------------------------------
// NewService tests
// ---------------------------------------------------------------------------

func TestNewServiceDefaultTTL(t *testing.T) {
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", 0, 0, zap.NewNop())
	if svc.accessTokenTTL != 7200*time.Second {
		t.Errorf("accessTokenTTL = %v, want %v", svc.accessTokenTTL, 7200*time.Second)
	}
	if svc.refreshTokenTTL != 604800*time.Second {
		t.Errorf("refreshTokenTTL = %v, want %v", svc.refreshTokenTTL, 604800*time.Second)
	}
}

func TestNewServiceNegativeTTL(t *testing.T) {
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", -1, -1, zap.NewNop())
	if svc.accessTokenTTL != 7200*time.Second {
		t.Errorf("accessTokenTTL = %v, want default 7200s", svc.accessTokenTTL)
	}
	if svc.refreshTokenTTL != 604800*time.Second {
		t.Errorf("refreshTokenTTL = %v, want default 604800s", svc.refreshTokenTTL)
	}
}

func TestNewServiceCustomTTL(t *testing.T) {
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", 3600, 86400, zap.NewNop())
	if svc.accessTokenTTL != 3600*time.Second {
		t.Errorf("accessTokenTTL = %v, want 3600s", svc.accessTokenTTL)
	}
	if svc.refreshTokenTTL != 86400*time.Second {
		t.Errorf("refreshTokenTTL = %v, want 86400s", svc.refreshTokenTTL)
	}
}

// ---------------------------------------------------------------------------
// RegisterProvider tests
// ---------------------------------------------------------------------------

func TestRegisterProvider(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	mock := &mockAuthProvider{name: "test", userInfo: nil, err: nil}
	svc.RegisterProvider(mock)
	if len(svc.providers) != 1 {
		t.Errorf("providers count = %d, want 1", len(svc.providers))
	}
	if svc.providers[0].Name() != "test" {
		t.Errorf("provider name = %q, want %q", svc.providers[0].Name(), "test")
	}
}

func TestRegisterMultipleProviders(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{name: "sso"})
	svc.RegisterProvider(&mockAuthProvider{name: "ldap"})
	if len(svc.providers) != 2 {
		t.Errorf("providers count = %d, want 2", len(svc.providers))
	}
}

// ---------------------------------------------------------------------------
// Login tests
// ---------------------------------------------------------------------------

func TestLoginNoProviders(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error with no providers")
	}
}

func TestLoginSpecificProviderSuccess(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "alice",
			Email:      "alice@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	tokens, info, err := svc.Login(context.Background(), LoginRequest{
		Username: "alice",
		Password: "secret",
		Source:   "ldap",
	})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if tokens == nil {
		t.Fatal("expected non-nil tokens")
	}
	if info == nil {
		t.Fatal("expected non-nil user info")
	}
	if info.Username != "alice" {
		t.Errorf("username = %q, want %q", info.Username, "alice")
	}
	if tokens.AccessToken == "" {
		t.Error("access token should not be empty")
	}
	if tokens.RefreshToken == "" {
		t.Error("refresh token should not be empty")
	}
}

func TestLoginSpecificProviderNotFound(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{name: "ldap"})

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "pass",
		Source:   "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	if err.Error() != "unknown auth source: nonexistent" {
		t.Errorf("error = %q, want 'unknown auth source: nonexistent'", err.Error())
	}
}

func TestLoginSourceCaseInsensitive(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{
		name: "sso",
		userInfo: &UserInfo{
			Username:   "alice",
			Email:      "alice@example.com",
			Role:       "user",
			AuthSource: "sub2api_sso",
		},
	})

	// Source "SSO" (uppercase) should match provider named "sso"
	tokens, info, err := svc.Login(context.Background(), LoginRequest{
		Username: "alice",
		Password: "secret",
		Source:   "SSO",
	})
	if err != nil {
		t.Fatalf("Login with uppercase Source failed: %v", err)
	}
	if tokens == nil {
		t.Fatal("expected non-nil tokens")
	}
	if info.Username != "alice" {
		t.Errorf("username = %q, want %q", info.Username, "alice")
	}
}

func TestLoginSourceMixedCase(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "bob",
			Email:      "bob@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	// Source "Ldap" (mixed case) should match provider named "ldap"
	_, info, err := svc.Login(context.Background(), LoginRequest{
		Username: "bob",
		Password: "pass",
		Source:   "Ldap",
	})
	if err != nil {
		t.Fatalf("Login with mixed-case Source failed: %v", err)
	}
	if info.Username != "bob" {
		t.Errorf("username = %q, want %q", info.Username, "bob")
	}
}

func TestLoginSourceMatchedButAuthReturnsNilNil(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	// Provider "sso" is registered but returns nil, nil (invalid credentials)
	svc.RegisterProvider(&mockAuthProvider{
		name:     "sso",
		userInfo: nil,
		err:      nil,
	})

	// Source "SSO" matches provider "sso" via case-insensitive match,
	// but Authenticate returns nil,nil. Should report "invalid credentials",
	// NOT "unknown auth source: SSO".
	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "wrong",
		Source:   "SSO",
	})
	if err == nil {
		t.Fatal("expected error when provider returns nil,nil")
	}
	if strings.Contains(err.Error(), "unknown auth source") {
		t.Errorf("error = %q, should NOT say 'unknown auth source' when provider was found", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error = %q, want 'invalid credentials'", err.Error())
	}
}

func TestLoginSpecificProviderAuthError(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		err:  fmt.Errorf("connection refused"),
	})

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "pass",
		Source:   "ldap",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoginFallthrough(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	// First provider returns nil (skip), second succeeds
	svc.RegisterProvider(&mockAuthProvider{
		name:     "sso",
		userInfo: nil,
		err:      nil,
	})
	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "bob",
			Email:      "bob@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	tokens, info, err := svc.Login(context.Background(), LoginRequest{
		Username: "bob",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if info.Username != "bob" {
		t.Errorf("username = %q, want %q", info.Username, "bob")
	}
	if tokens == nil {
		t.Fatal("expected non-nil tokens")
	}
}

func TestLoginAllProvidersFail(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{
		name: "sso",
		err:  nil, // returns nil, nil (skip)
	})
	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		err:  fmt.Errorf("invalid credentials"),
	})

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "wrong",
	})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestLoginAllProvidersReturnNil(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	svc.RegisterProvider(&mockAuthProvider{name: "sso"})
	svc.RegisterProvider(&mockAuthProvider{name: "ldap"})

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error when all providers return nil")
	}
	if err.Error() != "authentication failed: invalid credentials" {
		t.Errorf("error = %q, want 'authentication failed: invalid credentials'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ensureLocalUser tests
// ---------------------------------------------------------------------------

func TestEnsureLocalUserCreatesNew(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	info := &UserInfo{
		Username:   "newuser",
		Email:      "newuser@example.com",
		Role:       "user",
		AuthSource: "ldap",
	}

	u, err := svc.ensureLocalUser(ctx, info)
	if err != nil {
		t.Fatalf("ensureLocalUser error: %v", err)
	}
	if u.Username != "newuser" {
		t.Errorf("username = %q, want %q", u.Username, "newuser")
	}
	if u.Email != "newuser@example.com" {
		t.Errorf("email = %q, want %q", u.Email, "newuser@example.com")
	}

	// Verify in DB
	dbUser, err := client.User.Query().Where(entuser.UsernameEQ("newuser")).Only(ctx)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if dbUser.Email != "newuser@example.com" {
		t.Errorf("db email = %q, want %q", dbUser.Email, "newuser@example.com")
	}
}

func TestEnsureLocalUserFindsExisting(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	// Pre-create user
	_, err := client.User.Create().
		SetUsername("existing").
		SetEmail("existing@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleAdmin).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	info := &UserInfo{
		Username:   "existing",
		Email:      "different@example.com",
		Role:       "user",
		AuthSource: "ldap",
	}

	u, err := svc.ensureLocalUser(ctx, info)
	if err != nil {
		t.Fatalf("ensureLocalUser error: %v", err)
	}
	// Should return existing user, not create new
	if u.Email != "existing@example.com" {
		t.Errorf("email = %q, want %q (existing)", u.Email, "existing@example.com")
	}
	// Role should be synced from provider
	if u.Role != entuser.RoleUser {
		t.Errorf("role = %q, want %q (synced from provider)", u.Role, entuser.RoleUser)
	}
}

func TestEnsureLocalUserWithSub2apiID(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	ctx := context.Background()

	sub2apiID := 999
	info := &UserInfo{
		Username:      "ssouser",
		Email:         "ssouser@example.com",
		Role:          "user",
		AuthSource:    "sub2api_sso",
		RelayUserID: &sub2apiID,
	}

	u, err := svc.ensureLocalUser(ctx, info)
	if err != nil {
		t.Fatalf("ensureLocalUser error: %v", err)
	}
	if u.RelayUserID == nil || *u.RelayUserID != 999 {
		t.Errorf("relay_user_id = %v, want 999", u.RelayUserID)
	}
}

// ---------------------------------------------------------------------------
// RefreshToken tests
// ---------------------------------------------------------------------------

func TestRefreshTokenSuccess(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	// Create user in DB
	u, err := client.User.Create().
		SetUsername("refreshuser").
		SetEmail("refresh@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Generate initial tokens
	info := &UserInfo{ID: u.ID, Username: "refreshuser", Role: "user"}
	pair, err := svc.generateTokenPair(info)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}

	// Refresh
	newPair, newInfo, err := svc.RefreshToken(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken error: %v", err)
	}
	if newPair == nil {
		t.Fatal("expected non-nil token pair")
	}
	if newInfo == nil {
		t.Fatal("expected non-nil user info")
	}
	if newInfo.Username != "refreshuser" {
		t.Errorf("username = %q, want %q", newInfo.Username, "refreshuser")
	}
	if newPair.AccessToken == "" {
		t.Error("new access token should not be empty")
	}
}

func TestRefreshTokenInvalid(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	_, _, err := svc.RefreshToken(context.Background(), "invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid refresh token")
	}
}

func TestRefreshTokenWithAccessToken(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	u, _ := client.User.Create().
		SetUsername("user1").
		SetEmail("user1@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(ctx)

	info := &UserInfo{ID: u.ID, Username: "user1", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	// Using access token as refresh token should fail
	_, _, err := svc.RefreshToken(ctx, pair.AccessToken)
	if err == nil {
		t.Fatal("expected error when using access token as refresh token")
	}
}

func TestRefreshTokenUserNotFound(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)

	// Generate token for non-existent user
	info := &UserInfo{ID: 99999, Username: "ghost", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	_, _, err := svc.RefreshToken(context.Background(), pair.RefreshToken)
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

// ---------------------------------------------------------------------------
// ValidateAccessToken edge cases
// ---------------------------------------------------------------------------

func TestValidateAccessTokenWrongSigningMethod(t *testing.T) {
	svc := newTestService()

	// Create a token with a non-HMAC signing method (none)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"user_id": 1,
		"type":    "access",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := svc.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Error("should reject token with 'none' signing method")
	}
}

// ---------------------------------------------------------------------------
// GenerateTokenPairForUser (exported) tests
// ---------------------------------------------------------------------------

func TestGenerateTokenPairForUser(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 5, Username: "exported", Role: "admin"}
	pair, err := svc.GenerateTokenPairForUser(info)
	if err != nil {
		t.Fatalf("GenerateTokenPairForUser error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Error("tokens should not be empty")
	}

	// Validate the generated access token
	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken error: %v", err)
	}
	if claims["username"] != "exported" {
		t.Errorf("username = %v, want 'exported'", claims["username"])
	}
}

// ---------------------------------------------------------------------------
// Middleware additional tests
// ---------------------------------------------------------------------------

func TestRequireAuthRefreshTokenRejected(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 1, Username: "user", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.RefreshToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (refresh token should be rejected)", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	svc := &Service{
		jwtSecret:       []byte("test-secret-key-for-unit-tests!!"),
		accessTokenTTL:  -1 * time.Hour,
		refreshTokenTTL: 7 * 24 * time.Hour,
	}
	info := &UserInfo{ID: 1, Username: "user", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (expired token)", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAdminNoUserContext(t *testing.T) {
	r := gin.New()
	r.GET("/admin", RequireAdmin(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (no user context)", w.Code, http.StatusForbidden)
	}
}

func TestGetUserContextWrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUser, "not-a-user-context")
	uc := GetUserContext(c)
	if uc != nil {
		t.Error("GetUserContext should return nil for wrong type")
	}
}

func TestRequireAuthUserContextValues(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 42, Username: "alice", Role: "admin"}
	pair, _ := svc.generateTokenPair(info)

	var capturedUC *UserContext
	r := gin.New()
	r.GET("/test", RequireAuth(svc), func(c *gin.Context) {
		capturedUC = GetUserContext(c)
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capturedUC == nil {
		t.Fatal("UserContext should not be nil")
	}
	if capturedUC.UserID != 42 {
		t.Errorf("UserID = %d, want 42", capturedUC.UserID)
	}
	if capturedUC.Username != "alice" {
		t.Errorf("Username = %q, want 'alice'", capturedUC.Username)
	}
	if capturedUC.Role != "admin" {
		t.Errorf("Role = %q, want 'admin'", capturedUC.Role)
	}
}

// ---------------------------------------------------------------------------
// LDAP provider additional tests
// ---------------------------------------------------------------------------

func TestLDAPProviderAuthenticateInvalidURL(t *testing.T) {
	// Non-empty URL but unreachable — should fail at dial
	p := newTestLDAPProvider(LDAPConfigForTest("ldap://127.0.0.1:1"))
	_, err := p.Authenticate(context.Background(), "user", "pass")
	if err == nil {
		t.Fatal("expected error for unreachable LDAP")
	}
}

func TestLDAPProviderAuthenticateInvalidScheme(t *testing.T) {
	p := newTestLDAPProvider(LDAPConfigForTest("not-a-url"))
	_, err := p.Authenticate(context.Background(), "user", "pass")
	if err == nil {
		t.Fatal("expected error for invalid LDAP URL scheme")
	}
}

func TestLDAPProviderAuthenticateBindFailure(t *testing.T) {
	// Start a mock LDAP server that responds to Bind with an error result
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleLDAPBindError(conn)
		}
	}()

	cfg := config.LDAPConfig{
		URL:          fmt.Sprintf("ldap://%s", ln.Addr().String()),
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
		UserFilter:   "(uid=%s)",
	}
	p := newTestLDAPProvider(cfg)
	_, err = p.Authenticate(context.Background(), "user", "pass")
	if err == nil {
		t.Fatal("expected error for bind failure")
	}
	if !strings.Contains(err.Error(), "ldap") {
		t.Errorf("error = %q, want ldap-related error", err.Error())
	}
}

// handleLDAPBindError reads an LDAP bind request and responds with
// an LDAP BindResponse indicating "invalid credentials" (result code 49).
func handleLDAPBindError(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}

	// Parse the message ID from the request (BER-encoded)
	// LDAP message: SEQUENCE { messageID INTEGER, protocolOp ... }
	// We need to extract the messageID to echo it back
	msgID := byte(1)
	if n > 5 {
		// Simple BER parse: skip SEQUENCE tag+length, read INTEGER tag+length+value
		offset := 2 // skip SEQUENCE tag + 1-byte length (simplified)
		if buf[0] == 0x30 {
			if buf[1]&0x80 != 0 {
				// multi-byte length
				lenBytes := int(buf[1] & 0x7f)
				offset = 2 + lenBytes
			}
			if offset < n && buf[offset] == 0x02 { // INTEGER tag
				intLen := int(buf[offset+1])
				if intLen == 1 && offset+2 < n {
					msgID = buf[offset+2]
				}
			}
		}
	}

	// Build LDAP BindResponse with result code 49 (invalidCredentials)
	// BindResponse ::= [APPLICATION 1] SEQUENCE {
	//   resultCode ENUMERATED (49),
	//   matchedDN LDAPDN (""),
	//   diagnosticMessage LDAPString ("invalid credentials")
	// }
	diagMsg := []byte("invalid credentials")
	bindRespValue := []byte{
		0x0a, 0x01, 49,   // ENUMERATED resultCode = 49
		0x04, 0x00,        // OCTET STRING matchedDN = ""
		0x04, byte(len(diagMsg)), // OCTET STRING diagnosticMessage
	}
	bindRespValue = append(bindRespValue, diagMsg...)

	bindResp := []byte{0x61, byte(len(bindRespValue))} // APPLICATION 1
	bindResp = append(bindResp, bindRespValue...)

	msgIDBytes := []byte{0x02, 0x01, msgID} // INTEGER messageID
	seqValue := append(msgIDBytes, bindResp...)
	msg := []byte{0x30, byte(len(seqValue))} // SEQUENCE
	msg = append(msg, seqValue...)

	conn.Write(msg)
}

func TestLDAPProviderAuthenticateBindSuccessSearchFail(t *testing.T) {
	// Mock LDAP server: bind succeeds, search fails
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleLDAPBindOKSearchFail(conn)
		}
	}()

	cfg := config.LDAPConfig{
		URL:          fmt.Sprintf("ldap://%s", ln.Addr().String()),
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
		UserFilter:   "(uid=%s)",
	}
	p := newTestLDAPProvider(cfg)
	_, err = p.Authenticate(context.Background(), "testuser", "testpass")
	if err == nil {
		t.Fatal("expected error")
	}
}

// handleLDAPBindOKSearchFail responds with bind success, then closes on search.
func handleLDAPBindOKSearchFail(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)

	// Read bind request
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	msgID := extractLDAPMsgID(buf[:n])

	// Send BindResponse success (resultCode = 0)
	conn.Write(buildLDAPBindResponse(msgID, 0, ""))

	// Read search request
	n, err = conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	msgID = extractLDAPMsgID(buf[:n])

	// Send SearchResultDone with error (noSuchObject = 32)
	conn.Write(buildLDAPSearchDone(msgID, 32, "no such object"))
}

func TestLDAPProviderAuthenticateUserNotFound(t *testing.T) {
	// Mock LDAP server: bind succeeds, search returns 0 entries
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleLDAPBindOKSearchEmpty(conn)
		}
	}()

	cfg := config.LDAPConfig{
		URL:          fmt.Sprintf("ldap://%s", ln.Addr().String()),
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
		UserFilter:   "(uid=%s)",
	}
	p := newTestLDAPProvider(cfg)
	_, err = p.Authenticate(context.Background(), "nobody", "pass")
	if err == nil {
		t.Fatal("expected error for user not found")
	}
	if !strings.Contains(err.Error(), "user not found") {
		t.Errorf("error = %q, want 'user not found'", err.Error())
	}
}

// handleLDAPBindOKSearchEmpty responds with bind success, then empty search result.
func handleLDAPBindOKSearchEmpty(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)

	// Read bind request
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	msgID := extractLDAPMsgID(buf[:n])

	// Send BindResponse success
	conn.Write(buildLDAPBindResponse(msgID, 0, ""))

	// Read search request
	n, err = conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	msgID = extractLDAPMsgID(buf[:n])

	// Send SearchResultDone with success but no entries
	conn.Write(buildLDAPSearchDone(msgID, 0, ""))
}

// ---------------------------------------------------------------------------
// LDAP BER encoding helpers
// ---------------------------------------------------------------------------

func extractLDAPMsgID(data []byte) byte {
	if len(data) < 6 && data[0] == 0x30 {
		return 1
	}
	offset := 2
	if data[1]&0x80 != 0 {
		offset = 2 + int(data[1]&0x7f)
	}
	if offset+2 < len(data) && data[offset] == 0x02 && data[offset+1] == 0x01 {
		return data[offset+2]
	}
	return 1
}

func buildLDAPBindResponse(msgID byte, resultCode byte, diag string) []byte {
	diagBytes := []byte(diag)
	bindRespValue := []byte{
		0x0a, 0x01, resultCode, // ENUMERATED resultCode
		0x04, 0x00,             // OCTET STRING matchedDN = ""
		0x04, byte(len(diagBytes)), // OCTET STRING diagnosticMessage
	}
	bindRespValue = append(bindRespValue, diagBytes...)

	bindResp := []byte{0x61, byte(len(bindRespValue))} // APPLICATION 1 = BindResponse
	bindResp = append(bindResp, bindRespValue...)

	msgIDBytes := []byte{0x02, 0x01, msgID}
	seqValue := append(msgIDBytes, bindResp...)
	msg := []byte{0x30, byte(len(seqValue))}
	msg = append(msg, seqValue...)
	return msg
}

func buildLDAPSearchDone(msgID byte, resultCode byte, diag string) []byte {
	diagBytes := []byte(diag)
	doneValue := []byte{
		0x0a, 0x01, resultCode,
		0x04, 0x00,
		0x04, byte(len(diagBytes)),
	}
	doneValue = append(doneValue, diagBytes...)

	done := []byte{0x65, byte(len(doneValue))} // APPLICATION 5 = SearchResultDone
	done = append(done, doneValue...)

	msgIDBytes := []byte{0x02, 0x01, msgID}
	seqValue := append(msgIDBytes, done...)
	msg := []byte{0x30, byte(len(seqValue))}
	msg = append(msg, seqValue...)
	return msg
}

// LDAPConfigForTest creates a minimal LDAPConfig with the given URL.
func LDAPConfigForTest(url string) config.LDAPConfig {
	return config.LDAPConfig{
		URL:          url,
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
		UserFilter:   "(uid=%s)",
	}
}

// ---------------------------------------------------------------------------
// SSO provider additional tests (already well-covered, add edge case)
// ---------------------------------------------------------------------------

func TestSSOProviderWithRelayProviderReturnsNil(t *testing.T) {
	// SSO with a relay provider that returns ErrInvalidCredentials should return nil,nil
	mock := &mockRelayProvider{authErr: relay.ErrInvalidCredentials}
	p := NewSSOProvider(mock, zap.NewNop())
	info, err := p.Authenticate(context.Background(), "anyuser", "anypass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil UserInfo for invalid credentials")
	}
}

// ---------------------------------------------------------------------------
// Login integration: creates user in DB
// ---------------------------------------------------------------------------

func TestLoginCreatesLocalUser(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "newlogin",
			Email:      "newlogin@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	_, info, err := svc.Login(ctx, LoginRequest{
		Username: "newlogin",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if info.ID == 0 {
		t.Error("user ID should be set after login")
	}

	// Verify user was created in DB
	u, err := client.User.Get(ctx, info.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Username != "newlogin" {
		t.Errorf("db username = %q, want %q", u.Username, "newlogin")
	}
}

func TestLoginReusesExistingUser(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	// Pre-create user
	existing, _ := client.User.Create().
		SetUsername("reuse").
		SetEmail("reuse@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleAdmin).
		Save(ctx)

	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "reuse",
			Email:      "reuse@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	_, info, err := svc.Login(ctx, LoginRequest{
		Username: "reuse",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if info.ID != existing.ID {
		t.Errorf("user ID = %d, want %d (existing)", info.ID, existing.ID)
	}
	// Role should be synced from provider on each login
	if info.Role != "user" {
		t.Errorf("role = %q, want 'user' (synced from provider)", info.Role)
	}
}

// ---------------------------------------------------------------------------
// RefreshToken edge cases
// ---------------------------------------------------------------------------

func TestRefreshTokenBadClaims(t *testing.T) {
	svc := newTestService()

	// Manually create a refresh token with missing user_id
	now := time.Now()
	claims := jwt.MapClaims{
		"type": "refresh",
		"iat":  now.Unix(),
		"exp":  now.Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString(svc.jwtSecret)

	_, _, err := svc.RefreshToken(context.Background(), tokenStr)
	if err == nil {
		t.Fatal("expected error for missing user_id claim")
	}
}

// ---------------------------------------------------------------------------
// Login with specific provider that returns error
// ---------------------------------------------------------------------------

func TestLoginSpecificProviderReturnsNilNil(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	// Provider returns nil, nil (skip) but it's the only one for the specified source
	svc.RegisterProvider(&mockAuthProvider{
		name:     "ldap",
		userInfo: nil,
		err:      nil,
	})

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "user",
		Password: "pass",
		Source:   "ldap",
	})
	if err == nil {
		t.Fatal("expected error when specific provider returns nil,nil")
	}
}

// ---------------------------------------------------------------------------
// validateToken edge cases
// ---------------------------------------------------------------------------

func TestValidateTokenInvalidClaims(t *testing.T) {
	svc := newTestService()

	// Create a token with valid signature but wrong type
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": 1,
		"type":    "access",
		"iat":     now.Unix(),
		"exp":     now.Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString(svc.jwtSecret)

	// Try to validate as refresh — should fail on type mismatch
	_, err := svc.validateToken(tokenStr, "refresh")
	if err == nil {
		t.Fatal("expected error for wrong token type")
	}
}

// ---------------------------------------------------------------------------
// LDAP with mock TCP server for deeper coverage
// ---------------------------------------------------------------------------

func TestLDAPProviderAuthenticateDialSuccess_BindFail(t *testing.T) {
	// Start a TCP listener that accepts but immediately closes
	// This tests the path where DialURL succeeds but Bind fails
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Close after a tiny delay to let the LDAP client connect
			time.Sleep(10 * time.Millisecond)
			conn.Close()
		}
	}()

	cfg := config.LDAPConfig{
		URL:          fmt.Sprintf("ldap://%s", ln.Addr().String()),
		BaseDN:       "dc=test,dc=com",
		BindDN:       "cn=admin,dc=test,dc=com",
		BindPassword: "secret",
		UserFilter:   "(uid=%s)",
		TLS:          false,
	}
	p := newTestLDAPProvider(cfg)
	_, err = p.Authenticate(context.Background(), "testuser", "testpass")
	if err == nil {
		t.Fatal("expected error when LDAP bind fails")
	}
	// Should be a bind error, not a dial error
	if !strings.Contains(err.Error(), "ldap") {
		t.Errorf("error = %q, want ldap-related error", err.Error())
	}
}

func TestLDAPProviderAuthenticateWithTLSConfig(t *testing.T) {
	// Start a TCP listener — TLS StartTLS will fail since it's plain TCP
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}
	}()

	cfg := config.LDAPConfig{
		URL:          fmt.Sprintf("ldap://%s", ln.Addr().String()),
		BaseDN:       "dc=test,dc=com",
		BindDN:       "cn=admin,dc=test,dc=com",
		BindPassword: "secret",
		UserFilter:   "(uid=%s)",
		TLS:          true, // This will trigger StartTLS which will fail
	}
	p := newTestLDAPProvider(cfg)
	_, err = p.Authenticate(context.Background(), "testuser", "testpass")
	if err == nil {
		t.Fatal("expected error when StartTLS fails")
	}
}

// ---------------------------------------------------------------------------
// ensureLocalUser DB error path
// ---------------------------------------------------------------------------

func TestEnsureLocalUserDBQueryError(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()

	// Close the client to force a DB error
	client.Close()

	info := &UserInfo{
		Username:   "dbfail",
		Email:      "dbfail@example.com",
		Role:       "user",
		AuthSource: "ldap",
	}

	_, err := svc.ensureLocalUser(ctx, info)
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

// ---------------------------------------------------------------------------
// Login with ensureLocalUser failure
// ---------------------------------------------------------------------------

func TestLoginEnsureLocalUserFailure(t *testing.T) {
	svc, client := newTestServiceWithDB(t)

	svc.RegisterProvider(&mockAuthProvider{
		name: "ldap",
		userInfo: &UserInfo{
			Username:   "failuser",
			Email:      "fail@example.com",
			Role:       "user",
			AuthSource: "ldap",
		},
	})

	// Close the DB to force ensureLocalUser to fail
	client.Close()

	_, _, err := svc.Login(context.Background(), LoginRequest{
		Username: "failuser",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error when ensureLocalUser fails")
	}
	if !strings.Contains(err.Error(), "ensure local user") {
		t.Errorf("error = %q, want 'ensure local user'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// RefreshToken with DB error
// ---------------------------------------------------------------------------

func TestRefreshTokenDBError(t *testing.T) {
	svc, client := newTestServiceWithDB(t)

	// Create a user and generate tokens first
	u, _ := client.User.Create().
		SetUsername("dbfailrefresh").
		SetEmail("dbfailrefresh@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(context.Background())

	info := &UserInfo{ID: u.ID, Username: "dbfailrefresh", Role: "user"}
	pair, _ := svc.generateTokenPair(info)

	// Close DB to force error on user lookup
	client.Close()

	_, _, err := svc.RefreshToken(context.Background(), pair.RefreshToken)
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}
