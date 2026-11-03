package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/golang-jwt/jwt/v5"
)

func newLogoutTestService(t *testing.T) (*OIDCService, *rsa.PrivateKey, string) {
	t.Helper()
	return newLogoutTestServiceWithStore(t, nil)
}

func newLogoutTestServiceWithStore(t *testing.T, state authstate.Store) (*OIDCService, *rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"userinfo_endpoint":      serverURL + "/userinfo",
			"jwks_uri":               serverURL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "test-key",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	serverURL = server.URL
	service, err := NewOIDCServiceWithStore(context.Background(), OIDCConfig{
		Issuer:       serverURL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  serverURL + "/auth/oidc/callback",
	}, state)
	if err != nil {
		t.Fatalf("build OIDC service: %v", err)
	}
	return service, privateKey, serverURL
}

func signLogoutToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign logout token: %v", err)
	}
	return signed
}

func logoutClaims(issuer, jti string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":    issuer,
		"aud":    "client-id",
		"iat":    now.Unix(),
		"exp":    now.Add(time.Minute).Unix(),
		"jti":    jti,
		"sub":    "hashed-user-1",
		"sid":    "hashed-session-1",
		"events": map[string]any{backchannelLogoutEvent: map[string]any{}},
	}
}

func TestVerifyLogoutTokenAcceptsValidToken(t *testing.T) {
	service, key, issuer := newLogoutTestService(t)
	identity, err := service.VerifyLogoutToken(context.Background(), signLogoutToken(t, key, logoutClaims(issuer, "jti-valid")))
	if err != nil {
		t.Fatalf("VerifyLogoutToken() error = %v", err)
	}
	if identity.Issuer != issuer || identity.Subject != "hashed-user-1" || identity.SessionID != "hashed-session-1" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestVerifyLogoutTokenAcceptsAudienceArray(t *testing.T) {
	service, key, issuer := newLogoutTestService(t)
	claims := logoutClaims(issuer, "jti-audience")
	claims["aud"] = []string{"other-client", "client-id"}
	if _, err := service.VerifyLogoutToken(context.Background(), signLogoutToken(t, key, claims)); err != nil {
		t.Fatalf("VerifyLogoutToken() error = %v", err)
	}
}

func TestVerifyLogoutTokenRejectsReplay(t *testing.T) {
	service, key, issuer := newLogoutTestService(t)
	raw := signLogoutToken(t, key, logoutClaims(issuer, "jti-replayed"))
	if _, err := service.VerifyLogoutToken(context.Background(), raw); err != nil {
		t.Fatalf("first VerifyLogoutToken() error = %v", err)
	}
	if _, err := service.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("second VerifyLogoutToken() succeeded, want rejection")
	}
}

func TestVerifyLogoutTokenRejectsInvalidClaims(t *testing.T) {
	cases := map[string]func(claims jwt.MapClaims){
		"issuer":        func(claims jwt.MapClaims) { claims["iss"] = "https://other.example.test/" },
		"audience":      func(claims jwt.MapClaims) { claims["aud"] = "other-client" },
		"events":        func(claims jwt.MapClaims) { claims["events"] = map[string]any{"other-event": map[string]any{}} },
		"nonce":         func(claims jwt.MapClaims) { claims["nonce"] = "nonce-value" },
		"subject":       func(claims jwt.MapClaims) { delete(claims, "sub") },
		"jti":           func(claims jwt.MapClaims) { delete(claims, "jti") },
		"issued-at-old": func(claims jwt.MapClaims) { claims["iat"] = time.Now().Add(-time.Hour).Unix() },
		"expired":       func(claims jwt.MapClaims) { claims["exp"] = time.Now().Add(-time.Minute).Unix() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			service, key, issuer := newLogoutTestService(t)
			claims := logoutClaims(issuer, "jti-"+name)
			mutate(claims)
			if _, err := service.VerifyLogoutToken(context.Background(), signLogoutToken(t, key, claims)); err == nil {
				t.Fatal("VerifyLogoutToken() succeeded, want rejection")
			}
		})
	}
}

func TestVerifyLogoutTokenRejectsBadSignature(t *testing.T) {
	service, _, issuer := newLogoutTestService(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if _, err := service.VerifyLogoutToken(context.Background(), signLogoutToken(t, otherKey, logoutClaims(issuer, "jti-bad-signature"))); err == nil {
		t.Fatal("VerifyLogoutToken() succeeded, want rejection")
	}
}
