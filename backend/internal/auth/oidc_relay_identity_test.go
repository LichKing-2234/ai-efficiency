package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

const oidcTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

func newOIDCTestService(t *testing.T) (*Service, *ent.Client) {
	t.Helper()
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", 7200, 604800, zap.NewNop(), oidcTestEncryptionKey)
	svc.SetOIDCService(&OIDCService{config: OIDCConfig{AllowEmailLink: true}})
	return svc, client
}

func oidcTestIdentity() OIDCIdentity {
	return OIDCIdentity{
		Issuer:        "https://id.example.com/application/o/ai-efficiency/",
		Subject:       "subject-alice",
		Email:         "alice@example.com",
		EmailVerified: true,
		Username:      "alice",
	}
}

func TestLoginOIDCProvisionsRelayIdentityForNewUser(t *testing.T) {
	svc, client := newOIDCTestService(t)
	api := &fakeRelayIdentityAPI{}
	svc.SetRelayIdentityResolver(NewRelayIdentityResolver(api, ""))

	tokens, info, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err != nil {
		t.Fatalf("LoginOIDC() error: %v", err)
	}
	if tokens == nil || tokens.AccessToken == "" {
		t.Fatal("expected token pair from OIDC sign-in")
	}
	if info == nil || info.RelayUserID == nil || *info.RelayUserID != 123 {
		t.Fatalf("info.RelayUserID = %+v, want 123", info)
	}
	if info.AuthSource != "oidc" {
		t.Fatalf("info.AuthSource = %q, want oidc", info.AuthSource)
	}
	if len(api.createUserCalls) != 1 {
		t.Fatalf("expected CreateUser called once, got %d", len(api.createUserCalls))
	}
	created := api.createUserCalls[0]
	if created.Username != "alice" {
		t.Fatalf("created relay username = %q, want alice", created.Username)
	}
	if created.Email != "alice@example.com" {
		t.Fatalf("created relay email = %q, want alice@example.com", created.Email)
	}
	if created.Password == "" {
		t.Fatal("expected generated relay password")
	}
	if created.Notes != relayOIDCProvisioningNote {
		t.Fatalf("created relay notes = %q, want %q", created.Notes, relayOIDCProvisioningNote)
	}

	u, err := client.User.Query().Where(entuser.OidcSubjectEQ("subject-alice")).Only(context.Background())
	if err != nil {
		t.Fatalf("load local user: %v", err)
	}
	if u.RelayUserID == nil || *u.RelayUserID != 123 {
		t.Fatalf("stored relay_user_id = %+v, want 123", u.RelayUserID)
	}
	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		t.Fatal("expected stored encrypted relay password")
	}
	if *u.RelayAuthPassword == created.Password {
		t.Fatal("stored relay password must be encrypted")
	}
	decrypted, err := svc.DecryptRelayAuthPassword(*u.RelayAuthPassword)
	if err != nil {
		t.Fatalf("DecryptRelayAuthPassword() error: %v", err)
	}
	if decrypted != created.Password {
		t.Fatal("stored relay password does not match the generated provisioning password")
	}
}

func TestLoginOIDCKeepsExistingRelayBinding(t *testing.T) {
	svc, client := newOIDCTestService(t)
	relayID := 42
	if _, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceOidc).
		SetRole(entuser.RoleUser).
		SetOidcIssuer(oidcTestIdentity().Issuer).
		SetOidcSubject(oidcTestIdentity().Subject).
		SetRelayUserID(relayID).
		Save(context.Background()); err != nil {
		t.Fatalf("create local user: %v", err)
	}
	api := &fakeRelayIdentityAPI{}
	svc.SetRelayIdentityResolver(NewRelayIdentityResolver(api, ""))

	_, info, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err != nil {
		t.Fatalf("LoginOIDC() error: %v", err)
	}
	if info.RelayUserID == nil || *info.RelayUserID != relayID {
		t.Fatalf("info.RelayUserID = %+v, want %d", info.RelayUserID, relayID)
	}
	if len(api.findByUsernameCalls) != 0 || len(api.createUserCalls) != 0 {
		t.Fatalf("expected no relay resolution for a bound user, got lookups=%v creates=%v", api.findByUsernameCalls, api.createUserCalls)
	}
}

