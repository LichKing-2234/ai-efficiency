package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestHCIProviderRejectsMissingHeader(t *testing.T) {
	p := NewHCIProvider("test-hci-secret")
	if _, err := p.AuthenticateHeaders(httptest.NewRequest("GET", "/", nil)); err == nil {
		t.Fatal("expected missing HCI header error")
	}
}

func TestHCIProviderReadsSignedEmail(t *testing.T) {
	secret := "test-hci-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": map[string]any{"email": "alice@example.com", "displayName": "Alice"},
	})
	raw, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("x-hci-auth-jwt", raw)
	info, err := NewHCIProvider(secret).AuthenticateHeaders(req)
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "alice@example.com" || info.AuthSource != "hci" {
		t.Fatalf("identity = %+v", info)
	}
}

func TestHCIProviderReadsClassicHeaderWithoutVerificationKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("x-oauth-email", "alice@example.com")
	info, err := NewHCIProvider("").AuthenticateHeaders(req)
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "alice@example.com" || info.AuthSource != "hci" {
		t.Fatalf("identity = %+v", info)
	}
}

func TestHCIProviderRejectsWrongSecret(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": map[string]any{"email": "alice@example.com"},
	})
	raw, err := token.SignedString([]byte("one-secret"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("x-hci-auth-jwt", raw)
	if _, err := NewHCIProvider("another-secret").AuthenticateHeaders(req); err == nil {
		t.Fatal("expected signature validation error")
	}
}
