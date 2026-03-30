package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

type relayIdentityAPI interface {
	FindUserByUsername(ctx context.Context, username string) (*relay.User, error)
	CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error)
}

// RelayIdentityResolver resolves a relay user by a stable username key and provisions one if missing.
// Intended for LDAP logins where we don't have relay-side SSO identity.
type RelayIdentityResolver struct {
	api            relayIdentityAPI
	fallbackDomain string
}

func NewRelayIdentityResolver(api relayIdentityAPI, fallbackDomain string) *RelayIdentityResolver {
	if fallbackDomain == "" {
		fallbackDomain = "ldap.local"
	}
	return &RelayIdentityResolver{
		api:            api,
		fallbackDomain: fallbackDomain,
	}
}

func (r *RelayIdentityResolver) ResolveOrProvision(ctx context.Context, username, email string) (*relay.User, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" {
		return nil, fmt.Errorf("relay identity: username is required")
	}

	u, err := r.api.FindUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("relay identity: find user by username: %w", err)
	}
	if u != nil {
		return u, nil
	}

	if email == "" {
		email = username + "@" + r.fallbackDomain
	}

	pw, err := highEntropyPassword()
	if err != nil {
		return nil, fmt.Errorf("relay identity: generate password: %w", err)
	}

	created, err := r.api.CreateUser(ctx, relay.CreateUserRequest{
		Username: username,
		Email:    email,
		Password: pw,
		Notes:    "provisioned_by_ai_efficiency_ldap",
	})
	if err != nil {
		return nil, fmt.Errorf("relay identity: create user: %w", err)
	}
	return created, nil
}

func highEntropyPassword() (string, error) {
	// 32 bytes => 43 chars base64url (no padding), plenty for a one-time provisioning password.
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
