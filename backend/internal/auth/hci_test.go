package auth

import (
	"context"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		"exp":  time.Now().Add(time.Minute).Unix(),
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
	req.Header.Set("X-AE-HCI-Proxy-Secret", strings.Repeat("p", 32))
	req.Header.Set("X-Consumer-Username", "alice@example.com")
	info, err := NewHCIProvider("", strings.Repeat("p", 32)).AuthenticateHeaders(req)
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "alice@example.com" || info.AuthSource != "hci" {
		t.Fatalf("identity = %+v", info)
	}
}

func TestHCIProviderRejectsWrongSecret(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":  time.Now().Add(time.Minute).Unix(),
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

func TestHCIHeaderTrustBoundary(t *testing.T) {
	for _, tc := range []struct{ name, proof, consumer string }{
		{"direct service", "", "agora-hciauth-v1"},
		{"forged proof", "wrong", "agora-hciauth-v1"},
		{"key-auth consumer", strings.Repeat("p", 32), "api-client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/auth/hci/bootstrap", nil)
			req.Header.Set("x-oauth-email", "admin@example.com")
			req.Header.Set("X-AE-HCI-Proxy-Secret", tc.proof)
			req.Header.Set("X-Consumer-Username", tc.consumer)
			if _, err := NewHCIProvider("", strings.Repeat("p", 32)).AuthenticateHeaders(req); err == nil {
				t.Fatal("forged identity accepted")
			}
		})
	}
	req := httptest.NewRequest("POST", "/auth/hci/bootstrap", nil)
	req.Header.Set("x-oauth-email", "alice@example.com")
	if _, err := NewHCIProvider(strings.Repeat("s", 32)).AuthenticateHeaders(req); err == nil {
		t.Fatal("missing JWT downgraded to header mode")
	}
}

func TestHCILocalIdentityPreservesAdminAndRelay(t *testing.T) {
	svc, db := newTestServiceWithDB(t)
	ctx := context.Background()
	u := db.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetRole(entuser.RoleAdmin).SetAuthSource(entuser.AuthSourceLdap).SetRelayUserID(42).SetRelayAuthPassword("encrypted-fixture").SaveX(ctx)
	if err := svc.ConfigureHCI(true, "", strings.Repeat("p", 32), "https://app.example.com"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/auth/hci/bootstrap", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Oauth-Email", "alice@example.com")
	req.Header.Set("X-AE-HCI-Proxy-Secret", strings.Repeat("p", 32))
	req.Header.Set("X-Consumer-Username", "alice@example.com")
	pair, info, err := svc.LoginHCI(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != u.ID || info.Role != "admin" || info.Username != "alice" {
		t.Fatalf("identity changed: %+v", info)
	}
	got := db.User.GetX(ctx, u.ID)
	if *got.RelayUserID != 42 || *got.RelayAuthPassword != "encrypted-fixture" || got.AuthSource != entuser.AuthSourceHci {
		t.Fatal("Relay credentials or HCI binding changed incorrectly")
	}
	if _, err = svc.ValidateAccessToken(ctx, pair.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.RefreshToken(ctx, pair.RefreshToken); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example.org")
	if _, _, err = svc.LoginHCI(ctx, req); err == nil {
		t.Fatal("cross-site session issuance accepted")
	}
	req.Header.Set("Origin", "https://app.example.com")
	db.User.UpdateOneID(u.ID).SetRelayDisabledAt(time.Now()).SaveX(ctx)
	if _, _, err = svc.LoginHCI(ctx, req); err == nil {
		t.Fatal("disabled account accepted")
	}
}

func TestHCIIdentityDoesNotMatchAnotherUsersUsername(t *testing.T) {
	svc, db := newTestServiceWithDB(t)
	ctx := context.Background()
	db.User.Create().SetUsername("alice@example.com").SetEmail("bob@example.org").SetAuthSource(entuser.AuthSourceLdap).SaveX(ctx)
	if _, err := svc.hciLocalUser(ctx, "alice@example.com"); err == nil {
		t.Fatal("linked HCI identity using another account username")
	}
	u, err := svc.hciLocalUser(ctx, "charlie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != entuser.RoleUser || u.RelayUserID != nil {
		t.Fatal("new HCI user must not provision or inherit Relay/admin")
	}
}

func TestHCIConfigurationFailsClosed(t *testing.T) {
	svc := &Service{}
	if err := svc.ConfigureHCI(true, "", "", "https://app.example.com"); err == nil {
		t.Fatal("unverified header mode enabled")
	}
	if svc.HCIEnabled() {
		t.Fatal("enabled after invalid config")
	}
	if err := svc.ConfigureHCI(false, "", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestHCIUsesAuthenticatedEmailAsKongConsumer(t *testing.T) {
	req := httptest.NewRequest("POST", "/auth/hci/bootstrap", nil)
	req.Header.Set("X-AE-HCI-Proxy-Secret", strings.Repeat("p", 32))
	req.Header.Set("X-Consumer-Username", "alice@example.com")
	req.Header.Set("X-Oauth-Email", "alice@example.com")
	info, err := NewHCIProvider("", strings.Repeat("p", 32)).AuthenticateHeaders(req)
	if err != nil || info.Email != "alice@example.com" {
		t.Fatalf("HCI email Consumer rejected: %v", err)
	}
	req.Header.Set("X-Oauth-Email", "bob@example.org")
	if _, err = NewHCIProvider("", strings.Repeat("p", 32)).AuthenticateHeaders(req); err == nil {
		t.Fatal("mismatched Consumer/email accepted")
	}
}
