package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/ai-efficiency/backend/internal/orgaccount"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// SSOProvider authenticates users against the relay server.
type SSOProvider struct {
	relayProvider relay.Provider
	logger        *zap.Logger
}

// NewSSOProvider creates a new SSO provider.
func NewSSOProvider(relayProvider relay.Provider, logger *zap.Logger) *SSOProvider {
	return &SSOProvider{
		relayProvider: relayProvider,
		logger:        logger,
	}
}

// Name returns the provider name.
func (p *SSOProvider) Name() string {
	return "sso"
}

// Authenticate verifies credentials against the relay server.
//
// The relay logs in by email, so a bare organization-account username is
// expanded with the organization domain before the call. Accounts that already
// look like an email are passed through untouched.
func (p *SSOProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	if p.relayProvider == nil {
		return nil, nil
	}

	username = expandOrgAccountIdentifier(username)
	relayUser, err := p.relayProvider.Authenticate(ctx, username, password)
	if err != nil {
		if errors.Is(err, relay.ErrInvalidCredentials) {
			p.logger.Debug("relay SSO: invalid credentials", zap.String("username", username))
			return nil, nil
		}
		if errors.Is(err, relay.ErrExtraVerificationRequired) {
			p.logger.Warn("relay SSO: extra verification required, skipping", zap.String("username", username))
			return nil, nil
		}
		p.logger.Warn("relay SSO: authentication error", zap.Error(err))
		return nil, nil
	}

	return relayUserInfo(relayUser, password), nil
}

func relayUserInfo(relayUser *relay.User, password string) *UserInfo {
	if relayUser == nil {
		return nil
	}
	relayID := int(relayUser.ID)
	role := relayUser.Role
	if role == "" {
		role = "user"
	}
	return &UserInfo{
		Username:          relayUser.Username,
		Email:             relayUser.Email,
		AuthSource:        "relay_sso",
		Role:              role,
		RelayUserID:       &relayID,
		RelayAuthPassword: password,
	}
}

// expandOrgAccountIdentifier turns a bare organization-account username into the
// relay login identifier. Inputs containing "@" are already full identifiers.
func expandOrgAccountIdentifier(username string) string {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || strings.Contains(trimmed, "@") {
		return username
	}
	return trimmed + "@" + orgaccount.OrgAccountEmailDomain
}