func TestLoginOIDCLinksRelayIdentityForExistingLocalUserWithoutBinding(t *testing.T) {
	svc, client := newOIDCTestService(t)
	if _, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceOidc).
		SetRole(entuser.RoleUser).
		SetOidcIssuer(oidcTestIdentity().Issuer).
		SetOidcSubject(oidcTestIdentity().Subject).
		Save(context.Background()); err != nil {
		t.Fatalf("create local user: %v", err)
	}
	api := &emailFirstRelayIdentityAPI{
		emailResult: &relay.User{ID: 77, Username: "alice", Email: "alice@example.com", Concurrency: defaultRelayUserConcurrency},
	}
	svc.SetRelayIdentityResolver(NewRelayIdentityResolver(api, ""))

	_, info, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err != nil {
		t.Fatalf("LoginOIDC() error: %v", err)
	}
	if info.RelayUserID == nil || *info.RelayUserID != 77 {
		t.Fatalf("info.RelayUserID = %+v, want 77", info.RelayUserID)
	}
	if len(api.findByEmailCalls) != 1 || api.findByEmailCalls[0] != "alice@example.com" {
		t.Fatalf("expected one exact email lookup, got %+v", api.findByEmailCalls)
	}
	if len(api.createUserCalls) != 0 {
		t.Fatalf("expected no relay user creation when an email match exists, got %d", len(api.createUserCalls))
	}
	u, err := client.User.Query().Where(entuser.OidcSubjectEQ("subject-alice")).Only(context.Background())
	if err != nil {
		t.Fatalf("load local user: %v", err)
	}
	if u.RelayUserID == nil || *u.RelayUserID != 77 {
		t.Fatalf("stored relay_user_id = %+v, want 77", u.RelayUserID)
	}
	if u.RelayAuthPassword != nil {
		t.Fatalf("expected no stored relay password for an existing relay user, got %q", *u.RelayAuthPassword)
	}
}

func TestLoginOIDCFailsWhenRelayProvisioningFails(t *testing.T) {
	svc, client := newOIDCTestService(t)
	svc.SetRelayIdentityResolver(NewRelayIdentityResolver(relayIdentityLookupAPI{
		findFn: func(context.Context, string) (*relay.User, error) { return nil, nil },
		createFn: func(context.Context, relay.CreateUserRequest) (*relay.User, error) {
			return nil, errors.New("relay unavailable")
		},
		updateFn: func(context.Context, int64, relay.UpdateUserRequest) (*relay.User, error) { return nil, nil },
	}, ""))

	_, _, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err == nil || !strings.Contains(err.Error(), "relay identity") {
		t.Fatalf("LoginOIDC() error = %v, want a relay identity error", err)
	}

	u, loadErr := client.User.Query().Where(entuser.OidcSubjectEQ("subject-alice")).Only(context.Background())
	if loadErr != nil {
		t.Fatalf("expected the local identity to exist after a failed provision, got %v", loadErr)
	}
	if u.RelayUserID != nil {
		t.Fatalf("stored relay_user_id = %+v, want nil", u.RelayUserID)
	}
}

func TestLoginOIDCDoesNotProvisionRelayIdentityForDisabledAccount(t *testing.T) {
	svc, client := newOIDCTestService(t)
	if _, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceOidc).
		SetRole(entuser.RoleUser).
		SetOidcIssuer(oidcTestIdentity().Issuer).
		SetOidcSubject(oidcTestIdentity().Subject).
		SetRelayDisabledAt(time.Now()).
		Save(context.Background()); err != nil {
		t.Fatalf("create local user: %v", err)
	}
	api := &fakeRelayIdentityAPI{}
	svc.SetRelayIdentityResolver(NewRelayIdentityResolver(api, ""))

	_, _, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("LoginOIDC() error = %v, want a disabled account error", err)
	}
	if len(api.findByUsernameCalls) != 0 || len(api.createUserCalls) != 0 {
		t.Fatalf("expected no relay resolution for a disabled account, got lookups=%v creates=%v", api.findByUsernameCalls, api.createUserCalls)
	}
}

func TestLoginOIDCWithoutResolverSkipsRelayProvisioning(t *testing.T) {
	svc, _ := newOIDCTestService(t)

	_, info, err := svc.LoginOIDC(context.Background(), oidcTestIdentity())
	if err != nil {
		t.Fatalf("LoginOIDC() error: %v", err)
	}
	if info.RelayUserID != nil {
		t.Fatalf("info.RelayUserID = %+v, want nil", info.RelayUserID)
	}
}
