package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

const defaultRelayUserConcurrency = 5

type relayIdentityAPI interface {
	FindUserByUsername(ctx context.Context, username string) (*relay.User, error)
	CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error)
}

type relayIdentityPasswordUpdater interface {
	UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error)
}

// RelayIdentityResolver resolves a relay user by a stable username key and provisions one if missing.
// Intended for LDAP logins where we don't have relay-side SSO identity.
type RelayIdentityResolver struct {
	api            relayIdentityAPI
	fallbackDomain string
}

func NewRelayIdentityResolver(api relayIdentityAPI, fallbackDomain string) *RelayIdentityResolver {
	return &RelayIdentityResolver{
		api:            api,
		fallbackDomain: strings.TrimSpace(fallbackDomain),
	}
}

func (r *RelayIdentityResolver) ResolveOrProvision(ctx context.Context, username, email string) (*relay.User, error) {
	u, _, err := r.ResolveOrProvisionWithPassword(ctx, username, email, "")
	return u, err
}

func (r *RelayIdentityResolver) ResolveOrProvisionWithPassword(ctx context.Context, username, email, password string) (*relay.User, string, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if username == "" {
		return nil, "", fmt.Errorf("relay identity: username is required")
	}
	canonicalUsername := relayProvisionUsername(username)
	if canonicalUsername == "" {
		return nil, "", fmt.Errorf("relay identity: canonical username is required")
	}

	u, err := r.api.FindUserByUsername(ctx, canonicalUsername)
	if err != nil {
		return nil, "", fmt.Errorf("relay identity: find user by username: %w", err)
	}
	foundByLegacyUsername := false
	if u == nil && canonicalUsername != username {
		u, err = r.api.FindUserByUsername(ctx, username)
		if err != nil {
			return nil, "", fmt.Errorf("relay identity: find legacy user by username: %w", err)
		}
		foundByLegacyUsername = u != nil
	}
	if u != nil {
		updateReq, shouldUpdate := relayUserUpdateForLDAP(u, canonicalUsername, password, foundByLegacyUsername)
		if !shouldUpdate {
			return u, "", nil
		}
		if updater, ok := r.api.(relayIdentityPasswordUpdater); ok {
			updated, err := updater.UpdateUser(ctx, u.ID, updateReq)
			if err != nil {
				return nil, "", fmt.Errorf("relay identity: update user: %w", err)
			}
			return updated, password, nil
		}
		return u, "", nil
	}

	email = ensureNonEmptyEmail(email, username, r.fallbackDomain)

	pw := password
	if pw == "" {
		pw, err = highEntropyPassword()
		if err != nil {
			return nil, "", fmt.Errorf("relay identity: generate password: %w", err)
		}
	}

	created, err := r.api.CreateUser(ctx, relay.CreateUserRequest{
		Username:    canonicalUsername,
		Email:       email,
		Password:    pw,
		Notes:       "provisioned_by_ai_efficiency_ldap",
		Concurrency: defaultRelayUserConcurrency,
	})
	if err != nil {
		return nil, "", fmt.Errorf("relay identity: create user: %w", err)
	}
	return created, pw, nil
}

func relayProvisionUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	if i := strings.Index(username, "@"); i > 0 {
		return strings.TrimSpace(username[:i])
	}
	return username
}

func relayUserUpdateForLDAP(u *relay.User, canonicalUsername, password string, foundByLegacyUsername bool) (relay.UpdateUserRequest, bool) {
	var req relay.UpdateUserRequest
	if password != "" {
		req.Password = password
	}
	if foundByLegacyUsername && strings.TrimSpace(u.Username) != canonicalUsername {
		req.Username = canonicalUsername
	}
	if u != nil && u.Concurrency < defaultRelayUserConcurrency {
		concurrency := defaultRelayUserConcurrency
		req.Concurrency = &concurrency
	}
	if req.Password == "" && req.Username == "" && req.Concurrency == nil {
		return relay.UpdateUserRequest{}, false
	}
	return req, true
}

func highEntropyPassword() (string, error) {
	// 32 bytes => 43 chars base64url (no padding), plenty for a one-time provisioning password.
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
