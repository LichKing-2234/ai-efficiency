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
const relayLDAPProvisioningNote = "provisioned_by_ai_efficiency_ldap"

type relayIdentityAPI interface {
	FindUserByUsername(ctx context.Context, username string) (*relay.User, error)
	CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error)
}

type relayIdentityEmailLookupAPI interface {
	FindUserByEmail(ctx context.Context, email string) (*relay.User, error)
}

type relayIdentityPasswordUpdater interface {
	UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error)
}

type relayIdentityUserGetter interface {
	GetUser(ctx context.Context, userID int64) (*relay.User, error)
}

type relayIdentityDefaultSubscriptionAssigner interface {
	AssignDefaultSubscriptionsForUser(ctx context.Context, userID int64) error
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
	u, _, err := r.ResolveOrProvisionForLDAP(ctx, username, email)
	return u, err
}

func (r *RelayIdentityResolver) ResolveOrProvisionForLDAP(ctx context.Context, username, email string) (*relay.User, string, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" {
		return nil, "", fmt.Errorf("relay identity: username is required")
	}
	canonicalUsername := relayProvisionUsername(username)
	if canonicalUsername == "" {
		return nil, "", fmt.Errorf("relay identity: canonical username is required")
	}

	if email != "" {
		u, err := r.findUserByEmail(ctx, email)
		if err != nil {
			return nil, "", err
		}
		if u != nil {
			return r.updateExistingLDAPRelayUser(ctx, u, canonicalUsername, false)
		}
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
		return r.updateExistingLDAPRelayUser(ctx, u, canonicalUsername, foundByLegacyUsername)
	}

	email = ensureNonEmptyEmail(email, username, r.fallbackDomain)

	pw, err := highEntropyPassword()
	if err != nil {
		return nil, "", fmt.Errorf("relay identity: generate password: %w", err)
	}

	created, err := r.api.CreateUser(ctx, relay.CreateUserRequest{
		Username:    canonicalUsername,
		Email:       email,
		Password:    pw,
		Notes:       relayLDAPProvisioningNote,
		Concurrency: defaultRelayUserConcurrency,
	})
	if err != nil {
		return nil, "", fmt.Errorf("relay identity: create user: %w", err)
	}
	return created, pw, nil
}

func (r *RelayIdentityResolver) findUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	lookup, ok := r.api.(relayIdentityEmailLookupAPI)
	if !ok {
		return nil, nil
	}
	u, err := lookup.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("relay identity: find user by email: %w", err)
	}
	return u, nil
}

func (r *RelayIdentityResolver) updateExistingLDAPRelayUser(ctx context.Context, u *relay.User, canonicalUsername string, foundByLegacyUsername bool) (*relay.User, string, error) {
	u = r.hydrateExistingLDAPRelayUser(ctx, u)
	updateReq, shouldUpdate := relayUserUpdateForLDAP(u, canonicalUsername, foundByLegacyUsername)
	resolved := u
	if !shouldUpdate {
		if err := r.assignDefaultsForExistingLDAPProvisionedUser(ctx, resolved); err != nil {
			return nil, "", err
		}
		return resolved, "", nil
	}
	if updater, ok := r.api.(relayIdentityPasswordUpdater); ok {
		updated, err := updater.UpdateUser(ctx, u.ID, updateReq)
		if err != nil {
			return nil, "", fmt.Errorf("relay identity: update user: %w", err)
		}
		if updated != nil {
			if strings.TrimSpace(updated.Notes) == "" {
				updated.Notes = u.Notes
			}
			if len(updated.AllowedGroups) == 0 {
				updated.AllowedGroups = u.AllowedGroups
				updated.AllowedGroupIDs = u.AllowedGroupIDs
			}
		}
		resolved = updated
	}
	if err := r.assignDefaultsForExistingLDAPProvisionedUser(ctx, resolved); err != nil {
		return nil, "", err
	}
	return resolved, "", nil
}

func (r *RelayIdentityResolver) ResetGeneratedPasswordForLDAPProvisionedUser(ctx context.Context, u *relay.User) (string, error) {
	u = r.hydrateExistingLDAPRelayUser(ctx, u)
	if u == nil || u.ID <= 0 || strings.TrimSpace(u.Notes) != relayLDAPProvisioningNote {
		return "", nil
	}
	updater, ok := r.api.(relayIdentityPasswordUpdater)
	if !ok {
		return "", fmt.Errorf("relay identity: update user password unsupported")
	}
	pw, err := highEntropyPassword()
	if err != nil {
		return "", fmt.Errorf("relay identity: generate password: %w", err)
	}
	if _, err := updater.UpdateUser(ctx, u.ID, relay.UpdateUserRequest{Password: pw}); err != nil {
		return "", fmt.Errorf("relay identity: reset generated password: %w", err)
	}
	return pw, nil
}

func (r *RelayIdentityResolver) hydrateExistingLDAPRelayUser(ctx context.Context, u *relay.User) *relay.User {
	if u == nil || u.ID <= 0 {
		return u
	}
	getter, ok := r.api.(relayIdentityUserGetter)
	if !ok {
		return u
	}
	full, err := getter.GetUser(ctx, u.ID)
	if err != nil || full == nil {
		return u
	}
	return mergeRelayUserFacts(u, full)
}

func mergeRelayUserFacts(base, full *relay.User) *relay.User {
	if full == nil {
		return base
	}
	if base == nil {
		return full
	}
	if strings.TrimSpace(full.Username) == "" {
		full.Username = base.Username
	}
	if strings.TrimSpace(full.Email) == "" {
		full.Email = base.Email
	}
	if strings.TrimSpace(full.Role) == "" {
		full.Role = base.Role
	}
	if strings.TrimSpace(full.Notes) == "" {
		full.Notes = base.Notes
	}
	if full.Concurrency == 0 {
		full.Concurrency = base.Concurrency
	}
	if len(full.AllowedGroups) == 0 {
		full.AllowedGroups = base.AllowedGroups
		full.AllowedGroupIDs = base.AllowedGroupIDs
	}
	return full
}

func (r *RelayIdentityResolver) assignDefaultsForExistingLDAPProvisionedUser(ctx context.Context, u *relay.User) error {
	if u == nil || u.ID <= 0 {
		return nil
	}
	if strings.TrimSpace(u.Notes) != relayLDAPProvisioningNote || len(u.AllowedGroups) > 0 || len(u.AllowedGroupIDs) > 0 {
		return nil
	}
	assigner, ok := r.api.(relayIdentityDefaultSubscriptionAssigner)
	if !ok {
		return nil
	}
	if err := assigner.AssignDefaultSubscriptionsForUser(ctx, u.ID); err != nil {
		return fmt.Errorf("relay identity: assign default subscriptions: %w", err)
	}
	return nil
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

func relayUserUpdateForLDAP(u *relay.User, canonicalUsername string, foundByLegacyUsername bool) (relay.UpdateUserRequest, bool) {
	var req relay.UpdateUserRequest
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
