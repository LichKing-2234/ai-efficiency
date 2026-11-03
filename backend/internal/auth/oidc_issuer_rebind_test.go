package auth

import (
	"context"
	"errors"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
)

func TestLoginOIDCRebindsBindingFromAnotherIssuer(t *testing.T) {
	svc, client := newOIDCTestService(t)
	ctx := context.Background()

	const staleIssuer = "https://old-id.example.com/application/o/ai-efficiency/"
	existing := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetOidcIssuer(staleIssuer).
		SetOidcSubject("subject-alice").
		SaveX(ctx)

	tokens, info, err := svc.LoginOIDC(ctx, oidcTestIdentity())
	if err != nil {
		t.Fatalf("LoginOIDC() error: %v", err)
	}
	if tokens == nil || tokens.AccessToken == "" || info == nil {
		t.Fatal("expected a token pair and user info after rebinding")
	}

	reloaded, err := client.User.Get(ctx, existing.ID)
	if err != nil {
		t.Fatalf("load local user: %v", err)
	}
	if reloaded.OidcIssuer == nil || *reloaded.OidcIssuer != oidcTestIdentity().Issuer {
		t.Fatalf("oidc_issuer = %v, want %q", reloaded.OidcIssuer, oidcTestIdentity().Issuer)
	}
	if reloaded.OidcSubject == nil || *reloaded.OidcSubject != oidcTestIdentity().Subject {
		t.Fatalf("oidc_subject = %v, want %q", reloaded.OidcSubject, oidcTestIdentity().Subject)
	}
	if reloaded.AuthSource != entuser.AuthSourceOidc {
		t.Fatalf("auth_source = %q, want oidc", reloaded.AuthSource)
	}
}

func TestLoginOIDCRejectsDifferentSubjectUnderSameIssuer(t *testing.T) {
	svc, client := newOIDCTestService(t)
	ctx := context.Background()
	identity := oidcTestIdentity()

	existing := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceOidc).
		SetOidcIssuer(identity.Issuer).
		SetOidcSubject("subject-someone-else").
		SaveX(ctx)

	if _, _, err := svc.LoginOIDC(ctx, identity); !errors.Is(err, ErrOIDCIdentityConflict) {
		t.Fatalf("LoginOIDC() error = %v, want ErrOIDCIdentityConflict", err)
	}

	reloaded, err := client.User.Get(ctx, existing.ID)
	if err != nil {
		t.Fatalf("load local user: %v", err)
	}
	if reloaded.OidcSubject == nil || *reloaded.OidcSubject != "subject-someone-else" {
		t.Fatalf("conflicting binding was modified: %v", reloaded.OidcSubject)
	}
}

func TestLoginOIDCRejectsUnverifiedEmailEvenWithStaleIssuer(t *testing.T) {
	svc, client := newOIDCTestService(t)
	ctx := context.Background()

	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetOidcIssuer("https://old-id.example.com/application/o/ai-efficiency/").
		SetOidcSubject("subject-alice").
		SaveX(ctx)

	identity := oidcTestIdentity()
	identity.EmailVerified = false
	if _, _, err := svc.LoginOIDC(ctx, identity); !errors.Is(err, ErrOIDCEmailLinkUnavailable) {
		t.Fatalf("LoginOIDC() error = %v, want ErrOIDCEmailLinkUnavailable", err)
	}
}
