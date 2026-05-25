package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// mockEntClient is not needed for JWT tests — we test token logic directly.

func newTestService() *Service {
	svc := &Service{
		jwtSecret:       []byte("test-secret-key-for-unit-tests!!"),
		accessTokenTTL:  2 * time.Hour,
		refreshTokenTTL: 7 * 24 * time.Hour,
		now:             time.Now,
	}
	svc.SetRefreshSessionStore(newMemoryRefreshSessionStore())
	return svc
}

func TestGenerateAndValidateAccessToken(t *testing.T) {
	svc := newTestService()

	info := &UserInfo{
		ID:       1,
		Username: "testuser",
		Role:     "admin",
	}

	pair, err := svc.generateTokenPair(context.Background(), info)
	if err != nil {
		t.Fatalf("generateTokenPair() error = %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("access token should not be empty")
	}
	if pair.RefreshToken == "" {
		t.Error("refresh token should not be empty")
	}
	if pair.ExpiresIn != 7200 {
		t.Errorf("ExpiresIn = %d, want 7200", pair.ExpiresIn)
	}

	// Validate access token
	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if int(claims["user_id"].(float64)) != 1 {
		t.Errorf("user_id = %v, want 1", claims["user_id"])
	}
	if claims["username"] != "testuser" {
		t.Errorf("username = %v, want testuser", claims["username"])
	}
	if claims["role"] != "admin" {
		t.Errorf("role = %v, want admin", claims["role"])
	}
}

func TestValidateAccessTokenRejectsRefreshToken(t *testing.T) {
	svc := newTestService()

	info := &UserInfo{ID: 1, Username: "testuser", Role: "user"}
	pair, _ := svc.generateTokenPair(context.Background(), info)

	// Access token validation should reject a refresh token
	_, err := svc.ValidateAccessToken(pair.RefreshToken)
	if err == nil {
		t.Error("ValidateAccessToken should reject refresh token")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	svc := &Service{
		jwtSecret:       []byte("test-secret"),
		accessTokenTTL:  -1 * time.Hour, // already expired
		refreshTokenTTL: 7 * 24 * time.Hour,
		now:             time.Now,
	}
	svc.SetRefreshSessionStore(newMemoryRefreshSessionStore())

	info := &UserInfo{ID: 1, Username: "testuser", Role: "user"}
	pair, _ := svc.generateTokenPair(context.Background(), info)

	_, err := svc.ValidateAccessToken(pair.AccessToken)
	if err == nil {
		t.Error("should reject expired token")
	}
}

func TestValidateTokenWrongSecret(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 1, Username: "testuser", Role: "user"}
	pair, _ := svc.generateTokenPair(context.Background(), info)

	// Create a service with different secret
	svc2 := &Service{
		jwtSecret: []byte("different-secret-key-32-bytes!!!"),
	}

	_, err := svc2.ValidateAccessToken(pair.AccessToken)
	if err == nil {
		t.Error("should reject token signed with different secret")
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	svc := newTestService()

	_, err := svc.ValidateAccessToken("not-a-valid-token")
	if err == nil {
		t.Error("should reject invalid token string")
	}

	_, err = svc.ValidateAccessToken("")
	if err == nil {
		t.Error("should reject empty token")
	}
}

func TestRefreshTokenIsOpaqueAndAccessTokenRemainsJWT(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 42, Username: "bob", Role: "user"}
	pair, err := svc.generateTokenPair(context.Background(), info)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}

	if !strings.HasPrefix(pair.RefreshToken, refreshTokenPrefix) {
		t.Fatalf("refresh token = %q, want opaque %s prefix", pair.RefreshToken, refreshTokenPrefix)
	}
	if strings.Count(pair.RefreshToken, ".") != 0 {
		t.Fatalf("refresh token must not be a JWT: %q", pair.RefreshToken)
	}
	if _, err := svc.ValidateAccessToken(pair.AccessToken); err != nil {
		t.Fatalf("access token should remain a valid JWT: %v", err)
	}
}

// mockAuthProvider implements AuthProvider for testing.
type mockAuthProvider struct {
	name     string
	userInfo *UserInfo
	err      error
}

func (m *mockAuthProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	return m.userInfo, m.err
}

func (m *mockAuthProvider) Name() string {
	return m.name
}

func TestTokenClaimsContainCorrectType(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 1, Username: "test", Role: "user"}
	pair, _ := svc.generateTokenPair(context.Background(), info)

	// Parse access token without validation to check claims
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(pair.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	claims := token.Claims.(jwt.MapClaims)
	if claims["type"] != "access" {
		t.Errorf("access token type = %v, want 'access'", claims["type"])
	}

	if !strings.HasPrefix(pair.RefreshToken, refreshTokenPrefix) {
		t.Errorf("refresh token = %q, want opaque token prefix", pair.RefreshToken)
	}
	if strings.Count(pair.RefreshToken, ".") != 0 {
		t.Errorf("refresh token should not be a JWT: %q", pair.RefreshToken)
	}
}
